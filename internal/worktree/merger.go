package worktree

import (
	"bytes"
	"fmt"
	"os/exec"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
)

// PipelineOrder define a ordem estrita de merge pós-debate.
var PipelineOrder = []config.RoleType{
	config.RoleArch,
	config.RoleHacker,
	config.RoleQA,
	config.RoleSec,
}

// Merger executa a integração sequencial dos branches com Quality Gates.
type Merger struct {
	RepoRoot string
}

// NewMerger inicializa o motor de integração.
func NewMerger(repoRoot string) *Merger {
	return &Merger{RepoRoot: repoRoot}
}

// MergeStepResult reporta o resultado de cada etapa do pipeline.
type MergeStepResult struct {
	Role    config.RoleType
	Success bool
	Error   error
	Message string
}

// ExecutePipeline realiza os merges ordenados executando o gate de teste.
func (m *Merger) ExecutePipeline(activeRoles []config.RoleConfig, testCmd string) ([]MergeStepResult, error) {
	activeMap := make(map[config.RoleType]bool)
	for _, r := range activeRoles {
		activeMap[r.Type] = true
	}

	var results []MergeStepResult

	for _, role := range PipelineOrder {
		if !activeMap[role] {
			continue
		}

		branchName := fmt.Sprintf("swarm/%s", role)
		res := MergeStepResult{Role: role}

		// 1. Executa o merge sem fast-forward
		var stderr bytes.Buffer
		mergeCmd := exec.Command("git", "merge", "--no-ff", branchName, "-m", fmt.Sprintf("swarm(merge): integrando contribuição de %s", role))
		mergeCmd.Dir = m.RepoRoot
		mergeCmd.Stderr = &stderr
		if err := mergeCmd.Run(); err != nil {
			// Conflito de merge: aborta e reverte
			_ = exec.Command("git", "merge", "--abort").Run()
			res.Success = false
			res.Error = fmt.Errorf("conflito de merge no branch %s: %s", branchName, stderr.String())
			res.Message = "Conflito detectado; merge abortado e branch devolvido para rebase"
			results = append(results, res)
			return results, res.Error
		}

		// 2. Se houver comando de teste configurado, executa o Quality Gate
		if testCmd != "" {
			var testStderr bytes.Buffer
			tCmd := exec.Command("sh", "-c", testCmd)
			tCmd.Dir = m.RepoRoot
			tCmd.Stderr = &testStderr
			if err := tCmd.Run(); err != nil {
				// Falha nos testes: reverte o merge
				_ = exec.Command("git", "reset", "--hard", "HEAD~1").Run()
				res.Success = false
				res.Error = fmt.Errorf("quality gate falhou para %s: %s", role, testStderr.String())
				res.Message = "Testes falharam; commit de merge revertido"
				results = append(results, res)
				return results, res.Error
			}
		}

		res.Success = true
		res.Message = "Integrado com sucesso"
		results = append(results, res)
	}

	return results, nil
}
