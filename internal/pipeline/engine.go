package pipeline

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/guardrail"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/lease"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/memory"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/worktree"
)

// StageCallback define a assinatura para notificações de transição de estágio em tempo real.
type StageCallback func(stage StageType, status string, detail string)

// EngineConfig define as configurações para a execução do pipeline de 4 estágios.
type EngineConfig struct {
	RepoRoot      string
	Task          string
	TestCmd       string
	Timeout       time.Duration
	DryRun        bool
	AutoMerge     bool
	Roles         []config.RoleConfig
	CardID        string        // opcional: namespace para isolar worktrees e branches por cartão
	LogWriter     io.Writer     // opcional: streaming em tempo real para tail -f
	OnStageChange StageCallback // opcional: callback para streaming de eventos de estágio
}

// PipelineEngine orquestra a máquina de estados determinística de 4 estágios.
type PipelineEngine struct {
	cfg        EngineConfig
	dispatcher *EphemeralDispatcher
	broker     *lease.Broker
	breaker    *guardrail.CircuitBreaker
	memClient  *memory.Client
	wtMgr      *worktree.Manager
	merger     *worktree.Merger
	builder    *PromptBuilder
}

// NewPipelineEngine inicializa o motor de execução do enxame.
func NewPipelineEngine(cfg EngineConfig, broker *lease.Broker, runner ProcessRunner) *PipelineEngine {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 15 * time.Minute
	}
	if broker == nil {
		broker = lease.NewBroker(nil)
	}
	if runner == nil {
		if cfg.LogWriter != nil {
			runner = NewOSProcessRunnerWithLogger(cfg.LogWriter)
		} else {
			runner = NewOSProcessRunner()
		}
	}

	dispatcher := NewEphemeralDispatcher(runner, broker)
	breaker := guardrail.NewCircuitBreaker(guardrail.DefaultConfig())
	memClient := memory.NewClient()
	var wtMgr *worktree.Manager
	if cfg.CardID != "" {
		wtMgr = worktree.NewNamespacedManager(cfg.RepoRoot, cfg.Roles, cfg.CardID)
	} else {
		wtMgr = worktree.NewManager(cfg.RepoRoot, cfg.Roles)
	}
	merger := worktree.NewMerger(cfg.RepoRoot)
	builder := NewPromptBuilder(cfg.Task)

	// Injeta CARGO_TARGET_DIR compartilhado para projetos Rust se não estiver definido
	if _, err := os.Stat(filepath.Join(cfg.RepoRoot, "Cargo.toml")); err == nil {
		if os.Getenv("CARGO_TARGET_DIR") == "" {
			_ = os.Setenv("CARGO_TARGET_DIR", filepath.Join(cfg.RepoRoot, "target"))
		}
	}

	return &PipelineEngine{
		cfg:        cfg,
		dispatcher: dispatcher,
		broker:     broker,
		breaker:    breaker,
		memClient:  memClient,
		wtMgr:      wtMgr,
		merger:     merger,
		builder:    builder,
	}
}

func (e *PipelineEngine) emitEvent(stage StageType, status, detail string) {
	if e.cfg.OnStageChange != nil {
		e.cfg.OnStageChange(stage, status, detail)
	}
	ts := time.Now().Format("15:04:05")
	icon := "⚡"
	switch stage {
	case StageContract:
		icon = "📐"
	case StageRedTeam:
		icon = "🧪"
	case StageSpeculative:
		icon = "⚔️"
	case StageAuditMerge:
		icon = "🏆"
	}
	prefix := ""
	if e.cfg.CardID != "" {
		prefix = fmt.Sprintf("[%s] ", e.cfg.CardID)
	}
	line := fmt.Sprintf("[%s] %s %s[%s] %s", ts, icon, prefix, stage, detail)
	if e.cfg.LogWriter != nil {
		_, _ = fmt.Fprintln(e.cfg.LogWriter, line)
	}
	fmt.Println(line)
}

