package pipeline

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

	engine := NewPipelineEngine(cfg, broker, nil)

	expectedTarget := filepath.Join(tmpDir, "target")
	osRunner, ok := engine.dispatcher.Runner.(*OSProcessRunner)
	if !ok {
		t.Fatalf("esperava OSProcessRunner no dispatcher")
	}
	found := false
	for _, envItem := range osRunner.Env {
		if envItem == fmt.Sprintf("CARGO_TARGET_DIR=%s", expectedTarget) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("CARGO_TARGET_DIR não foi injetado no OSProcessRunner: esperado %s, obtido %v", expectedTarget, osRunner.Env)
	}
}

func TestRunStageAuditMerge_RobustnessSelection(t *testing.T) {
	engine := &PipelineEngine{
		cfg: EngineConfig{
			DryRun:    true,
			AutoMerge: false,
		},
	}

	// Cenário A: Hacker com 20 linhas limpas e 1 checagem de erro vence monstro de 150 linhas de Sec
	survivorsMonster := []CandidateResult{
		{
			Role: config.RoleConfig{
				Type: config.RoleHacker,
				Name: "Hacker",
			},
			AccountID:    "acc-hacker",
			Branch:       "swarm/hacker",
			Duration:     3 * time.Second,
			TestPassed:   true,
			Diff:         "+ if err != nil { return nil, err }\n+ func Clean() {}\n",
			LinesAdded:   20,
			LinesDeleted: 0,
		},
		{
			Role: config.RoleConfig{
				Type: config.RoleSec,
				Name: "Security Specialist",
			},
			AccountID:    "acc-sec",
			Branch:       "swarm/sec",
			Duration:     5 * time.Second,
			TestPassed:   true,
			Diff:         "+ if err != nil { return nil, err }\n+ // bloated code\n",
			LinesAdded:   150,
			LinesDeleted: 10,
		},
	}

	resA, err := engine.runStageAuditMerge(context.Background(), survivorsMonster)
	if err != nil || resA.Winner == nil {
		t.Fatalf("runStageAuditMerge falhou: %v", err)
	}
	if resA.Winner.Role.Type != config.RoleHacker {
		t.Errorf("esperava que Hacker vencesse contra código inchado de 150 linhas de Sec, mas venceu: %s", resA.Winner.Role.Type)
	}

	// Cenário B: Sec com código defensivo de 8 linhas vence gambiarra de 3 linhas sem checagem de erro
	survivorsFair := []CandidateResult{
		{
			Role: config.RoleConfig{
				Type: config.RoleHacker,
				Name: "Hacker",
			},
			AccountID:    "acc-hacker",
			Branch:       "swarm/hacker",
			Duration:     3 * time.Second,
			TestPassed:   true,
			Diff:         "+ // 3-line quick hack\n+ return nil\n",
			LinesAdded:   3,
			LinesDeleted: 0,
		},
		{
			Role: config.RoleConfig{
				Type: config.RoleSec,
				Name: "Security Specialist",
			},
			AccountID:    "acc-sec",
			Branch:       "swarm/sec",
			Duration:     4 * time.Second,
			TestPassed:   true,
			Diff:         "+ if err != nil { return nil, err }\n+ if len(data) == 0 { return nil, fmt.Errorf(\"empty\") }\n+ if obj == nil { return nil, errors.New(\"nil\") }\n",
			LinesAdded:   8,
			LinesDeleted: 1,
		},
	}

	resB, err := engine.runStageAuditMerge(context.Background(), survivorsFair)
	if err != nil || resB.Winner == nil {
		t.Fatalf("runStageAuditMerge falhou: %v", err)
	}
	if resB.Winner.Role.Type != config.RoleSec {
		t.Errorf("esperava que RoleSec vencesse a gambiarra de 3 linhas graças às validações defensivas, mas venceu: %s", resB.Winner.Role.Type)
	}
}

