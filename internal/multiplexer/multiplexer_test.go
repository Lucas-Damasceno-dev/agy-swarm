package multiplexer_test

import (
	"sync"
	"testing"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/multiplexer"
)

// MockMultiplexer simula um driver de multiplexação em memória para testes.
type MockMultiplexer struct {
	mu               sync.Mutex
	SessionExistsVal bool
	PanesAlive       map[config.RoleType]bool
	Statuses         map[config.RoleType]string
	SentLiterals     map[config.RoleType][]string
	SentControls     map[config.RoleType][]string
	SentKeysList     map[config.RoleType][]string
}

func NewMockMultiplexer() *MockMultiplexer {
	return &MockMultiplexer{
		PanesAlive:   make(map[config.RoleType]bool),
		Statuses:     make(map[config.RoleType]string),
		SentLiterals: make(map[config.RoleType][]string),
		SentControls: make(map[config.RoleType][]string),
		SentKeysList: make(map[config.RoleType][]string),
	}
}

var _ multiplexer.Multiplexer = (*MockMultiplexer)(nil)
var _ multiplexer.PaneNotifier = (*MockMultiplexer)(nil)

func (m *MockMultiplexer) SessionExists() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.SessionExistsVal
}

func (m *MockMultiplexer) PaneExists(role config.RoleType) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.PanesAlive[role]
}


func (m *MockMultiplexer) KillSession() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SessionExistsVal = false
	return nil
}

func (m *MockMultiplexer) BuildLayout(roles []config.RoleConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range roles {
		m.PanesAlive[r.Type] = true
	}
	return nil
}

func (m *MockMultiplexer) SetupHUD() {}

func (m *MockMultiplexer) UpdateStatusBar(info string) {}

func (m *MockMultiplexer) UpdatePaneWithQuota(role config.RoleType, status string, color string, weeklyPct, fiveHourPct float64) {
	m.UpdatePaneStatus(role, status, color)
}

func (m *MockMultiplexer) IsPaneAlive(role config.RoleType) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.PanesAlive[role], nil
}

func (m *MockMultiplexer) UpdatePaneStatus(role config.RoleType, status string, color string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Statuses[role] = status
}

func (m *MockMultiplexer) SendLiteral(role config.RoleType, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SentLiterals[role] = append(m.SentLiterals[role], text)
	return nil
}

func (m *MockMultiplexer) SendControl(role config.RoleType, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SentControls[role] = append(m.SentControls[role], key)
	return nil
}

func (m *MockMultiplexer) SendKeys(role config.RoleType, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SentKeysList[role] = append(m.SentKeysList[role], text)
	return nil
}

func TestMockMultiplexerSatisfiesInterfaces(t *testing.T) {
	mock := NewMockMultiplexer()
	var mux multiplexer.Multiplexer = mock
	var notifier multiplexer.PaneNotifier = mock

	if mux.SessionExists() {
		t.Errorf("esperava sessão inexistente")
	}

	roles := config.DefaultRoles()
	if err := mux.BuildLayout(roles); err != nil {
		t.Fatalf("erro ao construir layout mock: %v", err)
	}

	alive, err := mux.IsPaneAlive(config.RoleLead)
	if err != nil || !alive {
		t.Errorf("esperava pane Lead vivo, obteve alive=%v, err=%v", alive, err)
	}

	notifier.UpdatePaneStatus(config.RoleLead, "ativo", "green")
	if mock.Statuses[config.RoleLead] != "ativo" {
		t.Errorf("status inesperado: %s", mock.Statuses[config.RoleLead])
	}

	if err := notifier.SendLiteral(config.RoleLead, "ls -la"); err != nil {
		t.Fatalf("erro no SendLiteral: %v", err)
	}
	if len(mock.SentLiterals[config.RoleLead]) != 1 || mock.SentLiterals[config.RoleLead][0] != "ls -la" {
		t.Errorf("literal não registrado corretamente")
	}

	if err := notifier.SendControl(config.RoleLead, "Enter"); err != nil {
		t.Fatalf("erro no SendControl: %v", err)
	}
	if len(mock.SentControls[config.RoleLead]) != 1 || mock.SentControls[config.RoleLead][0] != "Enter" {
		t.Errorf("controle não registrado corretamente")
	}

	if err := notifier.SendKeys(config.RoleLead, "echo hello"); err != nil {
		t.Fatalf("erro no SendKeys: %v", err)
	}
	if len(mock.SentKeysList[config.RoleLead]) != 1 || mock.SentKeysList[config.RoleLead][0] != "echo hello" {
		t.Errorf("keys não registrado corretamente")
	}
}
