package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/multiplexer"
)

// MessageLister define a interface para consulta de mensagens pendentes no mailbox.
type MessageLister interface {
	ListPendingMessages(project string) ([]Message, error)
}

// Watcher monitora os mailboxes dos agentes e acorda os panes correspondentes com barreira de sincronização e debounce.
type Watcher struct {
	Client              MessageLister
	Notifier            multiplexer.PaneNotifier
	Roles               []config.RoleConfig
	ExpectedSpecialists int
	BarrierTimeout      time.Duration
	BarrierStartTime    time.Time
	mu                  sync.RWMutex
	seenCounts          map[config.RoleType]int
	lastNotifiedCounts  map[config.RoleType]int
	lastNotifiedAt      map[config.RoleType]time.Time
	barrierFired        bool
	stopChan            chan struct{}
	stopOnce            sync.Once
}

// NewWatcher cria uma nova instância do monitor de mensagens com desacoplamento ISP, thread-safety e barreira de sincronização.
func NewWatcher(client MessageLister, notifier multiplexer.PaneNotifier, roles []config.RoleConfig) *Watcher {
	exp := len(roles) - 1
	if exp < 1 {
		exp = 1
	}
	return &Watcher{
		Client:              client,
		Notifier:            notifier,
		Roles:               roles,
		ExpectedSpecialists: exp,
		BarrierTimeout:      2 * time.Minute,
		BarrierStartTime:    time.Now(),
		seenCounts:          make(map[config.RoleType]int),
		lastNotifiedCounts:  make(map[config.RoleType]int),
		lastNotifiedAt:      make(map[config.RoleType]time.Time),
		stopChan:            make(chan struct{}),
	}
}

// Start inicia o loop de monitoramento assíncrono.
func (w *Watcher) Start(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-w.stopChan:
				return
			case <-ticker.C:
				w.pollMailboxes()
			}
		}
	}()
}

// Stop encerra o watcher graciosamente e com garantia de idempotência.
func (w *Watcher) Stop() {
	w.stopOnce.Do(func() {
		close(w.stopChan)
	})
}

// GetSeenCount retorna a quantidade de mensagens registradas de forma thread-safe.
func (w *Watcher) GetSeenCount(role config.RoleType) int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.seenCounts[role]
}

// IsBarrierFired retorna se a barreira de sincronização do Maestro já foi atingida.
func (w *Watcher) IsBarrierFired() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.barrierFired
}

// PollOnce executa uma única rodada determinística do polling para cada papel.
func (w *Watcher) PollOnce() {
	w.pollMailboxes()
}

// pollMailboxes verifica se há novas mensagens para cada papel ativo com proteção de mutex.
func (w *Watcher) pollMailboxes() {
	w.mu.Lock()
	defer w.mu.Unlock()

	for _, role := range w.Roles {
		// Proteção estrita contra poluição de tela: nunca injeta texto no viewport (execução de pipeline) ou na telemetria
		if role.Type == "viewport" || role.Type == "telemetry" {
			continue
		}

		msgs, err := w.Client.ListPendingMessages(role.Project)
		if err != nil {
			continue
		}

		current := len(msgs)
		prev := w.seenCounts[role.Type]
		w.seenCounts[role.Type] = current

		// Barreira de sincronização: Maestro só é acordado quando atingir quórum ou timeout
		if role.Type == config.RoleLead && !w.barrierFired {
			timedOut := current > 0 && time.Since(w.BarrierStartTime) >= w.BarrierTimeout
			ready := current >= w.ExpectedSpecialists || timedOut

			if ready {
				w.barrierFired = true
				w.lastNotifiedCounts[role.Type] = current
				w.lastNotifiedAt[role.Type] = time.Now()
				w.Notifier.UpdatePaneStatus(role.Type, fmt.Sprintf("💬 %d Proposta(s)", current), "green")
				prompt := fmt.Sprintf("🎯 Barreira de sincronização concluída! %d proposta(s) dos especialistas foram recebidas no seu inbox (%s). Execute 'ai-memory message list --project %s' e processe-as em lote.", current, role.Project, role.Project)
				_ = w.Notifier.SendKeys(role.Type, prompt)
			} else {
				w.Notifier.UpdatePaneStatus(role.Type, fmt.Sprintf("⏳ %d/%d Propostas", current, w.ExpectedSpecialists), "yellow")
			}
			continue
		}

		// Especialistas ou notificações pós-barreira do Maestro com debounce
		if current > prev {
			lastCount := w.lastNotifiedCounts[role.Type]
			lastTime := w.lastNotifiedAt[role.Type]

			// Debounce: evita re-injeção imediata em rajada (coalescing com janela de 10s)
			if current != lastCount || time.Since(lastTime) > 10*time.Second {
				w.lastNotifiedCounts[role.Type] = current
				w.lastNotifiedAt[role.Type] = time.Now()
				w.Notifier.UpdatePaneStatus(role.Type, fmt.Sprintf("💬 %d Msg(s)", current), "green")
				prompt := fmt.Sprintf("⚠️ Nova mensagem pendente no seu inbox (%s). Execute 'ai-memory message list --project %s' e processe-a.", role.Project, role.Project)
				_ = w.Notifier.SendKeys(role.Type, prompt)
			}
		} else if current == 0 && prev > 0 {
			w.lastNotifiedCounts[role.Type] = 0
			w.Notifier.UpdatePaneStatus(role.Type, "💤 Aguardando", "blue")
		}
	}
}
