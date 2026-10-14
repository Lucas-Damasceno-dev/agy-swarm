package pipeline

import (
	"context"
	"fmt"
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
			res.Error = fmt.Errorf("contrato inválido: stubs do Estágio 1 não compilam:\n%s", buildOut)
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

	// Estágio 2 cria worktree ramificada a partir do branch do Estágio 1 (swarm/arch),
	// garantindo que os contratos já existem fisicamente no disco para o QA compilar
	wtDir, err := e.wtMgr.ProvisionRoleWorktreeFromBase(role, "swarm/arch")
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
	}

	// Condição B: Execução dos testes DEVE FALHAR neste momento contra os stubs vazios
	testOutput := ""
	if e.cfg.TestCmd != "" {
		out, tCode, _ := e.dispatcher.Runner.RunCommand(ctx, wtDir, e.cfg.TestCmd, 2*time.Minute)
		testOutput = out
		if tCode == 0 {
			res.Error = fmt.Errorf("testes inválidos: testes passaram imediatamente sem nenhuma implementação (tautologia vazia)")
			return res, "", "", res.Error
		}
	}

	diff, _ := e.getGitDiff(wtDir, "swarm/arch", "HEAD")
	if diff == "" {
		diff, _ = e.getGitDiff(wtDir, "swarm/arch", "")
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

	// Execução concorrente real de todos os workers competindo em worktrees isoladas
	for _, w := range workers {
		wg.Add(1)
		go func(worker config.RoleConfig) {
			defer wg.Done()
			wStart := time.Now()

			// Cada worker parte de swarm/qa, contendo os contratos e testes já no disco!
			wtDir, err := e.wtMgr.ProvisionRoleWorktreeFromBase(worker, "swarm/qa")
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
				Branch:      fmt.Sprintf("swarm/%s", worker.Type),
				ExitCode:    exitCode,
				Duration:    time.Since(wStart),
				Error:       err,
			}

			// Blindagem contra Reward Hacking: Restaura a imutabilidade dos testes do Estágio 2
			_ = exec.Command("git", "-C", wtDir, "checkout", "swarm/qa", "--", "*_test.go").Run()
			_ = exec.Command("git", "-C", wtDir, "checkout", "swarm/qa", "--", "**/*_test.go").Run()

			// Filtro Frio: Executa o test-cmd na worktree do candidato
			if e.cfg.TestCmd != "" {
				testOut, tExit, _ := e.dispatcher.Runner.RunCommand(ctx, wtDir, e.cfg.TestCmd, 2*time.Minute)
				candidate.TestOutput = testOut
				candidate.TestPassed = (tExit == 0)

				diff, _ := e.getGitDiff(wtDir, "swarm/qa", "")
				candidate.Diff = diff
				candidate.LinesAdded, candidate.LinesDeleted = countDiffLines(diff)

				// Loop de Auto-Cura (Self-Healing) isolado com disjuntor local
				maxSelfHeal := 2
				for healAttempt := 1; !candidate.TestPassed && healAttempt <= maxSelfHeal; healAttempt++ {
					opened, _ := localBreaker.RecordFailure(diff, testOut)
					if opened {
						break
					}

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

					// Restaura testes para garantir imutabilidade pós-reparo
					_ = exec.Command("git", "-C", wtDir, "checkout", "swarm/qa", "--", "*_test.go").Run()
					_ = exec.Command("git", "-C", wtDir, "checkout", "swarm/qa", "--", "**/*_test.go").Run()

					// Reavalia Filtro Frio
					testOut, tExit, _ = e.dispatcher.Runner.RunCommand(ctx, wtDir, e.cfg.TestCmd, 2*time.Minute)
					candidate.TestOutput = testOut
					candidate.TestPassed = (tExit == 0)
					diff, _ = e.getGitDiff(wtDir, "swarm/qa", "")
					candidate.Diff = diff
					candidate.LinesAdded, candidate.LinesDeleted = countDiffLines(diff)
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

	// Critério de desempate determinístico: menor ruído de linhas alteradas com bônus de velocidade
	calcScore := func(c CandidateResult) float64 {
		lines := float64(c.LinesAdded + c.LinesDeleted)
		durSec := c.Duration.Seconds()
		return lines + (durSec * 0.1) // linhas de diff dominam; duração desempata
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

	// Executa o merge se AutoMerge estiver ativo ou em modo interativo
	if e.cfg.AutoMerge && !e.cfg.DryRun {
		mergeCmd := exec.CommandContext(ctx, "git", "-C", e.cfg.RepoRoot, "merge", "--no-ff", best.Branch, "-m", fmt.Sprintf("swarm(pipeline): integrando solucao vencedora de %s (%s)", best.Role.Name, best.AccountID))
		if err := mergeCmd.Run(); err != nil {
			// Tenta abortar o merge garantindo diretório raiz
			_ = exec.Command("git", "-C", e.cfg.RepoRoot, "merge", "--abort").Run()
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
