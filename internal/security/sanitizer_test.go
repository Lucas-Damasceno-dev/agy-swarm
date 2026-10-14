package security_test

import (
	"testing"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/security"
)

func TestSanitizePTYInput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Comando normal",
			input:    "git status",
			expected: "git status",
		},
		{
			name:     "Injeção de nova linha simples",
			input:    "rm -rf /\necho ok",
			expected: "rm -rf / echo ok",
		},
		{
			name:     "Injeção CRLF múltipla",
			input:    "cmd1\r\ncmd2\r\ncmd3",
			expected: "cmd1 cmd2 cmd3",
		},
		{
			name:     "Injeção de carriage return",
			input:    "privilegiado\rsobrescrever",
			expected: "privilegiado sobrescrever",
		},
		{
			name:     "Sequência ANSI CSI Color",
			input:    "\x1b[31mTexto Vermelho\x1b[0m",
			expected: "Texto Vermelho",
		},
		{
			name:     "Sequência ANSI OSC Title",
			input:    "\x1b]0;Hacked Title\x07Payload Real",
			expected: "Payload Real",
		},
		{
			name:     "Caracteres nulos e controle ASCII",
			input:    "teste\x00\x01\x02\x07dados",
			expected: "testedados",
		},
		{
			name:     "Preserva acentuação e emojis válidos",
			input:    "⚠️ Alerta: Função de Teste Concluída! 🚀",
			expected: "⚠️ Alerta: Função de Teste Concluída! 🚀",
		},
		{
			name:     "String vazia",
			input:    "",
			expected: "",
		},
		{
			name:     "Espaços em branco redundantes",
			input:    "   go test -v ./...   \n  ",
			expected: "go test -v ./...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := security.SanitizePTYInput(tt.input)
			if got != tt.expected {
				t.Errorf("SanitizePTYInput(%q) = %q; esperava %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestQuoteShellArg(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Palavra simples",
			input:    "arquivo.txt",
			expected: "'arquivo.txt'",
		},
		{
			name:     "String com espaços",
			input:    "meu arquivo de teste.txt",
			expected: "'meu arquivo de teste.txt'",
		},
		{
			name:     "String com aspas simples",
			input:    "it's complicated",
			expected: "'it'\\''s complicated'",
		},
		{
			name:     "Injeção de comandos shell",
			input:    "$(rm -rf /); touch /tmp/pwn",
			expected: "'$(rm -rf /); touch /tmp/pwn'",
		},
		{
			name:     "String vazia",
			input:    "",
			expected: "''",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := security.QuoteShellArg(tt.input)
			if got != tt.expected {
				t.Errorf("QuoteShellArg(%q) = %q; esperava %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestContainsDangerousShellMetachars(t *testing.T) {
	dangerous := []string{
		"ls ; rm -rf /",
		"make && exit",
		"cat /etc/passwd | grep root",
		"`whoami`",
		"$(id)",
		"echo $FOO > file",
		"cat < file",
	}

	for _, d := range dangerous {
		if !security.ContainsDangerousShellMetachars(d) {
			t.Errorf("esperava detectar metacaractere perigoso em %q", d)
		}
	}

	safe := []string{
		"git status",
		"go test ./...",
		"normal-argument_value:123",
	}

	for _, s := range safe {
		if security.ContainsDangerousShellMetachars(s) {
			t.Errorf("falso positivo para comando seguro %q", s)
		}
	}
}
