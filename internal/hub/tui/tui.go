package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/collector"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/daemon"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/model"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/orchestrator"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/syncer"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/kanban"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ViewMode int

const (
	ViewDashboard ViewMode = iota
	ViewPromptModal
	ViewLogModal
	ViewKanban
)

type tickMsg time.Time
type statusMsg string
type stateLoadedMsg struct {
	state *model.ClusterState
	err   error
}
type syncResultMsg struct {
	synced []string
	err    error
}
type proxyRestartResultMsg struct {
	err error
}

func fetchStateCmd(c *collector.Collector) tea.Cmd {
	return func() tea.Msg {
		if st, err := daemon.FetchStateFromDaemon(); err == nil {
			return stateLoadedMsg{state: st}
		}
		st, err := c.CollectAll()
		return stateLoadedMsg{state: st, err: err}
	}
}

func syncAccountsCmd(s *syncer.Syncer, profiles []*model.Profile) tea.Cmd {
	return func() tea.Msg {
		synced, err := s.SyncAllAccounts(profiles)
		if err == nil {
			_ = s.EnsureProxyRunning()
		}
		return syncResultMsg{synced: synced, err: err}
	}
}

func restartProxyCmd(s *syncer.Syncer) tea.Cmd {
	return func() tea.Msg {
		err := s.RestartProxyService()
		return proxyRestartResultMsg{err: err}
	}
}

type Model struct {
	collector    *collector.Collector
	orchestrator *orchestrator.Orchestrator
	syncer       *syncer.Syncer
	state        *model.ClusterState
	cursor       int
	viewMode     ViewMode
	err          error
	statusText   string
	statusTime   time.Time
	width        int
	height       int
	textInput    textinput.Model
}