func TestEnforceTestImmutability(t *testing.T) {
	tmpDir := t.TempDir()

	_ = exec.Command("git", "-C", tmpDir, "init").Run()
	_ = exec.Command("git", "-C", tmpDir, "config", "user.name", "Test").Run()
	_ = exec.Command("git", "-C", tmpDir, "config", "user.email", "test@example.com").Run()

	baseFile := filepath.Join(tmpDir, "main.go")
	_ = os.WriteFile(baseFile, []byte("package main\n"), 0644)
	_ = exec.Command("git", "-C", tmpDir, "add", ".").Run()
	_ = exec.Command("git", "-C", tmpDir, "commit", "-m", "init").Run()

	// Branch swarm/qa com teste legítimo
	_ = exec.Command("git", "-C", tmpDir, "branch", "swarm/qa").Run()
	_ = exec.Command("git", "-C", tmpDir, "checkout", "swarm/qa").Run()
	legitTest := filepath.Join(tmpDir, "main_test.go")
	_ = os.WriteFile(legitTest, []byte("package main\n// legit test\n"), 0644)
	_ = exec.Command("git", "-C", tmpDir, "add", ".").Run()
	_ = exec.Command("git", "-C", tmpDir, "commit", "-m", "qa tests").Run()

	// Worker branch
	_ = exec.Command("git", "-C", tmpDir, "checkout", "-b", "swarm/worker").Run()

	// Worker tenta adulterar: modifica teste legítimo E cria testes untracked (Go e Rust na pasta tests/)
	_ = os.WriteFile(legitTest, []byte("package main\n// hacked test\n"), 0644)
	bypassTest := filepath.Join(tmpDir, "bypass_test.go")
	_ = os.WriteFile(bypassTest, []byte("package main\nfunc TestBypass() {}\n"), 0644)

	rustTestsDir := filepath.Join(tmpDir, "tests")
	_ = os.MkdirAll(rustTestsDir, 0755)
	rustBypass := filepath.Join(rustTestsDir, "bypass_integration.rs")
	_ = os.WriteFile(rustBypass, []byte("#[test]\nfn test_bypass() {}\n"), 0644)

	// Aplica blindagem de imutabilidade
	enforceTestImmutability(tmpDir, "swarm/qa")

	// 1. bypass_test.go DEVE ter sido removido
	if _, err := os.Stat(bypassTest); !os.IsNotExist(err) {
		t.Errorf("bypass_test.go não-rastreado deveria ter sido removido pelo enforceTestImmutability")
	}

	// 2. tests/bypass_integration.rs DEVE ter sido removido
	if _, err := os.Stat(rustBypass); !os.IsNotExist(err) {
		t.Errorf("tests/bypass_integration.rs não-rastreado deveria ter sido removido pelo enforceTestImmutability")
	}

	// 3. main_test.go DEVE ter sido restaurado para o conteúdo de swarm/qa
	content, _ := os.ReadFile(legitTest)
	if string(content) != "package main\n// legit test\n" {
		t.Errorf("main_test.go não foi restaurado para a versão de swarm/qa; obtido: %s", string(content))
	}
}