// Execute roda os 4 estágios de forma determinística e adversarial.
func (e *PipelineEngine) Execute(ctx context.Context) ([]StageResult, error) {
	var results []StageResult

	// Garante que worktrees base estejam provisionados
	if !e.cfg.DryRun {
		_ = e.wtMgr.EnsureGitRepo()
		_ = e.wtMgr.EnsureGitIgnore()
	}

	// -------------------------------------------------------------
	// ESTÁGIO 1: Contratos e Interfaces (Arquiteto)
	// -------------------------------------------------------------
	archRole := e.findRole(config.RoleArch)
	e.emitEvent(StageContract, "STARTED", fmt.Sprintf("Arquiteto (%s) definindo interfaces e contratos...", archRole.Name))
	stage1Res, contractDiff, err := e.runStageContract(ctx, archRole)
	results = append(results, stage1Res)
	if err != nil {
		e.emitEvent(StageContract, "FAILED", fmt.Sprintf("Contratos falharam: %v", err))
		return results, fmt.Errorf("falha no Estágio 1 (Contratos): %w", err)
	}
	e.emitEvent(StageContract, "COMPLETED", fmt.Sprintf("Contratos e especificações gerados com sucesso por %s.", archRole.Name))

	// -------------------------------------------------------------
	// ESTÁGIO 2: Red-Team TDD (Testes Adversariais - QA/Sec)
	// -------------------------------------------------------------
	qaRole := e.findRole(config.RoleQA)
	e.emitEvent(StageRedTeam, "STARTED", fmt.Sprintf("QA (%s) gerando testes adversariais...", qaRole.Name))
	stage2Res, testDiff, failingOutput, err := e.runStageRedTeam(ctx, qaRole, contractDiff)
	results = append(results, stage2Res)
	if err != nil {
		e.emitEvent(StageRedTeam, "FAILED", fmt.Sprintf("Testes adversariais falharam: %v", err))
		return results, fmt.Errorf("falha no Estágio 2 (Red-Team TDD): %w", err)
	}
	e.emitEvent(StageRedTeam, "COMPLETED", fmt.Sprintf("Suíte de testes adversariais gerada por %s.", qaRole.Name))

	// -------------------------------------------------------------
	// ESTÁGIO 3: Execução Especulativa & Competição (Workers: Hacker, etc.)
	// -------------------------------------------------------------
	workers := e.getWorkerRoles()
	e.emitEvent(StageSpeculative, "STARTED", fmt.Sprintf("Workers (%d concorrentes) iniciando implementação Best-of-N...", len(workers)))
	stage3Res, survivors, err := e.runStageSpeculative(ctx, contractDiff, testDiff, failingOutput)
	results = append(results, stage3Res)
	if err != nil {
		e.emitEvent(StageSpeculative, "FAILED", fmt.Sprintf("Competição falhou: %v", err))
		return results, fmt.Errorf("falha no Estágio 3 (Competição): %w", err)
	}
	if len(survivors) == 0 {
		e.emitEvent(StageSpeculative, "FAILED", "Nenhum worker sobreviveu ao filtro frio de testes")
		return results, fmt.Errorf("nenhum worker sobreviveu ao filtro frio de testes no Estágio 3")
	}
	e.emitEvent(StageSpeculative, "COMPLETED", fmt.Sprintf("%d workers passaram no Quality Gate.", len(survivors)))

	// -------------------------------------------------------------
	// ESTÁGIO 4: Audit & Merge Gate (Maestro / Review)
	// -------------------------------------------------------------
	e.emitEvent(StageAuditMerge, "STARTED", "Auditando candidatos e selecionando solução vencedora...")
	stage4Res, err := e.runStageAuditMerge(ctx, survivors)
	results = append(results, stage4Res)
	if err != nil {
		e.emitEvent(StageAuditMerge, "FAILED", fmt.Sprintf("Merge Gate falhou: %v", err))
		return results, fmt.Errorf("falha no Estágio 4 (Merge Gate): %w", err)
	}
	if stage4Res.Winner != nil {
		e.emitEvent(StageAuditMerge, "COMPLETED", fmt.Sprintf("Solução de %s (%s) selecionada (+%d/-%d).",
			stage4Res.Winner.Role.Name, stage4Res.Winner.AccountID, stage4Res.Winner.LinesAdded, stage4Res.Winner.LinesDeleted))
	}

	return results, nil
}

