package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
)

// handleDiff inspeciona e exibe o diff unificado cumulativo de todos os branches do enxame contra HEAD.
func handleDiff() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao obter diretório corrente: %v\n", err)
		os.Exit(1)
	}

	roles := config.AllRoles()
	fmt.Println("⚡ AGY-SWARM // DIFF CUMULATIVO DOS ESPECIALISTAS")
	fmt.Println("Comparando branches 'swarm/{role}' contra o HEAD atual:")
	fmt.Println()

	hasAnyChanges := false

	for _, r := range roles {
		if r.Type == config.RoleLead {
			continue
		}

		branchName := fmt.Sprintf("swarm/%s", r.Type)
		// Verifica se o branch existe
		checkCmd := exec.Command("git", "rev-parse", "--verify", branchName)
		checkCmd.Dir = cwd
		if err := checkCmd.Run(); err != nil {
			continue
		}

		// Stat do diff
		statCmd := exec.Command("git", "diff", "--stat", "HEAD..."+branchName)
		statCmd.Dir = cwd
		statOut, err := statCmd.Output()
		if err != nil || len(statOut) == 0 {
			continue
		}

		hasAnyChanges = true
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		fmt.Printf(" %s %s (%s) · Branch: %s\n", r.Icon, r.Name, r.Project, branchName)
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		fmt.Print(string(statOut))
		fmt.Println()

		// Diff detalhado com cores
		diffCmd := exec.Command("git", "diff", "--color=always", "HEAD..."+branchName)
		diffCmd.Dir = cwd
		diffOut, err := diffCmd.Output()
		if err == nil && len(diffOut) > 0 {
			fmt.Print(string(diffOut))
			fmt.Println()
		}
	}

	if !hasAnyChanges {
		fmt.Println("Nenhum branch de especialista ativo ou nenhuma alteração pendente contra o HEAD.")
	}
}
