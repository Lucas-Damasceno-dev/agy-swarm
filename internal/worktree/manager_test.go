package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
)

func TestNamespacedManagerPaths(t *testing.T) {
	roles := config.DefaultRoles()
	m := NewNamespacedManager("/tmp/repo", roles, "card-01")

	archRole := roles[1] // config.RoleArch
	dir := m.DirForRole(archRole)
	branch := m.BranchForRole(archRole)

	expectedDir := filepath.Join("/tmp/repo", ".worktrees", "card-01", "worker-arch")
	if dir != expectedDir {
		t.Errorf("DirForRole incorreto: esperado %s, obtido %s", expectedDir, dir)
	}

	expectedBranch := "swarm/card-01/arch"
	if branch != expectedBranch {
		t.Errorf("BranchForRole incorreto: esperado %s, obtido %s", expectedBranch, branch)
	}
}

func TestSafeCleanup_NamespacedWorktreesAndBranches(t *testing.T) {
	tmpDir := t.TempDir()

	_ = exec.Command("git", "-C", tmpDir, "init").Run()
	_ = exec.Command("git", "-C", tmpDir, "config", "user.name", "Test").Run()
	_ = exec.Command("git", "-C", tmpDir, "config", "user.email", "test@example.com").Run()

	baseFile := filepath.Join(tmpDir, "README.md")
	_ = os.WriteFile(baseFile, []byte("# Repo\n"), 0644)
	_ = exec.Command("git", "-C", tmpDir, "add", ".").Run()
	_ = exec.Command("git", "-C", tmpDir, "commit", "-m", "init").Run()

	roles := []config.RoleConfig{
		{Type: config.RoleArch, Name: "Arch"},
		{Type: config.RoleQA, Name: "QA"},
	}

	// 1. Provisiona manager com namespace
	nsMgr := NewNamespacedManager(tmpDir, roles, "card-01")
	wtDir, err := nsMgr.ProvisionRoleWorktree(roles[0])
	if err != nil {
		t.Fatalf("ProvisionRoleWorktree falhou: %v", err)
	}

	if _, err := os.Stat(wtDir); os.IsNotExist(err) {
		t.Fatalf("worktree com namespace não foi criada no disco")
	}

	branchName := nsMgr.BranchForRole(roles[0])
	// Garante commit inicial na worktree para estar limpo
	_ = exec.Command("git", "-C", wtDir, "commit", "--allow-empty", "-m", "worker commit").Run()

	// 2. Executa SafeCleanup através do manager global (como em agy-swarm clean ou agy-swarm stop)
	globalMgr := NewManager(tmpDir, config.AllRoles())
	if err := globalMgr.SafeCleanup(false); err != nil {
		t.Fatalf("global SafeCleanup falhou: %v", err)
	}

	// 3. Valida que o diretório .worktrees foi completamente removido
	worktreesDir := filepath.Join(tmpDir, ".worktrees")
	if _, err := os.Stat(worktreesDir); !os.IsNotExist(err) {
		t.Errorf("pasta .worktrees ainda existe no disco após SafeCleanup global")
	}

	// 4. Valida que o branch swarm/card-01/arch foi excluído
	branchCheck, _ := exec.Command("git", "-C", tmpDir, "branch", "--list", branchName).Output()
	if strings.Contains(string(branchCheck), branchName) {
		t.Errorf("branch com namespace %s não foi excluído pelo SafeCleanup", branchName)
	}
}

func TestReapOrphanWorktrees(t *testing.T) {
	tmpDir := t.TempDir()

	_ = exec.Command("git", "-C", tmpDir, "init").Run()
	_ = exec.Command("git", "-C", tmpDir, "config", "user.name", "Test").Run()
	_ = exec.Command("git", "-C", tmpDir, "config", "user.email", "test@example.com").Run()

	baseFile := filepath.Join(tmpDir, "README.md")
	_ = os.WriteFile(baseFile, []byte("# Repo\n"), 0644)
	_ = exec.Command("git", "-C", tmpDir, "add", ".").Run()
	_ = exec.Command("git", "-C", tmpDir, "commit", "-m", "init").Run()

	// Cria uma worktree sob .worktrees/nested/orphan
	orphanDir := filepath.Join(tmpDir, ".worktrees", "nested", "orphan")
	_ = exec.Command("git", "-C", tmpDir, "worktree", "add", "-b", "swarm/orphan-branch", orphanDir, "HEAD").Run()

	mgr := NewManager(tmpDir, nil)
	if err := mgr.ReapOrphanWorktrees(true); err != nil {
		t.Fatalf("ReapOrphanWorktrees falhou: %v", err)
	}

	// Verifica se .worktrees foi eliminado
	if _, err := os.Stat(filepath.Join(tmpDir, ".worktrees")); !os.IsNotExist(err) {
		t.Error("esperava que .worktrees fosse completamente removido")
	}

	// Verifica se branch swarm/orphan-branch foi excluído
	branchCheck, _ := exec.Command("git", "-C", tmpDir, "branch", "--list", "swarm/orphan-branch").Output()
	if strings.Contains(string(branchCheck), "swarm/orphan-branch") {
		t.Error("esperava que swarm/orphan-branch fosse excluído")
	}
}