func (e *PipelineEngine) runStageContract(ctx context.Context, role config.RoleConfig) (StageResult, string, error) {
	start := time.Now()
	res := StageResult{Stage: StageContract}

	if e.cfg.DryRun {
		res.Success = true
		res.Duration = time.Since(start)
		res.ArtifactMsg = fmt.Sprintf("[DRY-RUN] Contratos simulados para %s.", role.Name)
		return res, "// contract stubs mock", nil
	}

	wtDir, err := e.wtMgr.ProvisionRoleWorktree(role)
	if err != nil {
		res.Error = err
		return res, "", err
	}

	baseSHAOut, _ := exec.Command("git", "-C", wtDir, "rev-parse", "HEAD").Output()
	baseSHA := strings.TrimSpace(string(baseSHAOut))

	promptContent := e.builder.BuildContractPrompt(role, "")
	promptFile, err := WritePromptFile(e.cfg.RepoRoot, "stage1-contract", promptContent)
	if err != nil {
		res.Error = err
		return res, "", err
	}
	defer os.Remove(promptFile)

	_, exitCode, leaseAcquired, err := e.dispatcher.DispatchWithFailover(ctx, wtDir, role, string(StageContract), promptFile, e.cfg.Timeout)
	if leaseAcquired != nil {
		defer e.broker.Release(leaseAcquired)
	}
	if err != nil || exitCode != 0 {
		res.Error = fmt.Errorf("execução efêmera falhou (exit: %d): %v", exitCode, err)
		return res, "", res.Error
	}

	// Validação de compilação estrita dos stubs de contrato (Stage 1)
	if _, statErr := os.Stat(filepath.Join(wtDir, "go.mod")); statErr == nil {
		buildOut, bCode, _ := e.dispatcher.Runner.RunCommand(ctx, wtDir, "go build ./...", 2*time.Minute)
		if bCode != 0 {
			res.Error = fmt.Errorf("contrato inválido: stubs Go não compilam:\n%s", buildOut)
			return res, "", res.Error
		}
	} else if _, statErr := os.Stat(filepath.Join(wtDir, "Cargo.toml")); statErr == nil {
		buildCmd := "cargo check --workspace"
		if e.cfg.TestCmd != "" && strings.Contains(e.cfg.TestCmd, "-p ") {
			parts := strings.Fields(e.cfg.TestCmd)
			for i, p := range parts {
				if p == "-p" && i+1 < len(parts) {
					buildCmd = fmt.Sprintf("cargo check -p %s", parts[i+1])
					break
				}
			}
		}
		buildOut, bCode, _ := e.dispatcher.Runner.RunCommand(ctx, wtDir, buildCmd, 5*time.Minute)
		if bCode != 0 {
			res.Error = fmt.Errorf("contrato inválido: stubs Rust não compilam:\n%s", buildOut)
			return res, "", res.Error
		}
	}

	// Garante que todas as alterações do Arquiteto estejam comitadas no branch swarm/arch
	autoCommit(wtDir, fmt.Sprintf("feat(spec): contratos e interfaces definidos por %s", role.Name))

	// Extrai o git diff gerado pelo arquiteto contra o commit base inicial
	diff, _ := e.getGitDiff(wtDir, baseSHA, "HEAD")
	if diff == "" && baseSHA != "" {
		diff, _ = e.getGitDiff(wtDir, baseSHA, "")
	}

	res.Success = true
	res.Duration = time.Since(start)
	res.ArtifactMsg = fmt.Sprintf("Contratos e especificações gerados com sucesso por %s.", role.Name)

	return res, diff, nil
}

