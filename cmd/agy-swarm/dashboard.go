package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/agyhub"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/memory"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/tmux"
)

// handleDashboard renderiza o painel visual de alta densidade no terminal.
func handleDashboard() {
	session := "agy-swarm"
	cwd, _ := os.Getwd()
	driver := tmux.NewDriver(session, cwd)
	running := driver.SessionExists()

	fmt.Println("⚡ AGY-SWARM // DASHBOARD DE CONTROLE & TELEMETRIA")
	fmt.Printf("Sessão Tmux: %s  |  Status: ", session)
	if running {
		fmt.Printf("🟢 ATIVA\n")
	} else {
		fmt.Printf("⚪ INATIVA\n")
	}

	// 1. Telemetria do Cluster de Cotas (agy-hub)
	hubClient := agyhub.NewClient("")
	fmt.Println("\n📊 TELEMETRIA DE COTAS DO CLUSTER (Google AI Pro):")
	fmt.Printf("%-8s %-10s %-14s %-12s %-10s %-10s\n", "ALIAS", "CONTA", "SEMANAL", "5 HORAS", "DIÁRIO", "STATUS")
	fmt.Println(strings.Repeat("─", 68))

	var quotaMap = make(map[string]agyhub.ProfileSummary)
	if hubClient.IsAvailable() {
		state, err := hubClient.GetClusterState()
		if err == nil {
			for _, p := range state.Profiles {
				quotaMap[p.ID] = p
				fmt.Printf("%-8s %-10s %-14s %-12s %-10s %-10s\n",
					p.Alias, p.ID,
					fmt.Sprintf("%.1f%%", p.WeeklyPercent),
					fmt.Sprintf("%.1f%%", p.FiveHourPercent),
					fmt.Sprintf("%d%%", p.QuotaPercent),
					p.Status,
				)
			}
			fmt.Println(strings.Repeat("─", 68))
			fmt.Printf("Cluster Total: %dM / %dM (Reset em %s)\n",
				state.ClusterRemaining/1000000, state.ClusterQuotaTokens/1000000, state.ResetCountdown)
		}
	} else {
		fmt.Println("  (agy-hub daemon offline em /tmp/agy-hub.sock)")
	}

	// 2. Estado dos Agentes do Enxame
	fmt.Println("\n👥 MATRIZ DE AGENTES & WORKTREES:")
	fmt.Printf("%-18s %-10s %-14s %-16s %-12s\n", "AGENTE", "PERFIL", "ai-memory", "BRANCH GIT", "MENSAGENS")
	fmt.Println(strings.Repeat("─", 74))

	memClient := memory.NewClient()
	for _, r := range config.DefaultRoles() {
		// Conta mensagens pendentes
		msgCount := 0
		if msgs, err := memClient.ListPendingMessages(r.Project); err == nil {
			msgCount = len(msgs)
		}
		msgStr := "0 (idle)"
		if msgCount > 0 {
			msgStr = fmt.Sprintf("📬 %d pendente(s)", msgCount)
		}

		branchName := "main"
		if r.Type != config.RoleLead {
			branchName = fmt.Sprintf("swarm/%s", r.Type)
			// Checa quantos commits ahead
			cCmd := exec.Command("git", "rev-list", "--count", "HEAD.."+branchName)
			cCmd.Dir = cwd
			if out, err := cCmd.Output(); err == nil {
				ahead := strings.TrimSpace(string(out))
				if ahead != "" && ahead != "0" {
					branchName = fmt.Sprintf("%s (+%s)", branchName, ahead)
				}
			}
		}

		fmt.Printf("%s %-15s %-10s %-14s %-16s %-12s\n",
			r.Icon, r.Name, r.Profile, r.Project, branchName, msgStr)
	}

	fmt.Println()
	fmt.Println("Comandos Rápidos:")
	fmt.Println("  agy-swarm attach      - Conectar ao terminal do enxame")
	fmt.Println("  agy-swarm diff        - Inspecionar diffs cumulativos dos especialistas")
	fmt.Println("  agy-swarm stop        - Encerramento e teardown seguro")
}
