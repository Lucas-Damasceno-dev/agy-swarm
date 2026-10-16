package tmux

import (
	"testing"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
)

func TestSanitizeInput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "clean string",
			input:    "git status && go test ./...",
			expected: "git status && go test ./...",
		},
		{
			name:     "null byte injection",
			input:    "malicious\x00command",
			expected: "maliciouscommand",
		},
		{
			name:     "ansi escape sequences (colors and styles)",
			input:    "\x1b[31;1mRed Alert\x1b[0m",
			expected: "Red Alert",
		},
		{
			name:     "osc 52 terminal clipboard hijack",
			input:    "data\x1b]52;c;ZXhwbG9pdA==\x07rest",
			expected: "datarest",
		},
		{
			name:     "lone escape byte",
			input:    "prompt\x1bwith lone escape",
			expected: "promptwith lone escape",
		},
		{
			name:     "crlf normalization",
			input:    "line1\r\nline2\rline3\n",
			expected: "line1\nline2line3\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeInput(tt.input)
			if got != tt.expected {
				t.Errorf("SanitizeInput(%q) = %q, esperado %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSendControlAllowlist(t *testing.T) {
	d := NewDriver("test-session", "/tmp")
	d.Panes[config.RoleLead] = &PaneInfo{ID: "%0", Role: config.DefaultRoles()[0]}

	// Tecla proibida deve falhar imediatamente na validação
	err := d.SendControl(config.RoleLead, "C-x")
	if err == nil {
		t.Errorf("esperado erro ao tentar enviar tecla não autorizada 'C-x'")
	}

	// Pane inexistente deve retornar erro
	err = d.SendControl(config.RoleArch, "Enter")
	if err == nil {
		t.Errorf("esperado erro para pane não registrado")
	}
}

func TestBuild3ZoneLayoutSessionSurvival(t *testing.T) {
	sessionName := "test-survival-session"
	tmpDir := t.TempDir()

	d := NewDriver(sessionName, tmpDir)
	defer func() {
		_ = d.KillSession()
	}()

	roles := config.DefaultRoles()
	err := d.BuildLayout(roles)
	if err != nil {
		t.Fatalf("BuildLayout falhou: %v", err)
	}

	if !d.SessionExists() {
		t.Fatalf("sessão Tmux foi destruída prematuramente após BuildLayout")
	}

	if !d.PaneExists(roles[0].Type) {
		t.Errorf("pane do Maestro não existe na sessão ativa")
	}

	if !d.PaneExists("telemetry") {
		t.Errorf("pane de telemetria não existe na sessão ativa")
	}

	if !d.PaneExists("viewport") {
		t.Errorf("pane de viewport não existe na sessão ativa")
	}
}

func TestSaveAndHydratePanesToRuntime(t *testing.T) {
	tmpDir := t.TempDir()
	session := "test-hydrate-session"
	d1 := NewDriver(session, tmpDir)
	d1.Panes[config.RoleLead] = &PaneInfo{ID: "%10", Role: config.RoleConfig{Type: config.RoleLead, Name: "Maestro"}}
	d1.Panes["viewport"] = &PaneInfo{ID: "%11", Role: config.RoleConfig{Type: "viewport", Name: "Viewport"}}

	if err := d1.SavePanesToRuntime(tmpDir); err != nil {
		t.Fatalf("SavePanesToRuntime falhou: %v", err)
	}

	d2 := NewDriver(session, tmpDir)
	if err := d2.HydratePanesFromRuntime(tmpDir); err != nil {
		t.Fatalf("HydratePanesFromRuntime falhou: %v", err)
	}

	if p, ok := d2.Panes[config.RoleLead]; !ok || p.ID != "%10" {
		t.Errorf("esperava pane do lead hidratado com %%10, obteve: %+v", p)
	}
	if p, ok := d2.Panes["viewport"]; !ok || p.ID != "%11" {
		t.Errorf("esperava pane de viewport hidratado com %%11, obteve: %+v", p)
	}
}

func TestSendLiteralViewportProtection(t *testing.T) {
	d := NewDriver("test-protect", "/tmp")
	// Especialista compartilha o mesmo pane ID do viewport
	d.Panes["viewport"] = &PaneInfo{ID: "%99", Role: config.RoleConfig{Type: "viewport"}}
	d.Panes[config.RoleQA] = &PaneInfo{ID: "%99", Role: config.RoleConfig{Type: config.RoleQA}}

	// Envio para especialista compartilhado deve retornar nil (silenciado) sem chamar exec
	err := d.SendLiteral(config.RoleQA, "mensagem invasiva")
	if err != nil {
		t.Errorf("esperava envio silenciado sem erro, obteve: %v", err)
	}
}

