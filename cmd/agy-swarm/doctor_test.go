package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunDoctorChecks(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Diretório temporário vazio (não é git repo)
	checks := runDoctorChecks(tmpDir)
	if len(checks) < 4 {
		t.Fatalf("esperava ao menos 4 verificações, obteve %d", len(checks))
	}

	foundGit := false
	for _, c := range checks {
		if c.Name == "Repositório Git" {
			foundGit = true
			if c.Passed {
				t.Error("esperava que repositório git falhasse para tmpDir vazio")
			}
			if !c.Critical {
				t.Error("esperava que falha de repositório git fosse crítica")
			}
		}
	}
	if !foundGit {
		t.Error("verificação 'Repositório Git' não encontrada")
	}

	// 2. No CWD real do projeto
	cwd, err := os.Getwd()
	if err != nil {
		t.Skip("não foi possível obter CWD")
	}

	realChecks := runDoctorChecks(cwd)
	for _, c := range realChecks {
		if c.Name == "Repositório Git" && !c.Passed {
			t.Errorf("esperava que CWD do projeto passasse na verificação git: %s", c.Detail)
		}
		if c.Name == "Quality Gate Padrão" && c.Detail == "" {
			t.Error("esperava Quality Gate não vazio")
		}
	}
}

func TestRunDoctorChecks_WithWorktrees(t *testing.T) {
	tmpDir := t.TempDir()
	wtDir := filepath.Join(tmpDir, ".worktrees", "orphan-worker")
	_ = os.MkdirAll(wtDir, 0755)

	checks := runDoctorChecks(tmpDir)
	for _, c := range checks {
		if c.Name == "Worktrees Isoladas" {
			if !c.Passed {
				t.Error("worktree residual não deve quebrar como falha")
			}
		}
	}
}