func NewModel() Model {
	ti := textinput.New()
	ti.Placeholder = "Instrução para dispatch automático na conta mais livre..."
	ti.CharLimit = 512
	ti.Width = 60

	c := collector.NewCollector()
	orch := orchestrator.NewOrchestrator()
	sync := syncer.NewSyncer()

	return Model{
		collector:    c,
		orchestrator: orch,
		syncer:       sync,
		state:        nil,
		textInput:    ti,
		viewMode:     ViewDashboard,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(fetchStateCmd(m.collector), tickCmd(), textinput.Blink)
}

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.textInput.Width = max(30, msg.Width-30)

	case tickMsg:
		if m.viewMode == ViewDashboard {
			return m, tea.Batch(fetchStateCmd(m.collector), tickCmd())
		}
		return m, tickCmd()

	case stateLoadedMsg:
		if msg.err == nil && msg.state != nil {
			m.state = msg.state
		} else if msg.err != nil && m.state == nil {
			m.statusText = fmt.Sprintf("⚠️ Erro: %v", msg.err)
			m.statusTime = time.Now()
		}
		return m, nil

	case syncResultMsg:
		if msg.err != nil {
			m.statusText = fmt.Sprintf("⚠️ Erro no sync: %v", msg.err)
		} else {
			m.statusText = fmt.Sprintf("✓ Sincronizadas %d contas no CLI-Proxy", len(msg.synced))
		}
		m.statusTime = time.Now()
		return m, fetchStateCmd(m.collector)

	case proxyRestartResultMsg:
		if msg.err != nil {
			m.statusText = fmt.Sprintf("⚠️ Falha proxy: %v", msg.err)
		} else {
			m.statusText = "✓ Serviço cli-proxy-api reiniciado (round-robin ativo)"
		}
		m.statusTime = time.Now()
		return m, nil

	case statusMsg:
		m.statusText = string(msg)
		m.statusTime = time.Now()

	case tea.KeyMsg:
		// Modal Key Handling
		if m.viewMode == ViewPromptModal {
			switch msg.String() {
			case "enter":
				val := strings.TrimSpace(m.textInput.Value())
				m.viewMode = ViewDashboard
				m.textInput.Reset()
				if val != "" {
					return m.handleDispatchPrompt(val)
				}
				return m, nil
			case "esc":
				m.viewMode = ViewDashboard
				m.textInput.Reset()
				return m, nil
			default:
				var cmd tea.Cmd
				m.textInput, cmd = m.textInput.Update(msg)
				return m, cmd
			}
		}

		if m.viewMode == ViewLogModal {
			switch msg.String() {
			case "esc", "q", "l":
				m.viewMode = ViewDashboard
				return m, nil
			}
			return m, nil
		}

		if m.viewMode == ViewKanban {
			switch msg.String() {
			case "esc", "q", "b", "tab":
				m.viewMode = ViewDashboard
				return m, nil
			case "r":
				m.statusText = "Recarregando quadro Kanban..."
				m.statusTime = time.Now()
				return m, nil
			case "ctrl+c":
				return m, tea.Quit
			}
			return m, nil
		}

		// Dashboard Key Handling
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "b", "tab":
			m.viewMode = ViewKanban
			return m, nil

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			} else if m.state != nil && len(m.state.Profiles) > 0 {
				m.cursor = len(m.state.Profiles) - 1
			}

		case "down", "j":
			if m.state != nil && len(m.state.Profiles) > 0 {
				if m.cursor < len(m.state.Profiles)-1 {
					m.cursor++
				} else {
					m.cursor = 0
				}
			}

		case "r":
			m.statusText = "Atualizando telemetria..."
			m.statusTime = time.Now()
			return m, fetchStateCmd(m.collector)

		case "l":
			m.viewMode = ViewLogModal
			return m, nil

		case "L", "a":
			if m.state != nil && len(m.state.Profiles) > m.cursor {
				p := m.state.Profiles[m.cursor]
				cmd := createAgyLoginCommand(p)
				return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
					if err != nil {
						return statusMsg(fmt.Sprintf("Erro no login: %v", err))
					}
					return statusMsg(fmt.Sprintf("✓ Login concluído para %s", p.Alias))
				})
			}

		case "s":
			if m.state != nil {
				m.statusText = "Sincronizando contas com CLI-Proxy..."
				m.statusTime = time.Now()
				return m, syncAccountsCmd(m.syncer, m.state.Profiles)
			}

		case "p":
			m.statusText = "Reiniciando serviço cli-proxy-api..."
			m.statusTime = time.Now()
			return m, restartProxyCmd(m.syncer)

		case "d", "e":
			m.viewMode = ViewPromptModal
			m.textInput.Focus()
			return m, textinput.Blink

		case "enter":
			if m.state != nil && len(m.state.Profiles) > m.cursor {
				p := m.state.Profiles[m.cursor]
				m.statusText = fmt.Sprintf("Iniciando %s...", p.Alias)
				m.statusTime = time.Now()
				return m, tea.ExecProcess(createAgyCommand(p), func(err error) tea.Msg {
					if err != nil {
						return statusMsg(fmt.Sprintf("Erro na sessão: %v", err))
					}
					return statusMsg(fmt.Sprintf("Sessão %s finalizada.", p.Alias))
				})
			}
		}
	}

	return m, nil
}

func (m Model) handleDispatchPrompt(prompt string) (tea.Model, tea.Cmd) {
	if m.state == nil || len(m.state.Profiles) == 0 {
		m.statusText = "Nenhum perfil ativo para dispatch."
		m.statusTime = time.Now()
		return m, nil
	}

	best, reason, err := m.orchestrator.SelectBest(m.state.Profiles, orchestrator.StrategyLeastLoaded)
	if err != nil {
		m.statusText = fmt.Sprintf("Falha no orchestrator: %v", err)
		m.statusTime = time.Now()
		return m, nil
	}

	m.statusText = fmt.Sprintf("Orquestrador despachou para %s (%s)", best.Alias, reason)
	m.statusTime = time.Now()

	cmd := createAgyPromptCommand(best, prompt)
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return statusMsg(fmt.Sprintf("Execução falhou: %v", err))
		}
		return statusMsg(fmt.Sprintf("Prompt finalizado em %s.", best.Alias))
	})
}

