package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/agyhub"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/kanban"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/lease"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/tmux"
)

type winsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

func getTerminalSize() (rows, cols int) {
	var ws winsize
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL,
		uintptr(syscall.Stdout),
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(&ws)))
	if err != 0 || ws.Row == 0 || ws.Col == 0 {
		return 24, 80
	}
	return int(ws.Row), int(ws.Col)
}

// handleDashboard renderiza o painel visual de alta densidade no terminal com suporte a --watch.
func handleDashboard() {
	watchMode := false
	for _, arg := range os.Args[2:] {
		if arg == "--watch" || arg == "-w" {
			watchMode = true
		}
	}

	if watchMode {
		renderDashboardLoop()
		return
	}

	renderDashboardOnce()
}

func renderDashboardLoop() {
	// Entra no buffer alternativo (alt-screen) e oculta cursor para eliminar flicker e scrollback
	fmt.Print("\033[?1049h\033[?25l")
	defer fmt.Print("\033[?25h\033[?1049l")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	renderFrame()

	for {
		select {
		case <-sigChan:
			return
		case <-ticker.C:
			renderFrame()
		}
	}
}

func renderFrame() {
	rows, cols := getTerminalSize()
	var rawBuf bytes.Buffer
	renderCompactDashboard(&rawBuf, rows, cols)

	var buf bytes.Buffer
	buf.WriteString("\033[H") // posiciona cursor no topo sem limpar tela

	lines := strings.Split(rawBuf.String(), "\n")
	for i, line := range lines {
		if i == len(lines)-1 && line == "" {
			continue
		}
		buf.WriteString(line)
		buf.WriteString("\033[K\n") // limpa o resto da linha atual
	}
	buf.WriteString("\033[J") // limpa da posição atual até o fim do painel

	_, _ = os.Stdout.Write(buf.Bytes())
}

func renderProgressBar(pct int, width int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := (pct * width) / 100
	empty := width - filled
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", empty) + "]"
}

