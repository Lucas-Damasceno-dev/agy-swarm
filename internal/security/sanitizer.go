package security

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	// ansiRegex detecta sequências de escape ANSI/VT100 (CSI, OSC, etc.), cobrindo OSC 52 e sequências estendidas.
	ansiRegex = regexp.MustCompile(`\x1b(\[[0-9;]*[a-zA-Z~]|\][^\x07\x1b]*(\x07|\x1b\\)|[PX^_][^\x1b]*\x1b\\|[()][AB012])`)
)

// SanitizeInput remove bytes nulos, sequências de escape ANSI e normaliza quebras de linha.
func SanitizeInput(s string) string {
	// Remove bytes nulos
	s = strings.ReplaceAll(s, "\x00", "")
	// Remove sequências ANSI conhecidas
	s = ansiRegex.ReplaceAllString(s, "")
	// Remove qualquer ESC residual
	s = strings.ReplaceAll(s, "\x1b", "")
	// Normaliza quebras de linha carriage-return
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}

// SanitizePTYInput sanitiza texto a ser enviado para o PTY via SendKeys,
// neutralizando escapes ANSI maliciosos, quebras de linha acidentais e caracteres de controle.
func SanitizePTYInput(input string) string {
	if input == "" {
		return ""
	}

	// 1. Remove sequências de escape ANSI que tentam redefinir terminal ou injetar comandos
	cleaned := ansiRegex.ReplaceAllString(input, "")

	// 2. Normaliza quebras de linha e retornos de carro para espaços para evitar execução prematura no PTY
	cleaned = strings.ReplaceAll(cleaned, "\r\n", " ")
	cleaned = strings.ReplaceAll(cleaned, "\r", " ")
	cleaned = strings.ReplaceAll(cleaned, "\n", " ")

	// 3. Remove caracteres de controle ASCII perigosos (0x00-0x1F exceto tab e printable)
	var sb strings.Builder
	for _, r := range cleaned {
		if r == '\t' || (!unicode.IsControl(r) && r != 0x7f) {
			sb.WriteRune(r)
		}
	}

	return strings.TrimSpace(sb.String())
}

// QuoteShellArg aplica escaping seguro no formato POSIX single-quote para argumentos de shell.
func QuoteShellArg(arg string) string {
	if arg == "" {
		return "''"
	}
	// Em shells POSIX, single quotes protegem todos os caracteres literais, exceto ela mesma.
	// Substitui ' por '\''
	return "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
}

// ContainsDangerousShellMetachars detecta caracteres comumente abusados para command injection.
func ContainsDangerousShellMetachars(input string) bool {
	dangerous := []string{";", "&&", "||", "`", "$(", "${", "\n", "\r", ">", "<", "|"}
	for _, d := range dangerous {
		if strings.Contains(input, d) {
			return true
		}
	}
	return false
}
