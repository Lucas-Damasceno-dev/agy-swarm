package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/agyhub"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/daemon"
)

type CheckResult struct {
	Name     string
	Passed   bool
	Critical bool
	Detail   string
}

func handleDoctor() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao obter diretório de trabalho: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("🏥 AGY-SWARM // DIAGNÓSTICO PRÉ-VOO DO ENXAME")
	fmt.Println("─────────────────────────────────────────────────────")

	results := runDoctorChecks(cwd)
	hasCriticalFailure := false

	for _, r := range results {
		icon := "✅"
		if !r.Passed {
			if r.Critical {
				icon = "❌"
				hasCriticalFailure = true
			} else {
				icon = "⚠️ "
			}
		}
		critLabel := ""
		if r.Critical && !r.Passed {
			critLabel = " [CRÍTICO]"
		}
		fmt.Printf("%s %-26s %s%s\n", icon, r.Name, r.Detail, critLabel)
	}

	fmt.Println("─────────────────────────────────────────────────────")
	if hasCriticalFailure {
		fmt.Println("❌ Falha no diagnóstico: resolva os itens críticos antes de iniciar pipelines.")
		os.Exit(1)
	}
	fmt.Println("✨ Ambiente operacional: pronto para despachar enxames e pipelines.")
}

func runDoctorChecks(cwd string) []CheckResult {
	var results []CheckResult

	// 1. Git Repository
	gitCmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	gitCmd.Dir = cwd
	if err := gitCmd.Run(); err != nil {
		results = append(results, CheckResult{
			Name:     "Repositório Git",
			Passed:   false,
			Critical: true,
			Detail:   "CWD não é um repositório git válido",
		})
	} else {
		statusOut, _ := exec.Command("git", "-C", cwd, "status", "--porcelain").Output()
		detail := "Árvore limpa (HEAD)"
		if len(strings.TrimSpace(string(statusOut))) > 0 {
			lines := strings.Split(strings.TrimSpace(string(statusOut)), "\n")
			detail = fmt.Sprintf("Árvore suja (%d arquivos modificados/untracked)", len(lines))
		}
		results = append(results, CheckResult{
			Name:     "Repositório Git",
			Passed:   true,
			Critical: true,
			Detail:   detail,
		})
	}

	// 2. Git Worktrees Órfãs
	wtDir := filepath.Join(cwd, ".worktrees")
	if entries, err := os.ReadDir(wtDir); err == nil && len(entries) > 0 {
		results = append(results, CheckResult{
			Name:     "Worktrees Isoladas",
			Passed:   true,
			Critical: false,
			Detail:   fmt.Sprintf("%d worktree(s) residual(is) em .worktrees/ (use 'agy-swarm clean')", len(entries)),
		})
	} else {
		results = append(results, CheckResult{
			Name:     "Worktrees Isoladas",
			Passed:   true,
			Critical: false,
			Detail:   "Nenhuma worktree órfã em .worktrees/",
		})
	}

	// 3. Tmux Multiplexer
	if tmuxPath, err := exec.LookPath("tmux"); err != nil {
		results = append(results, CheckResult{
			Name:     "Tmux Multiplexer",
			Passed:   false,
			Critical: true,
			Detail:   "Binário 'tmux' não encontrado no PATH",
		})
	} else {
		versionOut, _ := exec.Command(tmuxPath, "-V").Output()
		results = append(results, CheckResult{
			Name:     "Tmux Multiplexer",
			Passed:   true,
			Critical: true,
			Detail:   strings.TrimSpace(string(versionOut)),
		})
	}

	// 4. Daemon Hub Socket & Contas
	sockPath := daemon.GetDefaultSocketPath()
	t0 := time.Now()
	conn, err := net.DialTimeout("unix", sockPath, 250*time.Millisecond)
	if err != nil && sockPath != "/tmp/agy-hub.sock" {
		sockPath = "/tmp/agy-hub.sock"
		conn, err = net.DialTimeout("unix", sockPath, 250*time.Millisecond)
	}
	if err != nil {
		results = append(results, CheckResult{
			Name:     "Daemon Hub",
			Passed:   false,
			Critical: false,
			Detail:   fmt.Sprintf("Daemon offline em %s (modo fallback standalone ativo)", sockPath),
		})
	} else {
		latency := time.Since(t0).Round(time.Microsecond)
		_ = conn.Close()

		client := agyhub.NewClient(sockPath)
		state, err := client.GetClusterState()
		if err == nil && state != nil {
			activeCount := 0
			cooldownCount := 0
			for _, p := range state.Profiles {
				if p.Status == "ACTIVE" || p.Status == "IDLE" {
					activeCount++
				} else if p.Status == "COOLDOWN" {
					cooldownCount++
				}
			}
			results = append(results, CheckResult{
				Name:     "Daemon Hub",
				Passed:   true,
				Critical: false,
				Detail:   fmt.Sprintf("Online (%s latência, %d/%d contas operacionais, %d cooldown)", latency, activeCount, len(state.Profiles), cooldownCount),
			})
		} else {
			results = append(results, CheckResult{
				Name:     "Daemon Hub",
				Passed:   true,
				Critical: false,
				Detail:   fmt.Sprintf("Socket ativo (%s latência)", latency),
			})
		}
	}

	// 5. Ripgrep (rg)
	if _, err := exec.LookPath("rg"); err == nil {
		results = append(results, CheckResult{
			Name:     "Ripgrep (rg)",
			Passed:   true,
			Critical: false,
			Detail:   "Instalado no PATH",
		})
	} else {
		results = append(results, CheckResult{
			Name:     "Ripgrep (rg)",
			Passed:   false,
			Critical: false,
			Detail:   "Não encontrado (recomendado para varredura rápida)",
		})
	}

	// 6. Quality Gate Padrão
	detected := detectDefaultTestCmd(cwd)
	results = append(results, CheckResult{
		Name:     "Quality Gate Padrão",
		Passed:   true,
		Critical: false,
		Detail:   fmt.Sprintf("%q", detected),
	})

	_ = context.Background()
	return results
}
