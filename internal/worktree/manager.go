package worktree

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
)

// Manager gerencia o ciclo de vida dos Git Worktrees dos especialistas.
type Manager struct {
	RepoRoot string
	Roles    []config.RoleConfig
	mu       sync.Mutex
}

// NewManager cria uma nova instância do gerenciador de worktrees.
func NewManager(repoRoot string, roles []config.RoleConfig) *Manager {
	return &Manager{
		RepoRoot: repoRoot,
		Roles:    roles,
	}
}

// EnsureGitRepo valida se o diretório é um repositório git válido.
func (m *Manager) EnsureGitRepo() error {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = m.RepoRoot
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("o diretório '%s' não é um repositório git válido", m.RepoRoot)
	}
	return nil
}

// EnsureGitIgnore garante que as pastas .worktrees e .gemini estejam presentes no .gitignore raiz.
func (m *Manager) EnsureGitIgnore() error {
	gitignorePath := filepath.Join(m.RepoRoot, ".gitignore")
	content, _ := os.ReadFile(gitignorePath)
	s := string(content)
	var appendLines []string
	if !strings.Contains(s, ".worktrees/") && !strings.Contains(s, ".worktrees\n") {
		appendLines = append(appendLines, ".worktrees/")
	}
	if !strings.Contains(s, ".gemini/") && !strings.Contains(s, ".gemini\n") {
		appendLines = append(appendLines, ".gemini/")
	}
	if len(appendLines) > 0 {
		f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		defer f.Close()
		_, _ = f.WriteString("\n# agy-swarm worker worktrees e regras efêmeras\n" + strings.Join(appendLines, "\n") + "\n")
	}
	return nil
}

// ProvisionRoleWorktreeFromBase cria a worktree ramificada a partir de uma referência base específica.
func (m *Manager) ProvisionRoleWorktreeFromBase(role config.RoleConfig, baseRef string) (string, error) {
	if role.Type == config.RoleLead {
		// Maestro opera no próprio repositório principal
		return m.RepoRoot, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if baseRef == "" {
		baseRef = "HEAD"
	}

	targetDir := filepath.Join(m.RepoRoot, ".worktrees", fmt.Sprintf("worker-%s", role.Type))
	branchName := fmt.Sprintf("swarm/%s", role.Type)

	_ = os.MkdirAll(filepath.Dir(targetDir), 0755)

	// Se a worktree já existir, remove para recriar sincronizada a partir da base
	if _, err := os.Stat(targetDir); err == nil {
		_ = exec.Command("git", "-C", m.RepoRoot, "worktree", "remove", "--force", targetDir).Run()
		_ = os.RemoveAll(targetDir)
	}

	// Cria o worktree com branch dedicado baseado na base solicitada
	var stderr bytes.Buffer
	cmd := exec.Command("git", "worktree", "add", "-B", branchName, targetDir, baseRef)
	cmd.Dir = m.RepoRoot
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("falha ao criar git worktree para %s a partir de %s: %s (%w)", role.Name, baseRef, stderr.String(), err)
	}

	// Injeta as regras da persona em .gemini/rules/swarm-persona.md
	rulesDir := filepath.Join(targetDir, ".gemini", "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		return "", fmt.Errorf("falha ao criar pasta de regras: %w", err)
	}

	ruleContent := config.GeneratePersonaRule(role, m.Roles)
	rulePath := filepath.Join(rulesDir, "swarm-persona.md")
	if err := os.WriteFile(rulePath, []byte(ruleContent), 0644); err != nil {
		return "", fmt.Errorf("falha ao escrever regra de persona: %w", err)
	}

	// Blindagem Git: Garante que .gemini/rules/swarm-persona.md nunca seja rastreado nem commitado
	excludePathOut, err := exec.Command("git", "-C", targetDir, "rev-parse", "--git-path", "info/exclude").Output()
	if err == nil {
		excludeFile := strings.TrimSpace(string(excludePathOut))
		if !filepath.IsAbs(excludeFile) {
			excludeFile = filepath.Join(targetDir, excludeFile)
		}
		_ = os.MkdirAll(filepath.Dir(excludeFile), 0755)
		if f, err := os.OpenFile(excludeFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			_, _ = f.WriteString("\n.gemini/\n.gemini/rules/swarm-persona.md\n")
			_ = f.Close()
		}
	}

	return targetDir, nil
}

// ProvisionRoleWorktree cria o worktree do especialista baseado em HEAD por padrão.
func (m *Manager) ProvisionRoleWorktree(role config.RoleConfig) (string, error) {
	return m.ProvisionRoleWorktreeFromBase(role, "HEAD")
}

// ProvisionAll provisiona worktrees para todos os especialistas configurados.
func (m *Manager) ProvisionAll() (map[config.RoleType]string, error) {
	if err := m.EnsureGitRepo(); err != nil {
		return nil, err
	}
	_ = m.EnsureGitIgnore()

	result := make(map[config.RoleType]string)
	for _, r := range m.Roles {
		path, err := m.ProvisionRoleWorktree(r)
		if err != nil {
			return nil, err
		}
		result[r.Type] = path
	}
	return result, nil
}

// SafeCleanup remove os worktrees e branches temporários caso estejam sem alterações pendentes.
func (m *Manager) SafeCleanup(force bool) error {
	worktreesDir := filepath.Join(m.RepoRoot, ".worktrees")
	if _, err := os.Stat(worktreesDir); os.IsNotExist(err) {
		return nil
	}

	for _, r := range m.Roles {
		if r.Type == config.RoleLead {
			continue
		}
		targetDir := filepath.Join(worktreesDir, fmt.Sprintf("worker-%s", r.Type))
		if _, err := os.Stat(targetDir); os.IsNotExist(err) {
			continue
		}

		// Checa alterações pendentes
		if !force {
			statusCmd := exec.Command("git", "status", "--porcelain")
			statusCmd.Dir = targetDir
			out, _ := statusCmd.Output()
			if len(bytes.TrimSpace(out)) > 0 {
				return fmt.Errorf("worktree %s possui alterações pendentes. Use --force para descartar ou faça commit", targetDir)
			}
		}

		// Remove worktree
		_ = exec.Command("git", "worktree", "remove", "--force", targetDir).Run()
		branchName := fmt.Sprintf("swarm/%s", r.Type)
		_ = exec.Command("git", "branch", "-D", branchName).Run()
	}

	_ = exec.Command("git", "worktree", "prune").Run()
	_ = os.Remove(worktreesDir)
	return nil
}
