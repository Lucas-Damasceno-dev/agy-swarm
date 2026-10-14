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
