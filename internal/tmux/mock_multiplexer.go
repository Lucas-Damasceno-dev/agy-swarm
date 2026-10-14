package tmux

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
)

// CallRecord registra uma invocação de método no MockMultiplexer para verificação em testes.
type CallRecord struct {
	Method string
	Args   []interface{}
}

// MockMultiplexer implementa Multiplexer puramente in-memory, thread-safe e sem chamadas ao SO.
type MockMultiplexer struct {
	mu            sync.RWMutex
	SessionName   string
	Exists        bool
	KillErr       error
	BuildErr      error
	SendErr       error
	Panes         map[config.RoleType]*PaneInfo
	Buffers       map[config.RoleType]*bytes.Buffer
	RecordedCalls []CallRecord
}

var _ Multiplexer = (*MockMultiplexer)(nil)

// NewMockMultiplexer inicializa um multiplexer mockado em memória.
func NewMockMultiplexer(sessionName string) *MockMultiplexer {
	return &MockMultiplexer{
		SessionName:   sessionName,
		Exists:        false,
		Panes:         make(map[config.RoleType]*PaneInfo),
		Buffers:       make(map[config.RoleType]*bytes.Buffer),
		RecordedCalls: make([]CallRecord, 0),
	}
}

// SessionExists retorna o estado de existência da sessão mockada.
func (m *MockMultiplexer) SessionExists() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RecordedCalls = append(m.RecordedCalls, CallRecord{Method: "SessionExists"})
	return m.Exists
}

// KillSession simula o encerramento da sessão.
func (m *MockMultiplexer) KillSession() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RecordedCalls = append(m.RecordedCalls, CallRecord{Method: "KillSession"})
	if m.KillErr != nil {
		return m.KillErr
	}
	m.Exists = false
	return nil
}

// BuildLayout simula a alocação de painéis para os papéis fornecidos.
func (m *MockMultiplexer) BuildLayout(roles []config.RoleConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RecordedCalls = append(m.RecordedCalls, CallRecord{
		Method: "BuildLayout",
		Args:   []interface{}{roles},
	})
	if m.BuildErr != nil {
		return m.BuildErr
	}
	if len(roles) == 0 {
		return fmt.Errorf("nenhum papel configurado")
	}

	m.Exists = true
	for i, r := range roles {
		paneID := fmt.Sprintf("%%pane-%d", i)
		m.Panes[r.Type] = &PaneInfo{
			ID:       paneID,
			Role:     r,
			State:    "Inicializando",
			Worktree: "",
		}
		if _, ok := m.Buffers[r.Type]; !ok {
			m.Buffers[r.Type] = new(bytes.Buffer)
		}
	}
	return nil
}

// SetupHUD simula a configuração de cabeçalhos e bordas.
func (m *MockMultiplexer) SetupHUD() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RecordedCalls = append(m.RecordedCalls, CallRecord{Method: "SetupHUD"})
}

// UpdateStatusBar simula a atualização do status global.
func (m *MockMultiplexer) UpdateStatusBar(clusterInfo string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RecordedCalls = append(m.RecordedCalls, CallRecord{
		Method: "UpdateStatusBar",
		Args:   []interface{}{clusterInfo},
	})
}

// UpdatePaneWithQuota simula a atualização de status e cota do painel.
func (m *MockMultiplexer) UpdatePaneWithQuota(role config.RoleType, status string, color string, weeklyPct, fiveHourPct float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RecordedCalls = append(m.RecordedCalls, CallRecord{
		Method: "UpdatePaneWithQuota",
		Args:   []interface{}{role, status, color, weeklyPct, fiveHourPct},
	})
	if p, ok := m.Panes[role]; ok {
		p.State = status
	}
}

// UpdatePaneStatus simula a atualização de status e estilo do painel.
func (m *MockMultiplexer) UpdatePaneStatus(role config.RoleType, status string, color string) {
	m.UpdatePaneWithQuota(role, status, color, 0, 0)
}

// SendKeys grava o texto direcionado ao buffer do painel correspondente.
func (m *MockMultiplexer) SendKeys(role config.RoleType, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RecordedCalls = append(m.RecordedCalls, CallRecord{
		Method: "SendKeys",
		Args:   []interface{}{role, text},
	})
	if m.SendErr != nil {
		return m.SendErr
	}
	buf, ok := m.Buffers[role]
	if !ok {
		buf = new(bytes.Buffer)
		m.Buffers[role] = buf
	}
	buf.WriteString(text)
	buf.WriteString("\n")
	return nil
}

// GetRecordedCalls retorna uma cópia de segurança das chamadas registradas.
func (m *MockMultiplexer) GetRecordedCalls() []CallRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make([]CallRecord, len(m.RecordedCalls))
	copy(res, m.RecordedCalls)
	return res
}

// GetBuffer retorna o conteúdo acumulado enviado a um painel.
func (m *MockMultiplexer) GetBuffer(role config.RoleType) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if buf, ok := m.Buffers[role]; ok {
		return buf.String()
	}
	return ""
}

// GetPaneState retorna o status atual gravado para o painel.
func (m *MockMultiplexer) GetPaneState(role config.RoleType) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if p, ok := m.Panes[role]; ok {
		return p.State
	}
	return ""
}

// SetExists ajusta manualmente o estado de existência da sessão.
func (m *MockMultiplexer) SetExists(exists bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Exists = exists
}

// SetSendError injeta um erro para chamadas subsequentes de SendKeys.
func (m *MockMultiplexer) SetSendError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SendErr = err
}

// SetBuildError injeta um erro para chamadas de BuildLayout.
func (m *MockMultiplexer) SetBuildError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.BuildErr = err
}

// SetKillError injeta um erro para chamadas de KillSession.
func (m *MockMultiplexer) SetKillError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.KillErr = err
}

// SendLiteral simula o envio literal delegando ao buffer de SendKeys.
func (m *MockMultiplexer) SendLiteral(role config.RoleType, text string) error {
	return m.SendKeys(role, text)
}

// SendControl simula o envio de teclas de controle delegando ao buffer de SendKeys.
func (m *MockMultiplexer) SendControl(role config.RoleType, key string) error {
	return m.SendKeys(role, key)
}

// IsPaneAlive retorna se o painel existe e está ativo.
func (m *MockMultiplexer) IsPaneAlive(role config.RoleType) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.Panes[role]
	return ok, nil
}

// PaneExists verifica se o painel existe.
func (m *MockMultiplexer) PaneExists(role config.RoleType) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.Panes[role]
	return ok
}

// IsPaneIdle simula a verificação de painel ocioso.
func (m *MockMultiplexer) IsPaneIdle(role config.RoleType) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return true, nil
}