func renderCompactDashboard(w io.Writer, rows, cols int) {
	session := "agy-swarm"
	cwd, _ := os.Getwd()
	driver := tmux.NewDriver(session, cwd)
	running := driver.SessionExists()

	hubClient := agyhub.NewClient("")
	broker := lease.NewBroker(hubClient)
	snaps := broker.Snapshots()

	busyCount := 0
	cooldownCount := 0
	freeCount := 0

	type activeWorker struct {
		accountID string
		icon      string
		roleName  string
		stage     string
		elapsed   time.Duration
	}
	var activeWorkers []activeWorker

	roleMap := make(map[config.RoleType]config.RoleConfig)
	for _, r := range config.AllRoles() {
		roleMap[r.Type] = r
	}

	for _, s := range snaps {
		if s.Status == "BUSY" {
			busyCount++
			icon := "⚡"
			roleName := string(s.CurrentRole)
			if r, ok := roleMap[s.CurrentRole]; ok {
				icon = r.Icon
				roleName = r.Name
			}
			activeWorkers = append(activeWorkers, activeWorker{
				accountID: s.AccountID,
				icon:      icon,
				roleName:  roleName,
				stage:     s.CurrentStage,
				elapsed:   s.Elapsed,
			})
		} else if s.Status == "COOLDOWN" {
			cooldownCount++
		} else {
			freeCount++
		}
	}

	sepLen := cols - 1
	if sepLen > 60 {
		sepLen = 60
	}
	if sepLen < 20 {
		sepLen = 20
	}
	sep := strings.Repeat("─", sepLen)

	statusIcon := "⚪ INATIVA"
	if running {
		statusIcon = "🟢 ATIVA"
	}
	fmt.Fprintf(w, "⚡ AGY-SWARM // CLUSTER (%d CONTAS)                  %s\n", len(snaps), statusIcon)

	if hubClient.IsAvailable() {
		if st, err := hubClient.GetClusterState(); err == nil {
			bar := renderProgressBar(st.ClusterQuotaPercent, 10)
			fmt.Fprintf(w, "Cota: %s %d%% (%dM/%dM) │ Pool: 🟢 %d  🟡 %d  🔴 %d\n",
				bar, st.ClusterQuotaPercent, st.ClusterRemaining/1000000, st.ClusterQuotaTokens/1000000,
				freeCount, busyCount, cooldownCount)
		} else {
			fmt.Fprintf(w, "Pool: 🟢 %d Livres │ 🟡 %d Ocupadas │ 🔴 %d Cooldown\n", freeCount, busyCount, cooldownCount)
		}
	} else {
		fmt.Fprintf(w, "Pool: 🟢 %d Livres │ 🟡 %d Ocupadas │ 🔴 %d Cooldown\n", freeCount, busyCount, cooldownCount)
	}
	fmt.Fprintln(w, sep)

	boardPath := kanban.DefaultBoardPath(cwd)
	if kb, err := kanban.LoadBoard(boardPath); err == nil && len(kb.Cards) > 0 {
		doneCount := 0
		activeCount := 0
		blockedCount := 0
		var activeCards []kanban.Card

		for _, c := range kb.Cards {
			switch c.Status {
			case kanban.StatusDone:
				doneCount++
			case kanban.StatusInProgress:
				activeCount++
				activeCards = append(activeCards, c)
			case kanban.StatusBlocked:
				blockedCount++
			}
		}

		pct := (doneCount * 100) / len(kb.Cards)
		bar := renderProgressBar(pct, 10)
		fmt.Fprintf(w, "📋 ÉPICO: %s\n", kb.Epic)
		fmt.Fprintf(w, "Progresso: %s %d/%d DONE │ ⚡ %d Ativos │ 🚫 %d Bloq\n", bar, doneCount, len(kb.Cards), activeCount, blockedCount)
		for _, ac := range activeCards {
			scopeStr := ""
			if len(ac.Scope) > 0 {
				scopeStr = fmt.Sprintf(" [%s]", strings.Join(ac.Scope, ","))
			}
			fmt.Fprintf(w, "  ⚡ %s: %s%s\n", ac.ID, ac.Title, scopeStr)
		}
		fmt.Fprintln(w, sep)
	}

	fmt.Fprintln(w, "🔥 LEASES & WORKERS EM EXECUÇÃO:")
	if len(activeWorkers) == 0 {
		fmt.Fprintln(w, "  • Nenhum worker em execução no momento.")
	} else {
		for _, aw := range activeWorkers {
			m := int(aw.elapsed.Minutes())
			s := int(aw.elapsed.Seconds()) % 60
			fmt.Fprintf(w, "  • %-10s ──> %s %-10s (%-14s)   [%02d:%02ds]\n",
				aw.accountID, aw.icon, aw.roleName, aw.stage, m, s)
		}
	}
	fmt.Fprintln(w, sep)

	fmt.Fprintln(w, " Alt+k: Kanban │ Alt+d: Diffs │ Alt+w: Wave Grid │ Mouse: ON")
}

type branchDisplayInfo struct {
	Icon        string
	Name        string
	Project     string
	BranchName  string
	DiffSummary string
}

func getBranchesDisplayInfo(cwd string) []branchDisplayInfo {
	roles := config.AllRoles()
	roleMap := make(map[config.RoleType]config.RoleConfig)
	for _, r := range roles {
		roleMap[r.Type] = r
	}

	var list []branchDisplayInfo

	// 1. Maestro (HEAD principal)
	leadRole := roleMap[config.RoleLead]
	mainBranch := "main"
	if out, err := exec.Command("git", "-C", cwd, "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
		if b := strings.TrimSpace(string(out)); b != "" && b != "HEAD" {
			mainBranch = b
		}
	}
	list = append(list, branchDisplayInfo{
		Icon:        leadRole.Icon,
		Name:        leadRole.Name,
		Project:     leadRole.Project,
		BranchName:  mainBranch,
		DiffSummary: "limpo",
	})

	// 2. Branches sob refs/heads/swarm/ (dinâmicos: tanto swarm/<role> quanto swarm/<card-id>/<role>)
	branchOut, err := exec.Command("git", "-C", cwd, "for-each-ref", "--format=%(refname:short)", "refs/heads/swarm/").Output()
	if err == nil {
		for _, b := range strings.Split(strings.TrimSpace(string(branchOut)), "\n") {
			b = strings.TrimSpace(b)
			if b == "" {
				continue
			}

			parts := strings.Split(b, "/")
			roleKey := config.RoleType(parts[len(parts)-1])
			r, ok := roleMap[roleKey]
			icon := "🌿"
			name := b
			project := b
			if ok {
				icon = r.Icon
				name = r.Name
				project = r.Project
			}

			diffSummary := "limpo"
			cCmd := exec.Command("git", "-C", cwd, "rev-list", "--count", "HEAD.."+b)
			if out, err := cCmd.Output(); err == nil {
				ahead := strings.TrimSpace(string(out))
				if ahead != "" && ahead != "0" {
					diffSummary = fmt.Sprintf("+%sc", ahead)
				}
			}

			list = append(list, branchDisplayInfo{
				Icon:        icon,
				Name:        name,
				Project:     project,
				BranchName:  b,
				DiffSummary: diffSummary,
			})
		}
	}

	return list
}