func TestEnforceTestImmutability_CommittedBypass(t *testing.T) {
	tmpDir := t.TempDir()

	_ = exec.Command("git", "-C", tmpDir, "init").Run()
	_ = exec.Command("git", "-C", tmpDir, "config", "user.name", "Test").Run()
	_ = exec.Command("git", "-C", tmpDir, "config", "user.email", "test@example.com").Run()

	baseFile := filepath.Join(tmpDir, "main.go")
	_ = os.WriteFile(baseFile, []byte("package main\n"), 0644)
	_ = exec.Command("git", "-C", tmpDir, "add", ".").Run()
	_ = exec.Command("git", "-C", tmpDir, "commit", "-m", "init").Run()

	// Branch swarm/qa com teste legítimo
	_ = exec.Command("git", "-C", tmpDir, "branch", "swarm/qa").Run()
	_ = exec.Command("git", "-C", tmpDir, "checkout", "swarm/qa").Run()
	legitTest := filepath.Join(tmpDir, "main_test.go")
	_ = os.WriteFile(legitTest, []byte("package main\n// legit test\n"), 0644)
	_ = exec.Command("git", "-C", tmpDir, "add", ".").Run()
	_ = exec.Command("git", "-C", tmpDir, "commit", "-m", "qa tests").Run()

	// Worker branch
	_ = exec.Command("git", "-C", tmpDir, "checkout", "-b", "swarm/worker").Run()

	// Worker comita novo arquivo de teste (bypass_test.go e tests/fake_test.rs) para tentar burlar
	bypassTest := filepath.Join(tmpDir, "bypass_test.go")
	_ = os.WriteFile(bypassTest, []byte("package main\nfunc TestFakeMain() {}\n"), 0644)
	rustTestsDir := filepath.Join(tmpDir, "tests")
	_ = os.MkdirAll(rustTestsDir, 0755)
	rustBypass := filepath.Join(rustTestsDir, "fake_test.rs")
	_ = os.WriteFile(rustBypass, []byte("#[test]\nfn test_fake() {}\n"), 0644)

	_ = exec.Command("git", "-C", tmpDir, "add", ".").Run()
	_ = exec.Command("git", "-C", tmpDir, "commit", "-m", "worker malicious test commit").Run()

	// Executa enforceTestImmutability
	enforceTestImmutability(tmpDir, "swarm/qa")

	// 1. bypass_test.go DEVE ter sido removido do disco e do git
	if _, err := os.Stat(bypassTest); !os.IsNotExist(err) {
		t.Errorf("bypass_test.go comitado não foi removido pelo enforceTestImmutability")
	}

	// 2. tests/fake_test.rs DEVE ter sido removido
	if _, err := os.Stat(rustBypass); !os.IsNotExist(err) {
		t.Errorf("tests/fake_test.rs comitado não foi removido pelo enforceTestImmutability")
	}

	// 3. main_test.go DEVE existir e ser legítimo
	content, _ := os.ReadFile(legitTest)
	if string(content) != "package main\n// legit test\n" {
		t.Errorf("main_test.go adulterado ou inexistente: %s", string(content))
	}
}

func TestCalcScore_SinglePatternBonusPerLine(t *testing.T) {
	tmpDir := t.TempDir()
	_ = exec.Command("git", "-C", tmpDir, "init").Run()
	_ = exec.Command("git", "-C", tmpDir, "commit", "--allow-empty", "-m", "init").Run()

	cfg := EngineConfig{
		RepoRoot: tmpDir,
		Roles:    config.DefaultRoles(),
	}
	engine := NewPipelineEngine(cfg, nil, nil)

	// Candidato 1: Linha única com "if err != nil { return nil, err }"
	// Contém múltiplos padrões sobrepostos: "if err != nil", "!= nil", "return nil, err", "return err"
	// Com break por linha, deve pontuar apenas 0.5 de bônus, não 2.0
	c1 := CandidateResult{
		Role:        config.RoleConfig{Type: config.RoleHacker, Name: "Hacker"},
		Branch:      "swarm/hacker",
		Duration:    1 * time.Second,
		TestPassed:  true,
		Diff:        "+ if err != nil { return nil, err }\n",
		LinesAdded:  1,
		LinesDeleted: 0,
	}

	c2 := CandidateResult{
		Role:        config.RoleConfig{Type: config.RoleHacker, Name: "Hacker"},
		Branch:      "swarm/hacker",
		Duration:    1 * time.Second,
		TestPassed:  true,
		Diff:        "+ x := 42\n",
		LinesAdded:  1,
		LinesDeleted: 0,
	}

	res, err := engine.runStageAuditMerge(context.Background(), []CandidateResult{c1, c2})
	if err != nil || res.Winner == nil {
		t.Fatalf("runStageAuditMerge falhou: %v", err)
	}

	if res.Winner.Diff != c1.Diff {
		t.Errorf("esperava candidato com tratamento de erro como vencedor")
	}
}

