package pipeline

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/guardrail"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/lease"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/memory"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/worktree"
)

// EngineConfig define as configurações para a execução do pipeline de 4 estágios.
type EngineConfig struct {
	RepoRoot   string
	Task       string
	TestCmd    string
	Timeout    time.Duration
	DryRun     bool
	AutoMerge  bool
	Roles      []config.RoleConfig
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
		runner = NewOSProcessRunner()
	}

	dispatcher := NewEphemeralDispatcher(runner, broker)
	breaker := guardrail.NewCircuitBreaker(guardrail.DefaultConfig())
	memClient := memory.NewClient()
	wtMgr := worktree.NewManager(cfg.RepoRoot, cfg.Roles)
	merger := worktree.NewMerger(cfg.RepoRoot)
	builder := NewPromptBuilder(cfg.Task)

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
	stage1Res, contractDiff, err := e.runStageContract(ctx, archRole)
	results = append(results, stage1Res)
	if err != nil {
		return results, fmt.Errorf("falha no Estágio 1 (Contratos): %w", err)
	}

	// -------------------------------------------------------------
	// ESTÁGIO 2: Red-Team TDD (Testes Adversariais - QA/Sec)
	// -------------------------------------------------------------
	qaRole := e.findRole(config.RoleQA)
	stage2Res, testDiff, failingOutput, err := e.runStageRedTeam(ctx, qaRole, contractDiff)
	results = append(results, stage2Res)
	if err != nil {
		return results, fmt.Errorf("falha no Estágio 2 (Red-Team TDD): %w", err)
	}

	// -------------------------------------------------------------
	// ESTÁGIO 3: Execução Especulativa & Competição (Workers: Hacker, etc.)
	// -------------------------------------------------------------
	stage3Res, survivors, err := e.runStageSpeculative(ctx, contractDiff, testDiff, failingOutput)
	results = append(results, stage3Res)
	if err != nil {
		return results, fmt.Errorf("falha no Estágio 3 (Competição): %w", err)
	}
	if len(survivors) == 0 {
		return results, fmt.Errorf("nenhum worker sobreviveu ao filtro frio de testes no Estágio 3")
	}

	// -------------------------------------------------------------
	// ESTÁGIO 4: Audit & Merge Gate (Maestro / Review)
	// -------------------------------------------------------------
	stage4Res, err := e.runStageAuditMerge(ctx, survivors)
	results = append(results, stage4Res)
	if err != nil {
		return results, fmt.Errorf("falha no Estágio 4 (Merge Gate): %w", err)
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

	// Extrai o git diff gerado pelo arquiteto
	diff, _ := e.getGitDiff(wtDir, "HEAD~1", "HEAD")
	if diff == "" {
		// Se não comitou ainda, pega o diff staged/untracked
		diff, _ = e.getGitDiff(wtDir, "", "")
	}

	res.Success = true
	res.Duration = time.Since(start)
	res.ArtifactMsg = fmt.Sprintf("Contratos e especificações gerados com sucesso por %s.", role.Name)

	// Grava artefato no ai-memory sem gastar tokens do LLM
	_ = e.memClient.SendMessage("swarm-arch", "swarm-lead", fmt.Sprintf("Contratos Estágio 1 prontos:\n%s", diff))

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

	wtDir, err := e.wtMgr.ProvisionRoleWorktree(role)
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

	// Executa o test-cmd: DEVE FALHAR neste momento
	testOutput := ""
	if e.cfg.TestCmd != "" {
		out, tCode, _ := e.dispatcher.Runner.RunCommand(ctx, wtDir, e.cfg.TestCmd, 2*time.Minute)
		testOutput = out
		if tCode == 0 {
			// Se passou sem código, os testes não estão testando o comportamento esperado!
			// Não bloqueamos com erro fatal, mas emitimos alerta nos artefatos
			testOutput = "AVISO: Testes passaram imediatamente (pode indicar ausência de asserções negativas estritas)."
		}
	}

	diff, _ := e.getGitDiff(wtDir, "HEAD~1", "HEAD")
	if diff == "" {
		diff, _ = e.getGitDiff(wtDir, "", "")
	}

	res.Success = true
	res.Duration = time.Since(start)
	res.ArtifactMsg = fmt.Sprintf("Suíte de testes de verificação adversarial gerada por %s.", role.Name)

	_ = e.memClient.SendMessage("swarm-qa", "swarm-lead", fmt.Sprintf("Testes Estágio 2 prontos:\n%s", diff))

	return res, diff, testOutput, nil
}

