package tmux

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/multiplexer"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/security"
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

	// Para enxames modernos, adota o Layout de 3 Zonas (Maestro + Telemetria + Viewport Dinâmico)
	return d.Build3ZoneLayout(roles)
}

// Build3ZoneLayout implementa o layout moderno de 3 zonas:
// - Esquerda (50% largura): Maestro interativo
// - Top-Right (50% largura, 35% altura): Telemetria viva (dashboard --watch)
// - Bottom-Right (50% largura, 65% altura): Viewport dinâmico dos workers efêmeros
func (d *Driver) Build3ZoneLayout(roles []config.RoleConfig) error {
	pane0 := d.Panes[roles[0].Type].ID

	// 1. Divide a tela na vertical ao meio (50% esquerda, 50% direita)
	out, err := exec.Command("tmux", "split-window", "-h", "-t", pane0, "-c", d.Workdir, "-p", "50", "-P", "-F", "#{pane_id}").Output()
	if err != nil {
		return fmt.Errorf("falha ao dividir coluna direita: %w", err)
	}
	topRight := strings.TrimSpace(string(out))

	// 2. Divide a coluna direita: Top-Right (35%) e Bottom-Right (65%)
	out, err = exec.Command("tmux", "split-window", "-v", "-t", topRight, "-c", d.Workdir, "-p", "65", "-P", "-F", "#{pane_id}").Output()
	if err != nil {
		return fmt.Errorf("falha ao dividir viewport dinâmico inferior direito: %w", err)
	}
	bottomRight := strings.TrimSpace(string(out))

	// Configura Top-Right como Painel de Telemetria Viva
	telemetryRole := config.RoleConfig{
		Type:    "telemetry",
		Name:    "Telemetria-Viva",
		Icon:    "📊",
		Profile: "cluster",
	}
	d.Panes[telemetryRole.Type] = &PaneInfo{
		ID:       topRight,
		Role:     telemetryRole,
		State:    "Telemetria",
		Worktree: d.Workdir,
	}
	_ = exec.Command("tmux", "select-pane", "-t", topRight, "-T", "[📊 TELEMETRIA DO CLUSTER · agy-hub]").Run()

	// Configura Bottom-Right como Viewport Dinâmico
	viewportRole := config.RoleConfig{
		Type:    "viewport",
		Name:    "Worker-Viewport",
		Icon:    "⚡",
		Profile: "pool",
	}
	d.Panes[viewportRole.Type] = &PaneInfo{
		ID:       bottomRight,
		Role:     viewportRole,
		State:    "Aguardando Tarefa",
		Worktree: d.Workdir,
	}
	_ = exec.Command("tmux", "select-pane", "-t", bottomRight, "-T", "[⚡ WORKER ATIVO · Viewport Dinâmico]").Run()

	// Mapeia todos os especialistas efêmeros para o BottomRight Viewport
	// para que qualquer envio de comando ou status seja refletido na tela ativa
	for i := 1; i < len(roles); i++ {
		r := roles[i]
		d.Panes[r.Type] = &PaneInfo{
			ID:       bottomRight,
			Role:     r,
			State:    "Efêmero",
			Worktree: d.Workdir,
		}
	}

	// Seleciona o pane 0 (Maestro) como foco padrão para o usuário
	_ = exec.Command("tmux", "select-pane", "-t", pane0).Run()

	d.SetupHUD()
	return nil
}

// SwitchViewportRole atualiza o título e o papel em exibição no Viewport Dinâmico.
func (d *Driver) SwitchViewportRole(role config.RoleConfig) {
	p, ok := d.Panes[role.Type]
	if !ok || p.ID == "" {
		return
	}
	title := fmt.Sprintf("[%s %s (%s) · Executando Estágio]", role.Icon, role.Name, role.Profile)
	_ = exec.Command("tmux", "select-pane", "-t", p.ID, "-T", title).Run()
	_ = exec.Command("tmux", "select-pane", "-t", p.ID, "-P", "fg=green,bold").Run()
}

