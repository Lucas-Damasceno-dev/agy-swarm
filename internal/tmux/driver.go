package tmux

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/multiplexer"
)

var (
	_ multiplexer.Multiplexer = (*Driver)(nil)
	_ multiplexer.PaneNotifier = (*Driver)(nil)
)

// PaneState representa a atribuição de um pane a um papel.
type PaneInfo struct {
	ID       string
	Role     config.RoleConfig
	State    string
	Worktree string
}

// Driver gerencia sessões, splits e comandos do Tmux.
type Driver struct {
	SessionName string
	Workdir     string
	Detach      bool
	Panes       map[config.RoleType]*PaneInfo
}

// NewDriver inicializa o driver com o nome da sessão.
func NewDriver(sessionName, workdir string) *Driver {
	return &Driver{
		SessionName: sessionName,
		Workdir:     workdir,
		Detach:      false,
		Panes:       make(map[config.RoleType]*PaneInfo),
	}
}

// SessionExists verifica se a sessão do Tmux já está ativa.
func (d *Driver) SessionExists() bool {
	cmd := exec.Command("tmux", "has-session", "-t", d.SessionName)
	return cmd.Run() == nil
}

// PaneExists verifica se o pane associado ao papel existe e está acessível.
func (d *Driver) PaneExists(role config.RoleType) bool {
	p, ok := d.Panes[role]
	if !ok || p.ID == "" {
		return false
	}
	cmd := exec.Command("tmux", "display-message", "-p", "-t", p.ID, "#{pane_id}")
	return cmd.Run() == nil
}

// KillSession finaliza a sessão Tmux.
func (d *Driver) KillSession() error {
	cmd := exec.Command("tmux", "kill-session", "-t", d.SessionName)
	return cmd.Run()
}

// BuildLayout cria a sessão e os splits de acordo com os papéis configurados.
func (d *Driver) BuildLayout(roles []config.RoleConfig) error {
	if len(roles) == 0 {
		return fmt.Errorf("nenhum papel configurado")
	}

	// 1. Cria a sessão inicial com o Pane 0 (Maestro)
	out, err := exec.Command("tmux", "new-session", "-d", "-s", d.SessionName, "-n", "swarm", "-c", d.Workdir, "-P", "-F", "#{pane_id}").Output()
	if err != nil {
		return fmt.Errorf("falha ao criar sessão tmux: %w", err)
	}
	pane0 := strings.TrimSpace(string(out))
	d.Panes[roles[0].Type] = &PaneInfo{
		ID:       pane0,
		Role:     roles[0],
		State:    "Inicializando",
		Worktree: d.Workdir,
	}

	// Se tiver apenas 1 papel (Maestro isolado), encerra
	if len(roles) == 1 {
		d.SetupHUD()
		return nil
	}

	// 2. Se tiver 2 ou mais papéis: divide a tela em 50% esquerda e 50% direita
	out, err = exec.Command("tmux", "split-window", "-h", "-t", pane0, "-c", d.Workdir, "-P", "-F", "#{pane_id}").Output()
	if err != nil {
		return fmt.Errorf("falha ao dividir painel principal: %w", err)
	}
	rightTop := strings.TrimSpace(string(out))
	d.Panes[roles[1].Type] = &PaneInfo{
		ID:       rightTop,
		Role:     roles[1],
		State:    "Inicializando",
		Worktree: "",
	}

	// Ajusta proporção 50/50 na horizontal
	_ = exec.Command("tmux", "select-layout", "-t", d.SessionName, "even-horizontal").Run()

	// 3. Organiza os demais especialistas na coluna direita
	switch len(roles) {
	case 2:
		// Apenas Maestro e Especialista 1 (50/50 horizontal)
	case 3:
		// Divide o lado direito verticalmente (top e bottom)
		out, err = exec.Command("tmux", "split-window", "-v", "-t", rightTop, "-c", d.Workdir, "-P", "-F", "#{pane_id}").Output()
		if err == nil {
			d.Panes[roles[2].Type] = &PaneInfo{
				ID:       strings.TrimSpace(string(out)),
				Role:     roles[2],
				State:    "Inicializando",
				Worktree: "",
			}
		}
	case 4:
		// Divide o lado direito em 3 linhas
		p2, _ := exec.Command("tmux", "split-window", "-v", "-t", rightTop, "-c", d.Workdir, "-P", "-F", "#{pane_id}").Output()
		p3, _ := exec.Command("tmux", "split-window", "-v", "-t", strings.TrimSpace(string(p2)), "-c", d.Workdir, "-P", "-F", "#{pane_id}").Output()
		d.Panes[roles[2].Type] = &PaneInfo{ID: strings.TrimSpace(string(p2)), Role: roles[2], State: "Inicializando"}
		d.Panes[roles[3].Type] = &PaneInfo{ID: strings.TrimSpace(string(p3)), Role: roles[3], State: "Inicializando"}
	case 5:
		// Grade 2x2 perfeita no lado direito:
		// Passo A: divide rightTop verticalmente -> cria rightBottom
		rbOut, err := exec.Command("tmux", "split-window", "-v", "-t", rightTop, "-c", d.Workdir, "-P", "-F", "#{pane_id}").Output()
		if err != nil {
			return fmt.Errorf("falha ao criar grid vertical direito: %w", err)
		}
		rightBottom := strings.TrimSpace(string(rbOut))

		// Passo B: divide rightTop horizontalmente -> cria top-right
		trOut, err := exec.Command("tmux", "split-window", "-h", "-t", rightTop, "-c", d.Workdir, "-P", "-F", "#{pane_id}").Output()
		if err != nil {
			return fmt.Errorf("falha ao criar split superior direito: %w", err)
		}

		// Passo C: divide rightBottom horizontalmente -> cria bottom-right
		brOut, err := exec.Command("tmux", "split-window", "-h", "-t", rightBottom, "-c", d.Workdir, "-P", "-F", "#{pane_id}").Output()
		if err != nil {
			return fmt.Errorf("falha ao criar split inferior direito: %w", err)
		}

		// Mapeia os 4 especialistas no quadrante 2x2:
		// roles[1] = Top-Left (rightTop)
		// roles[2] = Top-Right (trOut)
		// roles[3] = Bottom-Left (rightBottom)
		// roles[4] = Bottom-Right (brOut)
		d.Panes[roles[2].Type] = &PaneInfo{ID: strings.TrimSpace(string(trOut)), Role: roles[2], State: "Inicializando"}
		d.Panes[roles[3].Type] = &PaneInfo{ID: rightBottom, Role: roles[3], State: "Inicializando"}
		d.Panes[roles[4].Type] = &PaneInfo{ID: strings.TrimSpace(string(brOut)), Role: roles[4], State: "Inicializando"}
	}

	// 4. Garante que o painel 0 ocupe metade da largura da janela
	_ = exec.Command("tmux", "select-pane", "-t", pane0).Run()

	// 5. Proteção de Auto-Destruição: se o terminal (foot via Super+C) for fechado,
	// destrói a sessão e dispara limpeza de worktrees automaticamente.
	if !d.Detach {
		_ = exec.Command("tmux", "set-option", "-t", d.SessionName, "destroy-unattached", "on").Run()
		_ = exec.Command("tmux", "set-hook", "-t", d.SessionName, "client-detached", "run-shell 'agy-swarm stop'").Run()
	}

	d.SetupHUD()
	return nil
}