func (e *PipelineEngine) runStageSpeculative(ctx context.Context, contractDiff string, testDiff string, failingOutput string) (StageResult, []CandidateResult, error) {
	start := time.Now()
	res := StageResult{Stage: StageSpeculative}

	workers := e.getWorkerRoles()
	if len(workers) == 0 {
		workers = []config.RoleConfig{e.findRole(config.RoleHacker)}
	}

	var survivors []CandidateResult

	for _, w := range workers {
		wStart := time.Now()

		if e.cfg.DryRun {
			survivors = append(survivors, CandidateResult{
				Role:        w,
				AccountID:   "default",
				WorktreeDir: e.cfg.RepoRoot,
				Branch:      fmt.Sprintf("swarm/%s", w.Type),
				TestPassed:  true,
				LinesAdded:  15,
				LinesDeleted: 2,
				Duration:    time.Since(wStart),
			})
			continue
		}

		wtDir, err := e.wtMgr.ProvisionRoleWorktree(w)
		if err != nil {
			continue
		}

		promptContent := e.builder.BuildWorkerPrompt(w, contractDiff, testDiff, failingOutput)
		promptFile, err := WritePromptFile(e.cfg.RepoRoot, fmt.Sprintf("stage3-%s", w.Type), promptContent)
		if err != nil {
			continue
		}

		allowed, reason := e.breaker.AllowExecution()
		if !allowed {
			_ = os.Remove(promptFile)
			res.Error = fmt.Errorf("circuito aberto pelo Circuit Breaker: %s", reason)
			return res, nil, res.Error
		}

		_, exitCode, leaseAcquired, err := e.dispatcher.DispatchWithFailover(ctx, wtDir, w, string(StageSpeculative), promptFile, e.cfg.Timeout)
		accountID := "unknown"
		if leaseAcquired != nil {
			accountID = leaseAcquired.AccountID
			e.broker.Release(leaseAcquired)
		}
		_ = os.Remove(promptFile)

		candidate := CandidateResult{
			Role:        w,
			AccountID:   accountID,
			WorktreeDir: wtDir,
			Branch:      fmt.Sprintf("swarm/%s", w.Type),
			ExitCode:    exitCode,
			Duration:    time.Since(wStart),
			Error:       err,
		}

		// Filtro Frio: Executa o test-cmd na worktree do candidato
		if e.cfg.TestCmd != "" {
			testOut, tExit, _ := e.dispatcher.Runner.RunCommand(ctx, wtDir, e.cfg.TestCmd, 2*time.Minute)
			candidate.TestOutput = testOut
			candidate.TestPassed = (tExit == 0)

			diff, _ := e.getGitDiff(wtDir, "HEAD~1", "HEAD")
			candidate.Diff = diff
			candidate.LinesAdded, candidate.LinesDeleted = countDiffLines(diff)

			if !candidate.TestPassed {
				// Registra falha no Circuit Breaker com detecção de Doom Loop
				opened, loopReason := e.breaker.RecordFailure(diff, testOut)
				if opened {
					res.Error = fmt.Errorf("disjuntor abriu durante o Estágio 3: %s", loopReason)
					res.Candidates = append(res.Candidates, candidate)
					return res, nil, res.Error
				}
				res.Candidates = append(res.Candidates, candidate)
				continue
			}
		} else {
			candidate.TestPassed = (exitCode == 0)
		}

		// Se passou no teste, reseta falhas no Circuit Breaker
		e.breaker.RecordSuccess()
		survivors = append(survivors, candidate)
		res.Candidates = append(res.Candidates, candidate)
	}

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

	// Escolhe o melhor candidato: critério de menor ruído (menor total de linhas alteradas com testes verdes)
	best := survivors[0]
	minChanges := best.LinesAdded + best.LinesDeleted

	for i := 1; i < len(survivors); i++ {
		changes := survivors[i].LinesAdded + survivors[i].LinesDeleted
		if changes < minChanges && survivors[i].TestPassed {
			best = survivors[i]
			minChanges = changes
		}
	}

	res.Winner = &best

	// Executa o merge se AutoMerge estiver ativo ou em modo interativo
	if e.cfg.AutoMerge && !e.cfg.DryRun {
		mergeCmd := exec.CommandContext(ctx, "git", "merge", "--no-ff", best.Branch, "-m", fmt.Sprintf("swarm(pipeline): integrando solucao vencedora de %s (%s)", best.Role.Name, best.AccountID))
		mergeCmd.Dir = e.cfg.RepoRoot
		if err := mergeCmd.Run(); err != nil {
			// Tenta rebase limpo se houver divergência
			_ = exec.Command("git", "merge", "--abort").Run()
			res.Error = fmt.Errorf("falha ao integrar branch vencedor %s: %w", best.Branch, err)
			return res, res.Error
		}

		// Valida se o test-cmd continua passando no branch principal após o merge
		if e.cfg.TestCmd != "" {
			_, tExit, _ := e.dispatcher.Runner.RunCommand(ctx, e.cfg.RepoRoot, e.cfg.TestCmd, 2*time.Minute)
			if tExit != 0 {
				res.Error = fmt.Errorf("Quality Gate falhou no branch principal após merge do vencedor %s", best.Role.Name)
				return res, res.Error
			}
		}
	}

	res.Success = true
	res.Duration = time.Since(start)
	res.ArtifactMsg = fmt.Sprintf("Solução vencedora de %s (%s) selecionada e auditada.", best.Role.Name, best.AccountID)

	_ = e.memClient.SendMessage("swarm-lead", "default", fmt.Sprintf("Ciclo concluído! Vencedor: %s. Linhas: +%d/-%d.", best.Role.Name, best.LinesAdded, best.LinesDeleted))

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
	for _, r := range e.cfg.Roles {
		if r.Type != config.RoleLead && r.Type != config.RoleArch && r.Type != config.RoleQA && r.Type != config.RoleSec {
			workers = append(workers, r)
		}
	}
	return workers
}

func (e *PipelineEngine) getGitDiff(dir, from, to string) (string, error) {
	args := []string{"diff"}
	if from != "" && to != "" {
		args = append(args, fmt.Sprintf("%s...%s", from, to))
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
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