// SetupHUD configura títulos, bordas, mouse e atalhos de navegação flutuante no Tmux.
func (d *Driver) SetupHUD() {
	_ = exec.Command("tmux", "set-option", "-t", d.SessionName, "pane-border-status", "top").Run()
	_ = exec.Command("tmux", "set-option", "-t", d.SessionName, "pane-border-format", " #{pane_title} ").Run()
	_ = exec.Command("tmux", "set-option", "-t", d.SessionName, "pane-border-lines", "heavy").Run()

	// Habilita suporte nativo ao mouse (clique de painel, redimensionamento de divisão e scroll de logs)
	_ = exec.Command("tmux", "set-option", "-t", d.SessionName, "mouse", "on").Run()

	// Atalhos flutuantes ergonômicos sem prefixo (Alt / Meta)
	_ = exec.Command("tmux", "bind-key", "-n", "M-k", "display-popup", "-E", "-w", "85%", "-h", "75%", "agy-swarm kanban status | less -R").Run()
	_ = exec.Command("tmux", "bind-key", "-n", "M-d", "display-popup", "-E", "-w", "90%", "-h", "85%", "agy-swarm diff | less -R").Run()
	_ = exec.Command("tmux", "bind-key", "-n", "M-w", "last-window").Run()

	// Legenda padrão de atalhos na barra de status
	_ = exec.Command("tmux", "set-option", "-t", d.SessionName, "status-right-length", "90").Run()
	_ = exec.Command("tmux", "set-option", "-t", d.SessionName, "status-right", " Alt+k: Kanban │ Alt+d: Diffs │ Alt+w: Wave Grid │ Mouse: ON ").Run()

	// Prioriza e fixa os títulos dos 3 painéis canônicos da interface
	if p, ok := d.Panes[config.RoleLead]; ok {
		d.UpdatePaneStatus(p.Role.Type, p.State, "blue")
	}
	if p, ok := d.Panes["telemetry"]; ok {
		d.UpdatePaneStatus(p.Role.Type, p.State, "blue")
	}
	if p, ok := d.Panes["viewport"]; ok {
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

// UpdateStatusBar atualiza o status-right do Tmux com dados agregados do cluster e atalhos rápidos.
func (d *Driver) UpdateStatusBar(clusterInfo string) {
	rightText := fmt.Sprintf(" %s │ Alt+k: Kanban │ Alt+d: Diffs │ Alt+w: Wave Grid ", clusterInfo)
	_ = exec.Command("tmux", "set-option", "-t", d.SessionName, "status-right-length", "90").Run()
	_ = exec.Command("tmux", "set-option", "-t", d.SessionName, "status-right", rightText).Run()
}

var (
	// AllowedControlKeys define a allowlist estrita de teclas de controle autorizadas.
	AllowedControlKeys = map[string]bool{
		"Enter":  true,
		"Escape": true,
		"C-c":    true,
	}
)

// SanitizeInput remove bytes nulos, sequências de escape ANSI e normaliza quebras de linha via internal/security.
func SanitizeInput(s string) string {
	return security.SanitizeInput(s)
}

// SendLiteral envia texto bruto sanitizado para o PTY de um pane específico com proteção contra flag e key injection.
func (d *Driver) SendLiteral(role config.RoleType, text string) error {
	p, ok := d.Panes[role]
	if !ok || p.ID == "" {
		return fmt.Errorf("painel para papel %s não encontrado", role)
	}

	// Proteção contra colisão no layout de 3 zonas:
	// Se o painel for compartilhado com o viewport, silencia notificações direcionadas a especialistas
	if vp, ok := d.Panes["viewport"]; ok && vp.ID != "" && p.ID == vp.ID && role != "viewport" {
		return nil
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

// SavePanesToRuntime persiste o mapeamento de papéis e IDs de painéis em runtimeDir de forma protegida.
func (d *Driver) SavePanesToRuntime(runtimeDir string) error {
	panesMap := make(map[string]string)
	for rt, p := range d.Panes {
		if p != nil && p.ID != "" {
			panesMap[string(rt)] = p.ID
		}
	}
	data, err := json.Marshal(panesMap)
	if err != nil {
		return err
	}
	targetFile := filepath.Join(runtimeDir, fmt.Sprintf("%s-panes.json", d.SessionName))
	return security.WriteRestrictedFile(targetFile, data)
}

// HydratePanesFromRuntime restaura o mapa de painéis a partir de runtimeDir ou inspeciona o Tmux ativo.
func (d *Driver) HydratePanesFromRuntime(runtimeDir string) error {
	targetFile := filepath.Join(runtimeDir, fmt.Sprintf("%s-panes.json", d.SessionName))
	if data, err := os.ReadFile(targetFile); err == nil && len(data) > 0 {
		var panesMap map[string]string
		if err := json.Unmarshal(data, &panesMap); err == nil && len(panesMap) > 0 {
			allRoles := config.AllRoles()
			for rtStr, paneID := range panesMap {
				rt := config.RoleType(rtStr)
				var roleCfg config.RoleConfig
				found := false
				for _, r := range allRoles {
					if r.Type == rt {
						roleCfg = r
						found = true
						break
					}
				}
				if !found {
					roleCfg = config.RoleConfig{Type: rt, Name: rtStr}
				}
				d.Panes[rt] = &PaneInfo{
					ID:       paneID,
					Role:     roleCfg,
					Worktree: d.Workdir,
				}
			}
			return nil
		}
	}

	// Fallback dinâmico: consulta o Tmux ativo diretamente
	out, err := exec.Command("tmux", "list-panes", "-t", d.SessionName, "-F", "#{pane_id}:#{pane_index}").Output()
	if err != nil {
		return fmt.Errorf("falha ao consultar panes da sessão %s: %w", d.SessionName, err)
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Split(strings.TrimSpace(line), ":")
		if len(parts) >= 2 {
			pID := parts[0]
			pIdx := parts[1]
			// Painel 0 no layout de 3 zonas é o Maestro (RoleLead)
			if pIdx == "0" {
				leadRole := config.RoleConfig{Type: config.RoleLead, Name: "Maestro", Project: "swarm-lead"}
				d.Panes[config.RoleLead] = &PaneInfo{
					ID:       pID,
					Role:     leadRole,
					Worktree: d.Workdir,
				}
			}
		}
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

