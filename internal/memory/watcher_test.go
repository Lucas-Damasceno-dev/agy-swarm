package memory_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/memory"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/multiplexer"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/tmux"
)

type mockNotifier struct {
	mu       sync.Mutex
	statuses map[config.RoleType]string
	sent     map[config.RoleType][]string
}

func newMockNotifier() *mockNotifier {
	return &mockNotifier{
		statuses: make(map[config.RoleType]string),
		sent:     make(map[config.RoleType][]string),
	}
}

var _ multiplexer.PaneNotifier = (*mockNotifier)(nil)

func (m *mockNotifier) UpdatePaneStatus(role config.RoleType, status string, color string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statuses[role] = status
}

func (m *mockNotifier) UpdatePaneWithQuota(role config.RoleType, status string, color string, weeklyPct, fiveHourPct float64) {
	m.UpdatePaneStatus(role, status, color)
}

func (m *mockNotifier) SendLiteral(role config.RoleType, text string) error {
	return nil
}

func (m *mockNotifier) SendControl(role config.RoleType, key string) error {
	return nil
}

func (m *mockNotifier) SendKeys(role config.RoleType, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent[role] = append(m.sent[role], text)
	return nil
}

func (m *mockNotifier) IsPaneIdle(role config.RoleType) (bool, error) {
	return true, nil
}

func TestWatcherWithMockNotifier(t *testing.T) {
	notifier := newMockNotifier()
	client := memory.NewClient()
	roles := []config.RoleConfig{
		{Type: config.RoleLead, Name: "Maestro", Project: "test-lead"},
	}

	watcher := memory.NewWatcher(client, notifier, roles)
	if watcher == nil {
		t.Fatal("esperava instância de Watcher não nula")
	}
}

// MockMessageLister simula consultas à caixa postal sem invocar o binário ai-memory.
type MockMessageLister struct {
	mu       sync.RWMutex
	messages map[string][]memory.Message
	err      error
}

func newMockMessageLister() *MockMessageLister {
	return &MockMessageLister{
		messages: make(map[string][]memory.Message),
	}
}

func (m *MockMessageLister) ListPendingMessages(project string) ([]memory.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.err != nil {
		return nil, m.err
	}
	return m.messages[project], nil
}

func (m *MockMessageLister) SetMessages(project string, msgs []memory.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages[project] = msgs
}

func (m *MockMessageLister) SetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

func TestWatcher_PollMailboxes_NewMessages(t *testing.T) {
	mockLister := newMockMessageLister()
	mockMux := tmux.NewMockMultiplexer("test-watcher-session")

	roles := []config.RoleConfig{
		{Type: config.RoleQA, Name: "QA-Testes", Project: "swarm-qa"},
		{Type: config.RoleArch, Name: "Arquiteto", Project: "swarm-arch"},
	}

	if err := mockMux.BuildLayout(roles); err != nil {
		t.Fatalf("falha ao inicializar layout mockado: %v", err)
	}

	w := memory.NewWatcher(mockLister, mockMux, roles)

	// Rodada inicial: caixas vazias
	w.PollOnce()
	if count := w.GetSeenCount(config.RoleQA); count != 0 {
		t.Errorf("esperava contagem 0, obteve %d", count)
	}

	// Adiciona 2 mensagens para QA
	mockLister.SetMessages("swarm-qa", []memory.Message{
		{ID: "msg-1", Body: "Primeira tarefa"},
		{ID: "msg-2", Body: "Segunda tarefa"},
	})

	w.PollOnce()

	if count := w.GetSeenCount(config.RoleQA); count != 2 {
		t.Errorf("esperava contagem 2 para QA, obteve %d", count)
	}

	state := mockMux.GetPaneState(config.RoleQA)
	if !strings.Contains(state, "2 Msg(s)") {
		t.Errorf("esperava estado contendo '2 Msg(s)', obteve %q", state)
	}

	buf := mockMux.GetBuffer(config.RoleQA)
	if !strings.Contains(buf, "Nova mensagem pendente no seu inbox (swarm-qa)") {
		t.Errorf("esperava notificação no buffer de QA, obteve: %q", buf)
	}
}

func TestWatcher_PollMailboxes_ReturnToIdle(t *testing.T) {
	mockLister := newMockMessageLister()
	mockMux := tmux.NewMockMultiplexer("test-watcher-idle")

	roles := []config.RoleConfig{
		{Type: config.RoleQA, Name: "QA-Testes", Project: "swarm-qa"},
	}
	_ = mockMux.BuildLayout(roles)

	w := memory.NewWatcher(mockLister, mockMux, roles)

	// Inicia com mensagens pendentes
	mockLister.SetMessages("swarm-qa", []memory.Message{{ID: "msg-1", Body: "Tarefa"}})
	w.PollOnce()

	if state := mockMux.GetPaneState(config.RoleQA); !strings.Contains(state, "1 Msg(s)") {
		t.Fatalf("esperava estado com 1 Msg(s), obteve %q", state)
	}

	// Esvazia a caixa
	mockLister.SetMessages("swarm-qa", []memory.Message{})
	w.PollOnce()

	if count := w.GetSeenCount(config.RoleQA); count != 0 {
		t.Errorf("esperava contagem zerada, obteve %d", count)
	}

	if state := mockMux.GetPaneState(config.RoleQA); !strings.Contains(state, "Aguardando") {
		t.Errorf("esperava estado voltando para Aguardando, obteve %q", state)
	}
}

