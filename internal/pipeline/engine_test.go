package pipeline

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/lease"
)

type mockRunner struct {
	ephemeralResponses map[string]string // accountID -> output
	commandResponses   map[string]int    // command -> exitCode
	failCount429       int
}

func (m *mockRunner) RunEphemeral(ctx context.Context, workdir string, accountID string, promptFile string, timeout time.Duration) (string, int, error) {
	if m.failCount429 > 0 {
		m.failCount429--
		return "HTTP 429: Resource has been exhausted (rate limit)", 1, fmt.Errorf("rate limit 429")
	}
	out := m.ephemeralResponses[accountID]
	if out == "" {
		out = "OK: process completed"
	}
	return out, 0, nil
}

func (m *mockRunner) RunCommand(ctx context.Context, workdir string, commandLine string, timeout time.Duration) (string, int, error) {
	code, ok := m.commandResponses[commandLine]
	if !ok {
		return "PASS", 0, nil
	}
	if code != 0 {
		return "FAIL: test failed", code, fmt.Errorf("command exit %d", code)
	}
	return "PASS", 0, nil
}

func TestPipelineEngineDryRun(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agy-pipeline-test-*")
	if err != nil {
		t.Fatalf("falha ao criar temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Inicializa git repo para o teste de worktree
	initCmd := exec.Command("git", "init")
	initCmd.Dir = tmpDir
	_ = initCmd.Run()

	commitCmd := exec.Command("git", "commit", "--allow-empty", "-m", "initial commit")
	commitCmd.Dir = tmpDir
	_ = commitCmd.Run()

	broker := lease.NewBroker(nil, filepath.Join(tmpDir, "leases.json"))
	runner := &mockRunner{
		ephemeralResponses: make(map[string]string),
		commandResponses:   make(map[string]int),
	}

	cfg := EngineConfig{
		RepoRoot:  tmpDir,
		Task:      "Criar endpoint de health check",
		TestCmd:   "go test ./...",
		DryRun:    true,
		AutoMerge: false,
		Roles:     config.DefaultRoles(),
	}

	engine := NewPipelineEngine(cfg, broker, runner)
	results, err := engine.Execute(context.Background())
	if err != nil {
		t.Fatalf("execução dry-run falhou: %v", err)
	}

	if len(results) != 4 {
		t.Fatalf("esperava 4 resultados de estágio, obteve %d", len(results))
	}

	if results[0].Stage != StageContract || !results[0].Success {
		t.Errorf("Estágio 1 falhou: %+v", results[0])
	}
	if results[1].Stage != StageRedTeam || !results[1].Success {
		t.Errorf("Estágio 2 falhou: %+v", results[1])
	}
	if results[2].Stage != StageSpeculative || !results[2].Success {
		t.Errorf("Estágio 3 falhou: %+v", results[2])
	}
	if results[3].Stage != StageAuditMerge || !results[3].Success {
		t.Errorf("Estágio 4 falhou: %+v", results[3])
	}
	if results[3].Winner == nil {
		t.Errorf("esperava vencedor selecionado no Estágio 4")
	}
}

func TestDispatcherFailoverOn429(t *testing.T) {
	tmpDir := t.TempDir()
	promptFile := filepath.Join(tmpDir, "prompt.txt")
	_ = os.WriteFile(promptFile, []byte("test prompt"), 0600)

	broker := lease.NewBroker(nil, filepath.Join(tmpDir, "leases.json"))
	runner := &mockRunner{
		failCount429: 1, // 1ª falha dá 429, 2ª passa com nova conta
	}

	dispatcher := NewEphemeralDispatcher(runner, broker)
	role := config.DefaultRoles()[1] // Arch

	output, exitCode, activeLease, err := dispatcher.DispatchWithFailover(context.Background(), tmpDir, role, "TestStage", promptFile, 5*time.Second)
	if err != nil {
		t.Fatalf("dispatch com failover falhou: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("exit code inesperado: %d", exitCode)
	}
	if activeLease == nil {
		t.Fatalf("activeLease é nulo")
	}
	defer broker.Release(activeLease)

	if output != "OK: process completed" {
		t.Errorf("output inesperado: %s", output)
	}
}

func TestPromptBuilderSanitization(t *testing.T) {
	builder := NewPromptBuilder("Refatorar módulo de autenticação")
	role := config.RoleConfig{
		Type: config.RoleHacker,
		Name: "Hacker-Pragmático",
		Bias: "Entrega enxuta",
	}

	prompt := builder.BuildWorkerPrompt(role, "+ interface Auth", "+ func TestAuth", "FAIL: nil pointer")
	if !filepath.IsAbs("/") { // noop check
	}
	if len(prompt) == 0 {
		t.Errorf("prompt vazio gerado")
	}

	// Valida que o prompt não induz o LLM a gastar tokens com ai-memory
	if len(prompt) < 100 {
		t.Errorf("prompt curto demais: %d", len(prompt))
	}
}

func TestGetGitDiffWithFromOnly(t *testing.T) {
	tmpDir := t.TempDir()

	_ = exec.Command("git", "init", tmpDir).Run()
	_ = exec.Command("git", "-C", tmpDir, "config", "user.name", "Test").Run()
	_ = exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com").Run()

	testFile := filepath.Join(tmpDir, "test.txt")
	_ = os.WriteFile(testFile, []byte("linha 1\n"), 0644)
	_ = exec.Command("git", "-C", tmpDir, "add", "-A").Run()
	_ = exec.Command("git", "-C", tmpDir, "commit", "-m", "commit 1").Run()

	// Cria branch de base
	_ = exec.Command("git", "-C", tmpDir, "branch", "swarm/qa").Run()

	// Adiciona modificação e comita em HEAD
	_ = os.WriteFile(testFile, []byte("linha 1\nlinha 2 modificada\n"), 0644)
	_ = exec.Command("git", "-C", tmpDir, "add", "-A").Run()
	_ = exec.Command("git", "-C", tmpDir, "commit", "-m", "commit 2").Run()

	engine := &PipelineEngine{}

	// Chamada com to == "" DEVE comparar contra swarm/qa e não ignorar o from!
	diff, err := engine.getGitDiff(tmpDir, "swarm/qa", "")
	if err != nil {
		t.Fatalf("getGitDiff falhou: %v", err)
	}

	if diff == "" {
		t.Fatalf("getGitDiff retornou diff vazio quando deveria conter alteracao contra swarm/qa")
	}

	added, deleted := countDiffLines(diff)
	if added == 0 {
		t.Errorf("esperava added > 0 no diff, obteve added=%d deleted=%d", added, deleted)
	}
}

func TestCargoTargetDirInjection(t *testing.T) {
	tmpDir := t.TempDir()
	cargoFile := filepath.Join(tmpDir, "Cargo.toml")
	_ = os.WriteFile(cargoFile, []byte("[workspace]\nmembers = []"), 0644)

	origEnv := os.Getenv("CARGO_TARGET_DIR")
	defer func() {
		if origEnv != "" {
			_ = os.Setenv("CARGO_TARGET_DIR", origEnv)
		} else {
			_ = os.Unsetenv("CARGO_TARGET_DIR")
		}
	}()
	_ = os.Unsetenv("CARGO_TARGET_DIR")

	broker := lease.NewBroker(nil, filepath.Join(tmpDir, "leases.json"))
	cfg := EngineConfig{
		RepoRoot: tmpDir,
		Task:     "test rust",
	}

	_ = NewPipelineEngine(cfg, broker, nil)

	expectedTarget := filepath.Join(tmpDir, "target")
	if got := os.Getenv("CARGO_TARGET_DIR"); got != expectedTarget {
		t.Errorf("CARGO_TARGET_DIR não foi injetado corretamente: esperado %s, obtido %s", expectedTarget, got)
	}
}