func (e *PipelineEngine) runStageRedTeam(ctx context.Context, role config.RoleConfig, contractDiff string) (StageResult, string, string, error) {
	start := time.Now()
	res := StageResult{Stage: StageRedTeam}

	if e.cfg.DryRun {
		res.Success = true
		res.Duration = time.Since(start)
		res.ArtifactMsg = fmt.Sprintf("[DRY-RUN] Testes adversariais simulados para %s.", role.Name)
		return res, "// test suite mock", "FAIL: TestContractCompliance (unimplemented)", nil
	}

	// Estágio 2 cria worktree ramificada a partir do branch do Estágio 1,
	// garantindo que os contratos já existem fisicamente no disco para o QA compilar
	archRole := e.findRole(config.RoleArch)
	archBranch := e.wtMgr.BranchForRole(archRole)
	wtDir, err := e.wtMgr.ProvisionRoleWorktreeFromBase(role, archBranch)
	if err != nil {
		res.Error = err
		return res, "", "", err
	}

	promptContent := e.builder.BuildRedTeamPrompt(role, contractDiff)
	promptFile, err := WritePromptFile(e.cfg.RepoRoot, "stage2-redteam", promptContent)
	if err != nil {
		res.Error = err
		return res, "", "", err
	}
	defer os.Remove(promptFile)

	_, exitCode, leaseAcquired, err := e.dispatcher.DispatchWithFailover(ctx, wtDir, role, string(StageRedTeam), promptFile, e.cfg.Timeout)
	if leaseAcquired != nil {
		defer e.broker.Release(leaseAcquired)
	}
	if err != nil || exitCode != 0 {
		res.Error = fmt.Errorf("red-team efêmero falhou (exit: %d): %v", exitCode, err)
		return res, "", "", res.Error
	}

	// Garante que todos os arquivos de teste novos/modificados estejam comitados no branch swarm/qa
	autoCommit(wtDir, fmt.Sprintf("test(red-team): suite de testes gerada por %s", role.Name))

	// Condição A: Compilação dos testes sem execução DEVE passar (exit 0)
	// Garante que o teste é sintaticamente válido e respeita os stubs do Arch
	if _, statErr := os.Stat(filepath.Join(wtDir, "go.mod")); statErr == nil {
		cOut, cCode, _ := e.dispatcher.Runner.RunCommand(ctx, wtDir, "go test -run=^$ ./...", 2*time.Minute)
		if cCode != 0 {
			res.Error = fmt.Errorf("testes inválidos: QA escreveu testes que não compilam contra as interfaces do Arch:\n%s", cOut)
			return res, "", "", res.Error
		}
	} else if _, statErr := os.Stat(filepath.Join(wtDir, "Cargo.toml")); statErr == nil {
		cTestCmd := "cargo test --workspace --no-run"
		if e.cfg.TestCmd != "" && strings.Contains(e.cfg.TestCmd, "-p ") {
			parts := strings.Fields(e.cfg.TestCmd)
			for i, p := range parts {
				if p == "-p" && i+1 < len(parts) {
					cTestCmd = fmt.Sprintf("cargo test -p %s --no-run", parts[i+1])
					break
				}
			}
		}
		cOut, cCode, _ := e.dispatcher.Runner.RunCommand(ctx, wtDir, cTestCmd, 5*time.Minute)
		if cCode != 0 {
			res.Error = fmt.Errorf("testes inválidos: testes Rust não compilam contra os traits/structs do Arch:\n%s", cOut)
			return res, "", "", res.Error
		}
	}

	// Condição B: Execução dos testes DEVE FALHAR neste momento contra os stubs vazios (exceto para build/check ou tarefas de refatoração)
	testOutput := ""
	if e.cfg.TestCmd != "" {
		out, tCode, _ := e.dispatcher.Runner.RunCommand(ctx, wtDir, e.cfg.TestCmd, 2*time.Minute)
		testOutput = out
		if isBuildOrCheckCmd(e.cfg.TestCmd) {
			if tCode != 0 {
				res.Error = fmt.Errorf("validação de build/check falhou no Estágio 2:\n%s", out)
				return res, "", "", res.Error
			}
		} else if tCode == 0 && !isRefactorOrAlignmentTask(e.cfg.Task) {
			res.Error = fmt.Errorf("testes inválidos: testes passaram imediatamente sem nenhuma implementação (tautologia vazia)")
			return res, "", "", res.Error
		}
	}

	diff, _ := e.getGitDiff(wtDir, archBranch, "HEAD")
	if diff == "" {
		diff, _ = e.getGitDiff(wtDir, archBranch, "")
	}

	res.Success = true
	res.Duration = time.Since(start)
	res.ArtifactMsg = fmt.Sprintf("Suíte de testes de verificação adversarial gerada por %s.", role.Name)

	return res, diff, testOutput, nil
}