func TestWatcher_ConcurrentRaceCondition(t *testing.T) {
	mockLister := newMockMessageLister()
	mockMux := tmux.NewMockMultiplexer("test-race")

	roles := []config.RoleConfig{
		{Type: config.RoleLead, Name: "Maestro", Project: "swarm-lead"},
		{Type: config.RoleQA, Name: "QA-Testes", Project: "swarm-qa"},
		{Type: config.RoleArch, Name: "Arquiteto", Project: "swarm-arch"},
		{Type: config.RoleSec, Name: "Segurança", Project: "swarm-sec"},
	}
	_ = mockMux.BuildLayout(roles)

	w := memory.NewWatcher(mockLister, mockMux, roles)

	var wg sync.WaitGroup
	iterations := 100
	concurrency := 20

	// Dispara múltiplos leitores e escritores concorrentes em seenCounts e polling
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				// Simula atualização dinâmica de mensagens
				mockLister.SetMessages("swarm-qa", []memory.Message{
					{ID: fmt.Sprintf("msg-%d-%d", workerID, j)},
				})

				// Executa polling concorrente
				w.PollOnce()

				// Leitura concorrente
				_ = w.GetSeenCount(config.RoleQA)
				_ = w.GetSeenCount(config.RoleLead)
			}
		}(i)
	}

	wg.Wait()
}

func TestWatcher_Lifecycle_ContextAndStop(t *testing.T) {
	mockLister := newMockMessageLister()
	mockMux := tmux.NewMockMultiplexer("test-lifecycle")

	roles := []config.RoleConfig{
		{Type: config.RoleQA, Name: "QA-Testes", Project: "swarm-qa"},
	}
	_ = mockMux.BuildLayout(roles)

	w := memory.NewWatcher(mockLister, mockMux, roles)

	ctx, cancel := context.WithCancel(context.Background())
	w.Start(ctx)

	// Valida que Stop() é idempotente e seguro contra panics
	time.Sleep(50 * time.Millisecond)
	w.Stop()
	w.Stop()

	// Valida cancelamento do contexto
	cancel()
}

func TestWatcher_SyncBarrierAndDebounce(t *testing.T) {
	mockLister := newMockMessageLister()
	mockMux := tmux.NewMockMultiplexer("test-barrier")

	roles := []config.RoleConfig{
		{Type: config.RoleLead, Name: "Maestro", Project: "swarm-lead"},
		{Type: config.RoleArch, Name: "Arquiteto", Project: "swarm-arch"},
		{Type: config.RoleHacker, Name: "Pragmatico", Project: "swarm-hacker"},
	}
	_ = mockMux.BuildLayout(roles)

	w := memory.NewWatcher(mockLister, mockMux, roles)
	// ExpectedSpecialists = 2 (Arch + Hacker)

	// Rodada 1: Nenhuma mensagem para o Maestro
	w.PollOnce()
	if w.IsBarrierFired() {
		t.Fatalf("barreira não deveria disparar sem mensagens")
	}
	if len(mockMux.GetBuffer(config.RoleLead)) > 0 {
		t.Fatalf("Maestro não deveria ter recebido notificação")
	}

	// Rodada 2: Apenas 1 especialista respondeu (quórum = 2)
	mockLister.SetMessages("swarm-lead", []memory.Message{{ID: "msg-1"}})
	w.PollOnce()
	if w.IsBarrierFired() {
		t.Fatalf("barreira não deveria disparar com apenas 1 de 2 propostas")
	}
	if len(mockMux.GetBuffer(config.RoleLead)) > 0 {
		t.Fatalf("Maestro ainda não deveria ser acordado antes da barreira")
	}

	// Rodada 3: 2 especialistas responderam -> Barreira deve disparar!
	mockLister.SetMessages("swarm-lead", []memory.Message{{ID: "msg-1"}, {ID: "msg-2"}})
	w.PollOnce()
	if !w.IsBarrierFired() {
		t.Fatalf("barreira deveria ter disparado com 2 propostas")
	}
	buf := mockMux.GetBuffer(config.RoleLead)
	if !strings.Contains(buf, "Barreira de sincronização concluída") {
		t.Fatalf("notificação esperada não encontrada no buffer do Maestro: %s", buf)
	}

	// Rodada 4: Mesma quantidade (2 mensagens) -> Debounce não deve re-enviar
	initialLen := len(buf)
	w.PollOnce()
	if len(mockMux.GetBuffer(config.RoleLead)) != initialLen {
		t.Fatalf("debounce falhou: mensagem reenviada sem alteração no inbox")
	}
}