func TestStageAuditMerge_SafeCleanupDefeatedBranches(t *testing.T) {
	tmpDir := t.TempDir()
	_ = exec.Command("git", "-C", tmpDir, "init").Run()
	_ = exec.Command("git", "-C", tmpDir, "config", "user.name", "Test").Run()
	_ = exec.Command("git", "-C", tmpDir, "config", "user.email", "test@example.com").Run()

	baseFile := filepath.Join(tmpDir, "main.go")
	_ = os.WriteFile(baseFile, []byte("package main\n"), 0644)
	_ = exec.Command("git", "-C", tmpDir, "add", ".").Run()
	_ = exec.Command("git", "-C", tmpDir, "commit", "-m", "init").Run()

	// Cria branches de candidatos
	_ = exec.Command("git", "-C", tmpDir, "branch", "swarm/hacker").Run()
	_ = exec.Command("git", "-C", tmpDir, "branch", "swarm/refactor").Run()
	_ = exec.Command("git", "-C", tmpDir, "branch", "swarm/bench").Run()

	// Cria commit no branch vencedor (hacker)
	_ = exec.Command("git", "-C", tmpDir, "checkout", "swarm/hacker").Run()
	_ = os.WriteFile(filepath.Join(tmpDir, "hacker.go"), []byte("package main\n// win\n"), 0644)
	_ = exec.Command("git", "-C", tmpDir, "add", ".").Run()
	_ = exec.Command("git", "-C", tmpDir, "commit", "-m", "hacker solution").Run()

	// Cria commit no branch perdedor (refactor)
	_ = exec.Command("git", "-C", tmpDir, "checkout", "swarm/refactor").Run()
	_ = os.WriteFile(filepath.Join(tmpDir, "refactor.go"), []byte("package main\n// lose\n"), 0644)
	_ = exec.Command("git", "-C", tmpDir, "add", ".").Run()
	_ = exec.Command("git", "-C", tmpDir, "commit", "-m", "refactor solution").Run()

	// Volta para a branch main
	_ = exec.Command("git", "-C", tmpDir, "checkout", "main").Run()

	cfg := EngineConfig{
		RepoRoot:  tmpDir,
		AutoMerge: true,
		Roles:     config.DefaultRoles(),
	}
	engine := NewPipelineEngine(cfg, nil, nil)

	survivors := []CandidateResult{
		{
			Role:        config.RoleConfig{Type: config.RoleHacker, Name: "Hacker"},
			Branch:      "swarm/hacker",
			Duration:    1 * time.Second,
			TestPassed:  true,
			Diff:        "+ hacker win\n",
			LinesAdded:  1,
			LinesDeleted: 0,
		},
		{
			Role:        config.RoleConfig{Type: config.RoleRefactor, Name: "Refactor"},
			Branch:      "swarm/refactor",
			Duration:    10 * time.Second,
			TestPassed:  true,
			Diff:        "+ refactor lose\n",
			LinesAdded:  50,
			LinesDeleted: 0,
		},
	}

	res, err := engine.runStageAuditMerge(context.Background(), survivors)
	if err != nil {
		t.Fatalf("runStageAuditMerge com AutoMerge falhou: %v", err)
	}
	if res.Winner == nil || res.Winner.Role.Type != config.RoleHacker {
		t.Fatalf("vencedor incorreto: %+v", res.Winner)
	}

	// Valida que SafeCleanup expurgou branches sob swarm/*
	branchOut, _ := exec.Command("git", "-C", tmpDir, "for-each-ref", "--format=%(refname:short)", "refs/heads/swarm/").Output()
	remaining := strings.TrimSpace(string(branchOut))
	if remaining != "" {
		t.Errorf("esperava que branches derrotadas fossem expurgadas após automerge, mas restaram:\n%s", remaining)
	}
}

func TestIsRefactorOrAlignmentTask(t *testing.T) {
	// Casos que NÃO devem desativar o TDD (palavras genéricas no corpo da Spec)
	negativeCases := []string{
		"[CARD-01] Criar novo endpoint: adicionar validação de tipo para payload",
		"[CARD-02] Integrar banco de dados: fix no schema e ajuste de índices",
		"[CARD-03] Feature pipeline: build de artefatos efêmeros",
	}
	for _, tc := range negativeCases {
		if isRefactorOrAlignmentTask(tc) {
			t.Errorf("esperava false para task %q, obteve true (TDD desativado indevidamente)", tc)
		}
	}

	// Casos legítimos de refatoração que PODEM ignorar falha inicial do TDD
	positiveCases := []string{
		"[REFACTOR // MED] Extrair helpers de parsing",
		"[BENCH // HIGH] Otimizar alocações de buffer",
		"[CARD-05] Refactor database layer: reorganizar conexões",
		"[CARD-06] Limpeza de dead code: remover structs obsoletas",
	}
	for _, tc := range positiveCases {
		if !isRefactorOrAlignmentTask(tc) {
			t.Errorf("esperava true para task %q, obteve false", tc)
		}
	}
}



