package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
)

// handleDiff inspeciona e exibe o diff unificado cumulativo de todos os branches do enxame contra HEAD.
func handleDiff() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao obter diretório corrente: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("⚡ AGY-SWARM // DIFF CUMULATIVO DOS ESPECIALISTAS")
	fmt.Println("Comparando branches 'swarm/**' contra o HEAD atual:")
	fmt.Println()

	// Lista dinamicamente todos os branches sob refs/heads/swarm/ (suporta kanban waves e namespaces)
	branchOut, err := exec.Command("git", "-C", cwd, "for-each-ref", "--format=%(refname:short)", "refs/heads/swarm/").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao consultar branches git: %v\n", err)
		os.Exit(1)
	}

	rawBranches := strings.Split(strings.TrimSpace(string(branchOut)), "\n")
	roles := config.AllRoles()
	roleMap := make(map[config.RoleType]config.RoleConfig)
	for _, r := range roles {
		roleMap[r.Type] = r
	}

	hasAnyChanges := false

	for _, branchName := range rawBranches {
		branchName = strings.TrimSpace(branchName)
		if branchName == "" {
			continue
		}

		// Stat do diff
		statCmd := exec.Command("git", "-C", cwd, "diff", "--stat", "HEAD..."+branchName)
		statOut, err := statCmd.Output()
		if err != nil || len(statOut) == 0 {
			continue
		}

		hasAnyChanges = true

		// Identifica papel pelo sufixo do branch (ex: swarm/card-01/arch -> arch)
		parts := strings.Split(branchName, "/")
		roleKey := config.RoleType(parts[len(parts)-1])
		r, ok := roleMap[roleKey]
		icon := "🌿"
		name := branchName
		project := branchName
		if ok {
			icon = r.Icon
			name = r.Name
			project = r.Project
		}

		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		fmt.Printf(" %s %s (%s) · Branch: %s\n", icon, name, project, branchName)
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		fmt.Print(string(statOut))
		fmt.Println()

		// Diff detalhado com cores
		diffCmd := exec.Command("git", "-C", cwd, "diff", "--color=always", "HEAD..."+branchName)
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