func (e *PipelineEngine) runStageSpeculative(ctx context.Context, contractDiff string, testDiff string, failingOutput string) (StageResult, []CandidateResult, error) {
	start := time.Now()
	res := StageResult{Stage: StageSpeculative}

	workers := e.getWorkerRoles()
	if len(workers) == 0 {
		workers = []config.RoleConfig{e.findRole(config.RoleHacker)}
	}

	if e.cfg.DryRun {
		var survivors []CandidateResult
		for _, w := range workers {
			survivors = append(survivors, CandidateResult{
				Role:        w,
				AccountID:   "default",
				WorktreeDir: e.cfg.RepoRoot,
				Branch:      fmt.Sprintf("swarm/%s", w.Type),
				TestPassed:  true,
				LinesAdded:  15,
				LinesDeleted: 2,
				Duration:    10 * time.Millisecond,
			})
		}
		res.Success = true
		res.Duration = time.Since(start)
		res.Candidates = survivors
		return res, survivors, nil
	}

	var survivors []CandidateResult
	var mu sync.Mutex
	var wg sync.WaitGroup

	qaRole := e.findRole(config.RoleQA)
	qaBranch := e.wtMgr.BranchForRole(qaRole)

	// Execução concorrente real de todos os workers competindo em worktrees isoladas
	for _, w := range workers {
		wg.Add(1)
		go func(worker config.RoleConfig) {
			defer wg.Done()
			wStart := time.Now()

			// Cada worker parte de qaBranch, contendo os contratos e testes já no disco!
			wtDir, err := e.wtMgr.ProvisionRoleWorktreeFromBase(worker, qaBranch)
			if err != nil {
				return
			}

			promptContent := e.builder.BuildWorkerPrompt(worker, contractDiff, testDiff, failingOutput)
			promptFile, err := WritePromptFile(e.cfg.RepoRoot, fmt.Sprintf("stage3-%s", worker.Type), promptContent)
			if err != nil {
				return
			}

			// Disjuntor isolado por worker: falha de um especialista não contamina os outros!
			localBreaker := guardrail.NewCircuitBreaker(guardrail.DefaultConfig())

			allowed, reason := localBreaker.AllowExecution()
			if !allowed {
				_ = os.Remove(promptFile)
				mu.Lock()
				res.Error = fmt.Errorf("circuito aberto para %s: %s", worker.Name, reason)
				mu.Unlock()
				return
			}

			_, exitCode, leaseAcquired, err := e.dispatcher.DispatchWithFailover(ctx, wtDir, worker, string(StageSpeculative), promptFile, e.cfg.Timeout)
			accountID := "unknown"
			if leaseAcquired != nil {
				accountID = leaseAcquired.AccountID
				e.broker.Release(leaseAcquired)
			}
			_ = os.Remove(promptFile)

			candidate := CandidateResult{
				Role:        worker,
				AccountID:   accountID,
				WorktreeDir: wtDir,
				Branch:      e.wtMgr.BranchForRole(worker),
				ExitCode:    exitCode,
				Duration:    time.Since(wStart),
				Error:       err,
			}

			// Blindagem contra Reward Hacking: Remove testes untracked e restaura os testes do Estágio 2
			enforceTestImmutability(wtDir, qaBranch)

			// Filtro Frio: Executa o test-cmd na worktree do candidato
			if e.cfg.TestCmd != "" {
				testOut, tExit, _ := e.dispatcher.Runner.RunCommand(ctx, wtDir, e.cfg.TestCmd, 2*time.Minute)
				candidate.TestOutput = testOut
				candidate.TestPassed = (tExit == 0)

				_ = exec.Command("git", "-C", wtDir, "add", "-N", ".").Run()
				diff, _ := e.getGitDiff(wtDir, qaBranch, "")
				candidate.Diff = diff
				candidate.LinesAdded, candidate.LinesDeleted = countDiffLines(diff)
				if candidate.LinesAdded == 0 && candidate.LinesDeleted == 0 {
					candidate.TestPassed = false
					testOut = "ERRO: O worker encerrou sem produzir nenhuma alteração de código (diff vazio contra qaBranch)."
					candidate.TestOutput = testOut
				}

				// Loop de Auto-Cura (Self-Healing) isolado com disjuntor local
				maxSelfHeal := 2
				for healAttempt := 1; !candidate.TestPassed && healAttempt <= maxSelfHeal; healAttempt++ {
					opened, _ := localBreaker.RecordFailure(diff, testOut)
					if opened {
						break
					}

					e.emitEvent(StageSpeculative, "SELF_HEALING", fmt.Sprintf("%s auto-cura (tentativa %d/%d)...", worker.Name, healAttempt, maxSelfHeal))
					healPrompt := e.builder.BuildSelfHealingPrompt(worker, testOut, healAttempt)
					healFile, pErr := WritePromptFile(e.cfg.RepoRoot, fmt.Sprintf("heal-%s-%d", worker.Type, healAttempt), healPrompt)
					if pErr != nil {
						break
					}

					_, _, healLease, _ := e.dispatcher.DispatchWithFailover(ctx, wtDir, worker, "StageSelfHealing", healFile, e.cfg.Timeout)
					if healLease != nil {
						e.broker.Release(healLease)
					}
					_ = os.Remove(healFile)

					// Restaura testes para garantir imutabilidade pós-reparo (limpa untracked e restaura QA)
					enforceTestImmutability(wtDir, qaBranch)

					// Reavalia Filtro Frio
					testOut, tExit, _ = e.dispatcher.Runner.RunCommand(ctx, wtDir, e.cfg.TestCmd, 2*time.Minute)
					candidate.TestOutput = testOut
					candidate.TestPassed = (tExit == 0)
					_ = exec.Command("git", "-C", wtDir, "add", "-N", ".").Run()
					diff, _ = e.getGitDiff(wtDir, qaBranch, "")
					candidate.Diff = diff
					candidate.LinesAdded, candidate.LinesDeleted = countDiffLines(diff)
					if candidate.LinesAdded == 0 && candidate.LinesDeleted == 0 {
						candidate.TestPassed = false
						testOut = "ERRO: O worker encerrou sem produzir nenhuma alteração de código (diff vazio contra qaBranch)."
						candidate.TestOutput = testOut
					}
				}
			} else {
				candidate.TestPassed = (exitCode == 0)
			}

			// Se passou no teste, comita a solução do worker na worktree
			if candidate.TestPassed {
				autoCommit(wtDir, fmt.Sprintf("feat(impl): solucao implementada por %s", worker.Name))
				localBreaker.RecordSuccess()
				mu.Lock()
				survivors = append(survivors, candidate)
				mu.Unlock()
			}

			mu.Lock()
			res.Candidates = append(res.Candidates, candidate)
			mu.Unlock()
		}(w)
	}

	wg.Wait()

	res.Success = (len(survivors) > 0)
	res.Duration = time.Since(start)
	return res, survivors, nil
}