func renderDashboardOnce() {
	session := "agy-swarm"
	cwd, _ := os.Getwd()
	driver := tmux.NewDriver(session, cwd)
	running := driver.SessionExists()

	hubClient := agyhub.NewClient("")
	broker := lease.NewBroker(hubClient)
	snaps := broker.Snapshots()

	fmt.Println("⚡ AGY-SWARM // TELEMETRIA VIVA DO CLUSTER (10 CONTAS)")
	fmt.Printf("Sessão Tmux: %s  |  Status: ", session)
	if running {
		fmt.Printf("🟢 ATIVA\n")
	} else {
		fmt.Printf("⚪ INATIVA\n")
	}

	// 1. Tabela das 10 Contas com Leases e Cotas
	fmt.Println("\n📊 POOL DE CONTAS GOOGLE AI PRO & LEASES ATIVOS:")
	fmt.Printf("%-10s %-12s %-12s %-10s %-10s %-18s\n", "CONTA", "SEMANAL", "5 HORAS", "SCORE", "ESTADO", "LEASE ATUAL")
	fmt.Println(strings.Repeat("─", 76))

	for _, s := range snaps {
		leaseDesc := "Livre"
		if s.Status == "BUSY" {
			leaseDesc = fmt.Sprintf("⚡ %s (%s)", s.CurrentRole, s.CurrentStage)
		} else if s.Status == "COOLDOWN" {
			leaseDesc = "⏳ Quarentena 429"
		}

		statusIcon := "🟢"
		if s.Status == "BUSY" {
			statusIcon = "🟡"
		} else if s.Status == "COOLDOWN" {
			statusIcon = "🔴"
		}

		fmt.Printf("%-10s %-12s %-12s %-10.1f %s %-8s %-18s\n",
			s.AccountID,
			fmt.Sprintf("%.1f%%", s.WeeklyPercent),
			fmt.Sprintf("%.1f%%", s.FiveHourPercent),
			s.Score,
			statusIcon,
			s.Status,
			leaseDesc,
		)
	}

	if hubClient.IsAvailable() {
		if st, err := hubClient.GetClusterState(); err == nil {
			fmt.Println(strings.Repeat("─", 76))
			fmt.Printf("Cluster Total: %dM / %dM (Reset em %s)\n",
				st.ClusterRemaining/1000000, st.ClusterQuotaTokens/1000000, st.ResetCountdown)
		}
	} else {
		fmt.Println("  (agy-hub daemon offline em /tmp/agy-hub.sock)")
	}

	// 2. Estado dos Branches dos Especialistas
	fmt.Println("\n👥 WORKTREES & BRANCHES GIT:")
	fmt.Printf("%-18s %-14s %-24s %-12s\n", "PAPEL", "ai-memory", "BRANCH GIT", "DIFF STAT")
	fmt.Println(strings.Repeat("─", 72))

	for _, bInfo := range getBranchesDisplayInfo(cwd) {
		displayName := bInfo.BranchName
		statText := bInfo.DiffSummary
		if statText != "limpo" {
			statText = fmt.Sprintf("%s commit(s)", strings.TrimSuffix(statText, "c"))
		}
		fmt.Printf("%s %-15s %-14s %-24s %-12s\n",
			bInfo.Icon, bInfo.Name, bInfo.Project, displayName, statText)
	}
	fmt.Println()
	fmt.Println("Controles Rápidos:")
	fmt.Println("  agy-swarm attach      - Conectar ao terminal do enxame")
	fmt.Println("  agy-swarm diff        - Inspecionar diffs cumulativos dos especialistas")
	fmt.Println("  agy-swarm stop        - Encerramento e teardown seguro")
}