func (m Model) View() string {
	if m.state == nil {
		return "Carregando telemetria dos perfis AGY...\n"
	}

	// Live Log Modal View
	if m.viewMode == ViewLogModal {
		return m.renderLogModal()
	}

	// Kanban View
	if m.viewMode == ViewKanban {
		return m.renderKanbanView()
	}

	var b strings.Builder

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#00FFA3")).
		Background(lipgloss.Color("#1A1B26")).
		Padding(0, 1)

	subTitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#7AA2F7"))

	statusBadgeStyle := lipgloss.NewStyle().
		Bold(true).
		Padding(0, 1)

	b.WriteString(titleStyle.Render("⚡ AGY-HUB // Antigravity & Hermes Multi-Account Orchestrator"))
	b.WriteString("\n")

	var proxyBadge string
	if m.state.ProxyRunning {
		proxyBadge = statusBadgeStyle.Copy().Background(lipgloss.Color("#164E63")).Foreground(lipgloss.Color("#38BDF8")).Render(
			fmt.Sprintf("● PROXY 127.0.0.1:%d [ONLINE - PID %d]", m.state.ProxyPort, m.state.ProxyPID),
		)
	} else {
		proxyBadge = statusBadgeStyle.Copy().Background(lipgloss.Color("#7F1D1D")).Foreground(lipgloss.Color("#FCA5A5")).Render(
			fmt.Sprintf("○ PROXY 127.0.0.1:%d [OFFLINE]", m.state.ProxyPort),
		)
	}

	clusterStats := fmt.Sprintf("Contas: %d  |  Total Steps: %d  |  Jobs Ativos: %d",
		len(m.state.Profiles), m.state.TotalSteps, m.state.ActiveJobs)

	b.WriteString(fmt.Sprintf("%s  %s\n\n", proxyBadge, subTitleStyle.Render(clusterStats)))

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#C0CAF5")).
		Background(lipgloss.Color("#24283B"))

	rowHeader := fmt.Sprintf(" %-3s %-7s %-25s %-12s %-6s %-7s %-10s %-8s %-10s",
		"", "ALIAS", "EMAIL / ID", "STATUS", "RPM", "STEPS", "EST.TOK(24h)", "PROXY", "24h SPARK")
	b.WriteString(headerStyle.Render(rowHeader))
	b.WriteString("\n")

	for i, p := range m.state.Profiles {
		isSelected := i == m.cursor

		pointer := "  "
		if isSelected {
			pointer = "▶ "
		}

		statusCol := renderStatusBadge(p.Status)
		emailCol := p.Email
		if emailCol == "" {
			emailCol = fmt.Sprintf("[%s]", p.ID)
		}
		if len(emailCol) > 24 {
			emailCol = emailCol[:21] + "..."
		}

		proxyCol := "○ NO"
		if p.ProxySynced {
			proxyCol = "● SYNC"
		}

		spark := p.Sparkline
		if spark == "" {
			spark = "─"
		}

		tokK := fmt.Sprintf("%dk", p.EstTokens24h/1000)
		if p.EstTokens24h > 1000000 {
			tokK = fmt.Sprintf("%.1fM", float64(p.EstTokens24h)/1000000)
		}

		line := fmt.Sprintf(" %-2s %-7s %-25s %-12s %-6d %-7d %-12s %-8s %s",
			pointer, p.Alias, emailCol, statusCol, p.RPM, p.TotalSteps, tokK, proxyCol, spark)

		rowStyle := lipgloss.NewStyle()
		if isSelected {
			rowStyle = rowStyle.
				Bold(true).
				Foreground(lipgloss.Color("#F1F5F9")).
				Background(lipgloss.Color("#334155"))
		} else if i%2 == 1 {
			rowStyle = rowStyle.Background(lipgloss.Color("#0F172A"))
		}

		b.WriteString(rowStyle.Render(line))
		b.WriteString("\n")
	}

	if len(m.state.Profiles) > m.cursor {
		selected := m.state.Profiles[m.cursor]
		detailBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#475569")).
			Padding(0, 1).
			MarginTop(1)

		var details strings.Builder
		details.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#38BDF8")).
			Render(fmt.Sprintf("Detalhes: %s (%s)", selected.Alias, selected.ID)))
		details.WriteString("\n")
		details.WriteString(fmt.Sprintf("• Email: %s  |  Prioridade: %d  |  Tokens Estimados (24h): ~%d\n",
			selected.Email, selected.Priority, selected.EstTokens24h))
		details.WriteString(fmt.Sprintf("• Diretório: %s\n", selected.HomeDir))
		details.WriteString(fmt.Sprintf("• Estado: %s - %s\n", selected.Status, selected.StatusReason))

		if len(selected.RunningPIDs) > 0 {
			details.WriteString(fmt.Sprintf("• Processos Ativos (PIDs): %v\n", selected.RunningPIDs))
		}

		if len(selected.RecentConversations) > 0 {
			details.WriteString("• Conversas Recentes:\n")
			for _, c := range selected.RecentConversations[:min(3, len(selected.RecentConversations))] {
				title := c.Title
				if title == "" {
					title = c.ID[:8]
				}
				if len(title) > 38 {
					title = title[:35] + "..."
				}
				details.WriteString(fmt.Sprintf("   - %s (%d steps, %s)\n", title, c.StepCount, c.LastModified.Format("15:04:05 02/01")))
			}
		}

		b.WriteString(detailBox.Render(details.String()))
		b.WriteString("\n")
	}

	best, reason, _ := m.orchestrator.PreviewBest(m.state.Profiles, orchestrator.StrategyLeastLoaded)
	if best != nil {
		recStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FCD34D")).
			Bold(true)
		b.WriteString(recStyle.Render(fmt.Sprintf("⚡ Recomendação do Orquestrador: %s (%s)", best.Alias, reason)))
		b.WriteString("\n")
	}

	if m.viewMode == ViewPromptModal {
		inputBox := lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color("#A855F7")).
			Padding(1).
			MarginTop(1)

		modalContent := fmt.Sprintf(
			"🎯 DISPATCH ORQUESTRADO (Execução na conta com menor carga)\n\n%s\n\n[Enter] Executar   [Esc] Cancelar",
			m.textInput.View(),
		)
		b.WriteString(inputBox.Render(modalContent))
		b.WriteString("\n")
	}

	if m.statusText != "" && time.Since(m.statusTime) < 5*time.Second {
		noteStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#22C55E")).
			Bold(true)
		b.WriteString(fmt.Sprintf("\n%s\n", noteStyle.Render(m.statusText)))
	}

	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#94A3B8")).
		MarginTop(1)

	footer := "[b/Tab] Kanban • [Enter] Sessão • [d] Dispatch • [l] Logs • [L] Login • [s] Sync • [p] Proxy • [r] Refresh • [q] Sair"
	b.WriteString(footerStyle.Render(footer))

	return b.String()
}