func (e *PipelineEngine) runStageAuditMerge(ctx context.Context, survivors []CandidateResult) (StageResult, error) {
	start := time.Now()
	res := StageResult{Stage: StageAuditMerge}

	if len(survivors) == 0 {
		res.Error = fmt.Errorf("nenhum candidato qualificado para o merge")
		return res, res.Error
	}

	// Critério de desempate determinístico com incentivo a robustez e código defensivo equilibrado:
	// - Adições são ponderadas suavemente (0.1), enquanto churn de exclusão recebe peso 0.3.
	// - Inspeciona apenas linhas adicionadas (+), ignorando linhas inalteradas, exclusões e comentários.
	// - Pontua 0.5 por indicador de tratamento de erro e código defensivo (teto 10.0).
	// - Concede bônus moderado de papel (2.0 para RoleSec, 1.0 para RoleRefactor).
	calcScore := func(c CandidateResult) float64 {
		durSec := c.Duration.Seconds()
		churn := (float64(c.LinesDeleted) * 0.3) + (float64(c.LinesAdded) * 0.1)

		robustnessBonus := 0.0
		errorPatterns := []string{
			"if err != nil", "if err == nil", "fmt.errorf", "errors.new",
			"return nil, err", "return err", "!= nil", "== nil",
			"len(", "match ", "unwrap_or", "result<", "ok(",
		}

		// Inspeciona apenas linhas de código adicionadas (+), excluindo cabeçalhos e comentários
		for _, rawLine := range strings.Split(c.Diff, "\n") {
			if strings.HasPrefix(rawLine, "+") && !strings.HasPrefix(rawLine, "+++") {
				line := strings.TrimSpace(strings.TrimPrefix(rawLine, "+"))
				if strings.HasPrefix(line, "//") || strings.HasPrefix(line, "/*") || strings.HasPrefix(line, "*") || strings.HasPrefix(line, "#") {
					continue
				}
				lineLower := strings.ToLower(line)
				for _, pattern := range errorPatterns {
					if strings.Contains(lineLower, pattern) {
						robustnessBonus += 0.5
						break
					}
				}
			}
		}

		// Incentivo de robustez moderado por papel
		switch c.Role.Type {
		case config.RoleSec:
			robustnessBonus += 2.0 // Bônus moderado para o agente de Segurança
		case config.RoleRefactor:
			robustnessBonus += 1.0 // Bônus moderado para refatoração limpa
		}

		if robustnessBonus > 10.0 {
			robustnessBonus = 10.0
		}

		return churn + (durSec * 0.05) - robustnessBonus
	}

	best := survivors[0]
	bestScore := calcScore(best)

	for i := 1; i < len(survivors); i++ {
		score := calcScore(survivors[i])
		if score < bestScore && survivors[i].TestPassed {
			best = survivors[i]
			bestScore = score
		}
	}

	res.Winner = &best

	// Executa o merge se AutoMerge estiver ativo ou em modo interativo utilizando e.merger
	if e.cfg.AutoMerge && !e.cfg.DryRun {
		commitMsg := fmt.Sprintf("swarm(pipeline): integrando solucao vencedora de %s (%s)", best.Role.Name, best.AccountID)
		if err := e.merger.MergeBranch(ctx, best.Branch, commitMsg, e.cfg.TestCmd); err != nil {
			res.Error = err
			return res, res.Error
		}

		// Assim que o merge automático de um pipeline for concluído com sucesso,
		// expurga as worktrees e branches descartadas daquele ciclo
		_ = e.wtMgr.SafeCleanup(true)
	}

	res.Success = true
	res.Duration = time.Since(start)
	res.ArtifactMsg = fmt.Sprintf("Solução vencedora de %s (%s) selecionada e auditada.", best.Role.Name, best.AccountID)

	_ = e.memClient.SendMessage("pipeline", "swarm-lead", fmt.Sprintf("Ciclo concluído! Vencedor: %s. Linhas: +%d/-%d.", best.Role.Name, best.LinesAdded, best.LinesDeleted))

	return res, nil
}

