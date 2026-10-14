package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/agyhub"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/lease"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/memory"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/tmux"
)

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
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Limpa a tela na entrada
	fmt.Print("\033[H\033[2J")
	renderDashboardOnce()

	for range ticker.C {
		fmt.Print("\033[H")
		renderDashboardOnce()
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
