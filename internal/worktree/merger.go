package worktree

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
			// Aborta o merge em conflito garantindo diretório raiz
			_ = exec.Command("git", "-C", m.RepoRoot, "merge", "--abort").Run()

			// Tentativa de Relay Rebase automático para resolver divergências limpas
			if rebaseErr := m.RebaseBranch(role, "HEAD"); rebaseErr == nil {
				// Retry do merge após rebase limpo
				var retryStderr bytes.Buffer
				retryCmd := exec.Command("git", "merge", "--no-ff", branchName, "-m", fmt.Sprintf("swarm(merge): integrando contribuição de %s (após relay rebase)", role))
				retryCmd.Dir = m.RepoRoot
				retryCmd.Stderr = &retryStderr
				if retryErr := retryCmd.Run(); retryErr != nil {
					_ = exec.Command("git", "-C", m.RepoRoot, "merge", "--abort").Run()
					res.Success = false
					res.Error = fmt.Errorf("conflito de merge persistente no branch %s após rebase: %s", branchName, retryStderr.String())
					res.Message = "Conflito irresolvível automaticamente; merge abortado"
					results = append(results, res)
					return results, res.Error
				}
			} else {
				res.Success = false
				res.Error = fmt.Errorf("conflito de merge no branch %s: %s (rebase falhou: %v)", branchName, stderr.String(), rebaseErr)
				res.Message = "Conflito detectado; merge abortado e branch requer intervenção manual"
				results = append(results, res)
				return results, res.Error
			}
		}

		// 2. Se houver comando de teste configurado, executa o Quality Gate
		if testCmd != "" {
			var testStderr bytes.Buffer
			tCmd := exec.Command("sh", "-c", testCmd)
			tCmd.Dir = m.RepoRoot
			tCmd.Stderr = &testStderr
			if err := tCmd.Run(); err != nil {
				// Falha nos testes: reverte o merge
				_ = exec.Command("git", "-C", m.RepoRoot, "reset", "--hard", "HEAD~1").Run()
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

// RebaseBranch executa o rebase do branch de trabalho sobre a base especificada.
func (m *Merger) RebaseBranch(role config.RoleType, base string) error {
	branchName := fmt.Sprintf("swarm/%s", role)
	cmd := exec.Command("git", "rebase", base, branchName)
	cmd.Dir = m.RepoRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = exec.Command("git", "-C", m.RepoRoot, "rebase", "--abort").Run()
		return fmt.Errorf("falha no rebase do branch %s sobre %s: %s", branchName, base, stderr.String())
	}
	return nil
}

// RebaseInWorktree executa o rebase do branch dentro do próprio diretório da worktree onde está ativo,
// contornando a restrição do Git que proíbe rebase a partir de outro checkout do repositório.
func (m *Merger) RebaseInWorktree(worktreeDir string, targetBranch string) error {
	var stderr bytes.Buffer
	cmd := exec.Command("git", "-C", worktreeDir, "rebase", targetBranch)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = exec.Command("git", "-C", worktreeDir, "rebase", "--abort").Run()
		return fmt.Errorf("falha no rebase em %s sobre %s: %s (%w)", worktreeDir, targetBranch, strings.TrimSpace(stderr.String()), err)
	}
	return nil
}

// MergeBranch executa o merge sem fast-forward do branch alvo no repositório principal com Quality Gate e reversão automática.
func (m *Merger) MergeBranch(ctx context.Context, branchName string, commitMsg string, testCmd string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	// Remove .gitignore untracked no repositório raiz caso exista
	rootGitIgnore := filepath.Join(m.RepoRoot, ".gitignore")
	checkUntracked := exec.CommandContext(ctx, "git", "-C", m.RepoRoot, "status", "--porcelain", ".gitignore")
	if out, err := checkUntracked.Output(); err == nil && strings.HasPrefix(strings.TrimSpace(string(out)), "??") {
		_ = os.Remove(rootGitIgnore)
	}

	mergeCmd := exec.CommandContext(ctx, "git", "-C", m.RepoRoot, "merge", "--no-ff", branchName, "-m", commitMsg)
	if out, err := mergeCmd.CombinedOutput(); err != nil {
		_ = exec.Command("git", "-C", m.RepoRoot, "merge", "--abort").Run()
		return fmt.Errorf("falha ao integrar branch %s: %s (%w)", branchName, strings.TrimSpace(string(out)), err)
	}

	// Executa o Quality Gate se especificado
	if testCmd != "" {
		testExec := exec.CommandContext(ctx, "sh", "-c", testCmd)
		testExec.Dir = m.RepoRoot
		if out, err := testExec.CombinedOutput(); err != nil {
			_ = exec.Command("git", "-C", m.RepoRoot, "reset", "--hard", "HEAD~1").Run()
			return fmt.Errorf("Quality Gate falhou no branch principal após merge de %s:\n%s (%w)", branchName, strings.TrimSpace(string(out)), err)
		}
	}

	return nil
}