func (e *PipelineEngine) findRole(rt config.RoleType) config.RoleConfig {
	for _, r := range e.cfg.Roles {
		if r.Type == rt {
			return r
		}
	}
	// Fallback para AllRoles
	for _, r := range config.AllRoles() {
		if r.Type == rt {
			return r
		}
	}
	return config.DefaultRoles()[0]
}

func (e *PipelineEngine) getWorkerRoles() []config.RoleConfig {
	var workers []config.RoleConfig
	seen := make(map[config.RoleType]bool)

	for _, r := range e.cfg.Roles {
		if r.Type != config.RoleLead && r.Type != config.RoleArch && r.Type != config.RoleQA {
			workers = append(workers, r)
			seen[r.Type] = true
		}
	}

	// Garante pelo menos 3 concorrentes para competição real Best-of-N
	if len(workers) < 3 {
		candidates := []config.RoleType{config.RoleHacker, config.RoleRefactor, config.RoleBench, config.RoleSec}
		all := config.AllRoles()
		for _, c := range candidates {
			if len(workers) >= 3 {
				break
			}
			if !seen[c] {
				for _, r := range all {
					if r.Type == c {
						workers = append(workers, r)
						seen[c] = true
						break
					}
				}
			}
		}
	}

	return workers
}

// autoCommit comita alterações não salvas ou arquivos untracked garantindo rastreabilidade entre estágios.
func autoCommit(dir, message string) {
	statusCmd := exec.Command("git", "-C", dir, "status", "--porcelain")
	out, err := statusCmd.Output()
	if err == nil && len(strings.TrimSpace(string(out))) > 0 {
		_ = exec.Command("git", "-C", dir, "add", "-A").Run()
		_ = exec.Command("git", "-C", dir, "commit", "-m", message).Run()
	}
}

