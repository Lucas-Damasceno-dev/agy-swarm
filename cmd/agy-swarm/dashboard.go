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
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/lease"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/memory"
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
	var buf bytes.Buffer
	// Posiciona cursor no início e limpa a tela visível
	buf.WriteString("\033[H\033[2J")

	renderCompactDashboard(&buf, rows, cols)

	_, _ = os.Stdout.Write(buf.Bytes())
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
	var activeLeases []string

	for _, s := range snaps {
		if s.Status == "BUSY" {
			busyCount++
			activeLeases = append(activeLeases, fmt.Sprintf("• %s: %s (%s)", s.AccountID, s.CurrentRole, s.CurrentStage))
		} else if s.Status == "COOLDOWN" {
			cooldownCount++
		} else {
			freeCount++
		}
	}

	sepLen := cols - 1
	if sepLen > 68 {
		sepLen = 68
	}
	if sepLen < 20 {
		sepLen = 20
	}
	sep := strings.Repeat("─", sepLen)

	fmt.Fprintf(w, "⚡ AGY-SWARM // CLUSTER (%d CONTAS)\n", len(snaps))
	if hubClient.IsAvailable() {
		if st, err := hubClient.GetClusterState(); err == nil {
			fmt.Fprintf(w, "Cota: %dM/%dM (%d%%) │ Reset: %s\n",
				st.ClusterRemaining/1000000, st.ClusterQuotaTokens/1000000, st.ClusterQuotaPercent, st.ResetCountdown)
		}
	} else {
		statusStr := "⚪ INATIVA"
		if running {
			statusStr = "🟢 ATIVA"
		}
		fmt.Fprintf(w, "Sessão: %s (%s)\n", session, statusStr)
	}

	fmt.Fprintf(w, "Pool: 🟢 %d Livres │ 🟡 %d Ocupadas │ 🔴 %d Cooldown\n", freeCount, busyCount, cooldownCount)
	fmt.Fprintln(w, sep)

	linesUsed := 5
	if len(activeLeases) > 0 && linesUsed < rows-4 {
		fmt.Fprintln(w, "⚡ LEASES ATIVOS:")
		linesUsed++
		for _, al := range activeLeases {
			if linesUsed >= rows-4 {
				break
			}
			fmt.Fprintln(w, al)
			linesUsed++
		}
		fmt.Fprintln(w, sep)
		linesUsed++
	}

	fmt.Fprintln(w, "👥 WORKTREES & BRANCHES:")
	linesUsed++

	for _, r := range config.AllRoles() {
		if linesUsed >= rows-1 {
			break
		}
		branchName := "main"
		diffSummary := "limpo"

		if r.Type != config.RoleLead {
			branchName = fmt.Sprintf("swarm/%s", r.Type)
			checkCmd := exec.Command("git", "rev-parse", "--verify", branchName)
			checkCmd.Dir = cwd
			if err := checkCmd.Run(); err != nil {
				continue
			}

			cCmd := exec.Command("git", "rev-list", "--count", "HEAD.."+branchName)
			cCmd.Dir = cwd
			if out, err := cCmd.Output(); err == nil {
				ahead := strings.TrimSpace(string(out))
				if ahead != "" && ahead != "0" {
					branchName = fmt.Sprintf("%s (+%s)", branchName, ahead)
					diffSummary = fmt.Sprintf("+%sc", ahead)
				}
			}
		}

		fmt.Fprintf(w, "%s %-10s: %-16s (%s)\n", r.Icon, r.Name, branchName, diffSummary)
		linesUsed++
	}
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
	fmt.Printf("%-18s %-14s %-20s %-12s\n", "PAPEL", "ai-memory", "BRANCH GIT", "DIFF STAT")
	fmt.Println(strings.Repeat("─", 68))

	memClient := memory.NewClient()
	for _, r := range config.AllRoles() {
		branchName := "main"
		diffSummary := "limpo"

		if r.Type != config.RoleLead {
			branchName = fmt.Sprintf("swarm/%s", r.Type)
			// Só exibe se o branch existir no repositório
			checkCmd := exec.Command("git", "rev-parse", "--verify", branchName)
			checkCmd.Dir = cwd
			if err := checkCmd.Run(); err != nil {
				continue
			}

			cCmd := exec.Command("git", "rev-list", "--count", "HEAD.."+branchName)
			cCmd.Dir = cwd
			if out, err := cCmd.Output(); err == nil {
				ahead := strings.TrimSpace(string(out))
				if ahead != "" && ahead != "0" {
					branchName = fmt.Sprintf("%s (+%s)", branchName, ahead)
					diffSummary = fmt.Sprintf("+%s commit(s)", ahead)
				}
			}
		}

		fmt.Printf("%s %-15s %-14s %-20s %-12s\n",
			r.Icon, r.Name, r.Project, branchName, diffSummary)
	}

	_ = memClient
	fmt.Println()
	fmt.Println("Controles Rápidos:")
	fmt.Println("  agy-swarm attach      - Conectar ao terminal do enxame")
	fmt.Println("  agy-swarm diff        - Inspecionar diffs cumulativos dos especialistas")
	fmt.Println("  agy-swarm stop        - Encerramento e teardown seguro")
}