func (m Model) renderLogModal() string {
	var selected *model.Profile
	if len(m.state.Profiles) > m.cursor {
		selected = m.state.Profiles[m.cursor]
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("#38BDF8")).
		Padding(1).
		Width(max(60, m.width-4))

	var b strings.Builder
	title := fmt.Sprintf("📜 LIVE LOG STREAM - %s (%s)", selected.Alias, selected.ID)
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#38BDF8")).Render(title))
	b.WriteString("\n\n")

	if len(selected.RecentLogLines) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#64748B")).Render("Nenhum log recente em cli.log."))
	} else {
		for _, line := range selected.RecentLogLines {
			lineStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8"))
			if strings.Contains(line, "streamGenerateContent") {
				lineStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#38BDF8"))
			} else if strings.Contains(line, "429") || strings.Contains(line, "RESOURCE_EXHAUSTED") {
				lineStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Bold(true)
			} else if strings.Contains(line, "HandleUserInput") {
				lineStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#A855F7"))
			}
			b.WriteString(lineStyle.Render(line) + "\n")
		}
	}

	b.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("#64748B")).Render("[Esc / l] Fechar visualizador de logs"))
	return box.Render(b.String())
}

func renderStatusBadge(st model.AccountStatus) string {
	switch st {
	case model.StatusActive:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#4ADE80")).Bold(true).Render("🟢 ATIVO")
	case model.StatusCooldown:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#F87171")).Bold(true).Render("🔴 429 COOL")
	case model.StatusError:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Bold(true).Render("⚠️ ERRO")
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8")).Render("⚪ IDLE")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (m Model) renderKanbanView() string {
	cwd, _ := os.Getwd()
	boardPath := kanban.DefaultBoardPath(cwd)
	board, err := kanban.LoadBoard(boardPath)

	var b strings.Builder

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#38BDF8")).
		Background(lipgloss.Color("#1E293B")).
		Padding(0, 1)

	b.WriteString(titleStyle.Render("📋 AGY-SWARM KANBAN // Speculative Execution Board"))
	b.WriteString("\n\n")

	if err != nil || board == nil || len(board.Cards) == 0 {
		emptyBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#475569")).
			Padding(1, 2)

		msg := fmt.Sprintf("Nenhum quadro Kanban ativo em:\n  %s\n\nPara decompor um épico em DAG especulativa:\n  $ agy-swarm kanban plan \"Descreva seu épico\"\n\nOu adicione um cartão manualmente:\n  $ agy-swarm kanban add -title \"Feature\" -scope \"internal/pkg\"", boardPath)
		b.WriteString(emptyBox.Render(msg))
		b.WriteString("\n\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8")).Render("[b / Tab / Esc] Voltar ao Dashboard • [r] Recarregar • [q] Sair"))
		return b.String()
	}

	epicStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FCD34D")).Bold(true)
	b.WriteString(fmt.Sprintf("Épico: %s (Total: %d cartões)\n\n", epicStyle.Render(board.Epic), len(board.Cards)))

	backlog := make([]kanban.Card, 0)
	inProgress := make([]kanban.Card, 0)
	done := make([]kanban.Card, 0)
	blocked := make([]kanban.Card, 0)

	for _, c := range board.Cards {
		switch c.Status {
		case kanban.StatusBacklog:
			backlog = append(backlog, c)
		case kanban.StatusInProgress:
			inProgress = append(inProgress, c)
		case kanban.StatusDone:
			done = append(done, c)
		case kanban.StatusBlocked, kanban.StatusFailed:
			blocked = append(blocked, c)
		default:
			backlog = append(backlog, c)
		}
	}

	colWidth := max(22, (m.width-12)/4)
	if colWidth > 38 {
		colWidth = 38
	}

	renderCol := func(title string, color string, cards []kanban.Card) string {
		header := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(color)).
			Render(fmt.Sprintf("%s (%d)", title, len(cards)))

		var content strings.Builder
		content.WriteString(header + "\n\n")

		if len(cards) == 0 {
			content.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#64748B")).Render("— vazio —"))
		} else {
			for i, c := range cards {
				if i > 0 {
					content.WriteString("\n")
				}
				cardBox := lipgloss.NewStyle().
					Border(lipgloss.NormalBorder()).
					BorderForeground(lipgloss.Color("#334155")).
					Padding(0, 1).
					Width(colWidth - 4)

				var cardBody strings.Builder
				cardBody.WriteString(lipgloss.NewStyle().Bold(true).Render(c.ID) + "\n")
				cTitle := c.Title
				if len(cTitle) > colWidth-6 && colWidth > 6 {
					cTitle = cTitle[:colWidth-9] + "..."
				}
				cardBody.WriteString(cTitle + "\n")

				if len(c.Scope) > 0 {
					scopeStr := strings.Join(c.Scope, ", ")
					if len(scopeStr) > colWidth-6 && colWidth > 6 {
						scopeStr = scopeStr[:colWidth-9] + "..."
					}
					cardBody.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#64748B")).Render(scopeStr) + "\n")
				}

				if c.Winner != "" {
					cardBody.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#4ADE80")).Render("🏆 "+c.Winner) + "\n")
				}
				if c.Error != "" {
					errMsg := c.Error
					if len(errMsg) > colWidth-6 && colWidth > 6 {
						errMsg = errMsg[:colWidth-9] + "..."
					}
					cardBody.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Render("⚠️ "+errMsg) + "\n")
				}

				content.WriteString(cardBox.Render(cardBody.String()) + "\n")
			}
		}

		colBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(color)).
			Padding(1).
			Width(colWidth).
			Height(max(10, m.height-14))

		return colBox.Render(content.String())
	}

	colBacklog := renderCol("BACKLOG", "#94A3B8", backlog)
	colInProgress := renderCol("IN PROGRESS", "#38BDF8", inProgress)
	colDone := renderCol("DONE", "#4ADE80", done)
	colBlocked := renderCol("BLOCKED", "#F87171", blocked)

	columns := lipgloss.JoinHorizontal(lipgloss.Top, colBacklog, " ", colInProgress, " ", colDone, " ", colBlocked)
	b.WriteString(columns)
	b.WriteString("\n\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8")).Render("[b / Tab / Esc] Voltar ao Dashboard • [r] Recarregar • [q] Sair"))

	return b.String()
}
