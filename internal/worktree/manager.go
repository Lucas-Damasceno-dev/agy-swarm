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
	RepoRoot  string
	Roles     []config.RoleConfig
	Namespace string
	mu        sync.Mutex
}

// NewManager cria uma nova instância do gerenciador de worktrees no namespace padrão.
func NewManager(repoRoot string, roles []config.RoleConfig) *Manager {
	return &Manager{
		RepoRoot: repoRoot,
		Roles:    roles,
	}
}

// NewNamespacedManager cria uma nova instância de gerenciador de worktrees com isolamento por cartão/namespace.
func NewNamespacedManager(repoRoot string, roles []config.RoleConfig, namespace string) *Manager {
	return &Manager{
		RepoRoot:  repoRoot,
		Roles:     roles,
		Namespace: strings.TrimSpace(namespace),
	}
}

// DirForRole retorna o caminho do diretório da worktree do papel respeitando o namespace.
func (m *Manager) DirForRole(role config.RoleConfig) string {
	if m.Namespace != "" {
		cleanNS := strings.ToLower(strings.ReplaceAll(m.Namespace, " ", "-"))
		return filepath.Join(m.RepoRoot, ".worktrees", cleanNS, fmt.Sprintf("worker-%s", role.Type))
	}
	return filepath.Join(m.RepoRoot, ".worktrees", fmt.Sprintf("worker-%s", role.Type))
}

// BranchForRole retorna o nome do branch git do papel respeitando o namespace.
func (m *Manager) BranchForRole(role config.RoleConfig) string {
	if m.Namespace != "" {
		cleanNS := strings.ToLower(strings.ReplaceAll(m.Namespace, " ", "-"))
		return fmt.Sprintf("swarm/%s/%s", cleanNS, role.Type)
	}
	return fmt.Sprintf("swarm/%s", role.Type)
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

// EnsureGitIgnore garante que as pastas .worktrees e .gemini sejam ignoradas pelo Git.
// Utiliza prioritariamente .git/info/exclude para evitar criar arquivos untracked no working tree raiz,
// o que causaria conflitos de merge (exit status 2) quando branches efêmeros commitam .gitignore.
func (m *Manager) EnsureGitIgnore() error {
	gitDir := filepath.Join(m.RepoRoot, ".git")
	if fi, err := os.Stat(gitDir); err == nil && fi.IsDir() {
		excludePath := filepath.Join(gitDir, "info", "exclude")
		_ = os.MkdirAll(filepath.Dir(excludePath), 0755)
		content, _ := os.ReadFile(excludePath)
		s := string(content)
		var appendLines []string
		if !strings.Contains(s, ".worktrees/") && !strings.Contains(s, ".worktrees\n") {
			appendLines = append(appendLines, ".worktrees/")
		}
		if !strings.Contains(s, ".gemini/") && !strings.Contains(s, ".gemini\n") {
			appendLines = append(appendLines, ".gemini/")
		}
		if len(appendLines) > 0 {
			f, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err == nil {
				defer f.Close()
				_, _ = f.WriteString("\n# agy-swarm worker worktrees e regras efêmeras\n" + strings.Join(appendLines, "\n") + "\n")
			}
		}
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

	targetDir := m.DirForRole(role)
	branchName := m.BranchForRole(role)

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
		existing, _ := os.ReadFile(excludeFile)
		if !strings.Contains(string(existing), ".gemini/") {
			_ = os.MkdirAll(filepath.Dir(excludeFile), 0755)
			if f, err := os.OpenFile(excludeFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
				_, _ = f.WriteString("\n.gemini/\n.gemini/rules/swarm-persona.md\n")
				_ = f.Close()
			}
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
// Suporta tanto limpeza global (m.Namespace == "") quanto limpeza isolada por cartão/namespace.
func (m *Manager) SafeCleanup(force bool) error {
	worktreesDir := filepath.Join(m.RepoRoot, ".worktrees")

	var targetPrefix string
	var branchPattern string
	if m.Namespace != "" {
		cleanNS := strings.ToLower(strings.ReplaceAll(m.Namespace, " ", "-"))
		targetPrefix = filepath.Join(worktreesDir, cleanNS)
		branchPattern = fmt.Sprintf("refs/heads/swarm/%s/", cleanNS)
	} else {
		targetPrefix = worktreesDir
		branchPattern = "refs/heads/swarm/"
	}

	hasWorktreesDir := false
	if _, err := os.Stat(worktreesDir); err == nil {
		hasWorktreesDir = true
	}

	// 1. Localiza worktrees ativas registradas no Git
	if hasWorktreesDir {
		wtListOut, err := exec.Command("git", "-C", m.RepoRoot, "worktree", "list", "--porcelain").Output()
		var registeredWorktrees []string
		if err == nil {
			lines := strings.Split(string(wtListOut), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "worktree ") {
					wtPath := strings.TrimSpace(strings.TrimPrefix(line, "worktree "))
					// Verifica se a worktree pertence ao escopo alvo
					rel, rErr := filepath.Rel(targetPrefix, wtPath)
					if rErr == nil && !strings.HasPrefix(rel, "..") {
						registeredWorktrees = append(registeredWorktrees, wtPath)
					}
				}
			}
		}

		// 2. Se !force, checa alterações pendentes em todas as worktrees identificadas
		if !force {
			for _, wtPath := range registeredWorktrees {
				statusCmd := exec.Command("git", "-C", wtPath, "status", "--porcelain")
				out, _ := statusCmd.Output()
				if len(bytes.TrimSpace(out)) > 0 {
					return fmt.Errorf("worktree %s possui alterações pendentes. Use --force para descartar ou faça commit", wtPath)
				}
			}
		}

		// 3. Remove as worktrees registradas
		for _, wtPath := range registeredWorktrees {
			_ = exec.Command("git", "-C", m.RepoRoot, "worktree", "remove", "--force", wtPath).Run()
		}
		_ = exec.Command("git", "-C", m.RepoRoot, "worktree", "prune").Run()
	}

	// 4. Remove branches temporários do swarm no escopo alvo
	branchOut, _ := exec.Command("git", "-C", m.RepoRoot, "for-each-ref", "--format=%(refname:short)", branchPattern).Output()
	for _, b := range strings.Split(strings.TrimSpace(string(branchOut)), "\n") {
		b = strings.TrimSpace(b)
		if b != "" {
			_ = exec.Command("git", "-C", m.RepoRoot, "branch", "-D", b).Run()
		}
	}

	// 5. Limpa diretórios residuais no sistema de arquivos
	if hasWorktreesDir {
		if m.Namespace != "" {
			_ = os.RemoveAll(targetPrefix)
			entries, _ := os.ReadDir(worktreesDir)
			if len(entries) == 0 {
				_ = os.Remove(worktreesDir)
			}
		} else {
			_ = os.RemoveAll(worktreesDir)
		}
	}

	return nil
}