// SetupHUD configura os títulos e bordas padrão no Tmux.
func (d *Driver) SetupHUD() {
	_ = exec.Command("tmux", "set-option", "-t", d.SessionName, "pane-border-status", "top").Run()
	_ = exec.Command("tmux", "set-option", "-t", d.SessionName, "pane-border-format", " #{pane_title} ").Run()
	_ = exec.Command("tmux", "set-option", "-t", d.SessionName, "pane-border-lines", "heavy").Run()

	for _, p := range d.Panes {
		d.UpdatePaneStatus(p.Role.Type, p.State, "blue")
	}
}

// UpdatePaneWithQuota atualiza o título do painel incluindo métricas ao vivo de cota semanal e 5h.
func (d *Driver) UpdatePaneWithQuota(role config.RoleType, status string, color string, weeklyPct, fiveHourPct float64) {
	p, ok := d.Panes[role]
	if !ok || p.ID == "" {
		return
	}
	p.State = status

	var title string
	if weeklyPct > 0 || fiveHourPct > 0 {
		title = fmt.Sprintf("[%s %s · %s · 📊 %.0f%% Wk · %.0f%% 5h · %s]", p.Role.Icon, p.Role.Name, p.Role.Profile, weeklyPct, fiveHourPct, status)
	} else {
		title = fmt.Sprintf("[%s %s · %s · %s]", p.Role.Icon, p.Role.Name, p.Role.Profile, status)
	}
	_ = exec.Command("tmux", "select-pane", "-t", p.ID, "-T", title).Run()

	var colorStyle string
	switch color {
	case "green":
		colorStyle = "fg=green,bold"
	case "red":
		colorStyle = "fg=red,bold"
	case "yellow":
		colorStyle = "fg=yellow,bold"
	default:
		colorStyle = "fg=cyan"
	}
	_ = exec.Command("tmux", "select-pane", "-t", p.ID, "-P", colorStyle).Run()
}

// UpdatePaneStatus atualiza o título e cor da borda de um painel específico.
func (d *Driver) UpdatePaneStatus(role config.RoleType, status string, color string) {
	d.UpdatePaneWithQuota(role, status, color, 0, 0)
}