func (e *PipelineEngine) getGitDiff(dir, from, to string) (string, error) {
	args := []string{"-C", dir, "diff"}
	if from != "" && to != "" {
		args = append(args, fmt.Sprintf("%s...%s", from, to))
	} else if from != "" {
		args = append(args, from)
	}
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	return string(out), err
}

func countDiffLines(diff string) (int, int) {
	added, deleted := 0, 0
	lines := strings.Split(diff, "\n")
	for _, l := range lines {
		if strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++") {
			added++
		} else if strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "---") {
			deleted++
		}
	}
	return added, deleted
}

// enforceTestImmutability remove testes untracked e testes comitados criados pelo worker após qaBranch,
// restaurando a suíte do Estágio 2 (QA) e blindando contra ataques de reward hacking (ex: bypass_test.go ou testes Rust).
func enforceTestImmutability(wtDir, qaBranch string) {
	testGlobs := []string{"*_test.go", "**/*_test.go", "*_test.rs", "**/*_test.rs", "tests/**", "**/tests/**"}

	// 1. Remove quaisquer ficheiros de teste adicionados/comitados após qaBranch
	diffArgs := append([]string{"-C", wtDir, "diff", "--name-only", "--diff-filter=A", qaBranch, "--"}, testGlobs...)
	if out, err := exec.Command("git", diffArgs...).Output(); err == nil {
		for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			f = strings.TrimSpace(f)
			if f != "" {
				_ = exec.Command("git", "-C", wtDir, "rm", "-f", f).Run()
				_ = os.Remove(filepath.Join(wtDir, f))
			}
		}
	}

	// 2. Remove ficheiros de teste untracked
	cleanArgs := append([]string{"-C", wtDir, "clean", "-f", "-d", "--"}, testGlobs...)
	_ = exec.Command("git", cleanArgs...).Run()

	// 3. Restaura exatamente os arquivos de teste de qaBranch em modo --no-overlay
	for _, glob := range testGlobs {
		_ = exec.Command("git", "-C", wtDir, "checkout", "--no-overlay", qaBranch, "--", glob).Run()
	}
}

func isBuildOrCheckCmd(cmd string) bool {
	lower := strings.ToLower(cmd)
	parts := strings.Fields(lower)
	for _, p := range parts {
		if p == "build" || p == "check" || p == "tsc" || p == "lint" || p == "typecheck" {
			return true
		}
	}
	return strings.Contains(lower, "run build") || strings.Contains(lower, "run check") || strings.Contains(lower, "run lint")
}

func isRefactorOrAlignmentTask(task string) bool {
	lower := strings.ToLower(task)
	if strings.Contains(lower, "[refactor //") || strings.Contains(lower, "[bench //") {
		return true
	}
	// Avalia apenas o cabeçalho/título (antes dos dois-pontos da Spec)
	header := lower
	if idx := strings.Index(lower, ":"); idx != -1 {
		header = lower[:idx]
	}
	keywords := []string{"refactor", "refator", "cleanup", "limpeza", "migra"}
	for _, kw := range keywords {
		if strings.Contains(header, kw) {
			return true
		}
	}
	return false
}