// UpdateStatusBar atualiza o status-right do Tmux com dados agregados do cluster.
func (d *Driver) UpdateStatusBar(clusterInfo string) {
	rightText := fmt.Sprintf(" %s │ agy-swarm ", clusterInfo)
	_ = exec.Command("tmux", "set-option", "-t", d.SessionName, "status-right", rightText).Run()
}

var (
	ansiRegex = regexp.MustCompile(`\x1b(\[[0-9;]*[a-zA-Z~]|\][^\x07\x1b]*(\x07|\x1b\\)|[PX^_][^\x1b]*\x1b\\|[()][AB012])`)

	// AllowedControlKeys define a allowlist estrita de teclas de controle autorizadas.
	AllowedControlKeys = map[string]bool{
		"Enter":  true,
		"Escape": true,
		"C-c":    true,
	}
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

// SendLiteral envia texto bruto sanitizado para o PTY de um pane específico com proteção contra flag e key injection.
func (d *Driver) SendLiteral(role config.RoleType, text string) error {
	p, ok := d.Panes[role]
	if !ok || p.ID == "" {
		return fmt.Errorf("painel para papel %s não encontrado", role)
	}

	sanitized := SanitizeInput(text)
	cmd := exec.Command("tmux", "send-keys", "-t", p.ID, "-l", "--", sanitized)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("erro no send-keys literal para pane %s: %s (%w)", p.ID, stderr.String(), err)
	}
	return nil
}

// SendControl envia teclas de controle permitidas pela allowlist estrita.
func (d *Driver) SendControl(role config.RoleType, key string) error {
	p, ok := d.Panes[role]
	if !ok || p.ID == "" {
		return fmt.Errorf("painel para papel %s não encontrado", role)
	}

	if !AllowedControlKeys[key] {
		return fmt.Errorf("tecla de controle não permitida: %q (permitidas: Enter, Escape, C-c)", key)
	}

	cmd := exec.Command("tmux", "send-keys", "-t", p.ID, key)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("erro ao enviar tecla de controle %s para pane %s: %s (%w)", key, p.ID, stderr.String(), err)
	}
	return nil
}

// IsPaneIdle verifica se o painel está ocioso inspecionando as últimas linhas do buffer via capture-pane.
func (d *Driver) IsPaneIdle(role config.RoleType) (bool, error) {
	p, ok := d.Panes[role]
	if !ok || p.ID == "" {
		return false, fmt.Errorf("painel para papel %s não encontrado", role)
	}

	out, err := exec.Command("tmux", "capture-pane", "-p", "-t", p.ID, "-S", "-5").Output()
	if err != nil {
		return false, fmt.Errorf("falha ao capturar buffer do pane %s: %w", p.ID, err)
	}

	content := strings.ToLower(string(out))
	busyIndicators := []string{
		"(esc to cancel)",
		"thinking...",
		"running tool",
		"executing",
		"generating...",
	}
	for _, ind := range busyIndicators {
		if strings.Contains(content, ind) {
			return false, nil
		}
	}
	return true, nil
}

// SendKeys envia comandos diretamente para o PTY combinando envio literal sanitizado e tecla Enter,
// aguardando brevemente se o painel estiver no meio de uma geração ou tool.
func (d *Driver) SendKeys(role config.RoleType, text string) error {
	// Verifica prontidão do pane antes de injetar caracteres
	if idle, err := d.IsPaneIdle(role); err == nil && !idle {
		for i := 0; i < 20; i++ {
			time.Sleep(150 * time.Millisecond)
			if idle, err := d.IsPaneIdle(role); err == nil && idle {
				break
			}
		}
	}

	if err := d.SendLiteral(role, text); err != nil {
		return err
	}
	return d.SendControl(role, "Enter")
}

// IsPaneAlive verifica via 'tmux list-panes' se o processo filho do painel ainda está ativo.
func (d *Driver) IsPaneAlive(role config.RoleType) (bool, error) {
	p, ok := d.Panes[role]
	if !ok || p.ID == "" {
		return false, fmt.Errorf("painel para papel %s não encontrado", role)
	}

	out, err := exec.Command("tmux", "list-panes", "-t", d.SessionName, "-F", "#{pane_id}:#{pane_dead}").Output()
	if err != nil {
		return false, fmt.Errorf("falha ao listar panes da sessão %s: %w", d.SessionName, err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		parts := strings.Split(strings.TrimSpace(line), ":")
		if len(parts) >= 2 && parts[0] == p.ID {
			// pane_dead == "1" indica processo filho finalizado/morto
			return parts[1] == "0", nil
		}
	}

	return false, fmt.Errorf("pane %s não encontrado na sessão %s", p.ID, d.SessionName)
}

