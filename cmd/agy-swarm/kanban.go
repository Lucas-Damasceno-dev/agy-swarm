package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/agyhub"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/kanban"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/lease"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/pipeline"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/worktree"
)

func handleKanban() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao obter diretório de trabalho: %v\n", err)
		os.Exit(1)
	}

	boardPath := kanban.DefaultBoardPath(cwd)
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "kanban" {
		args = args[1:]
	}

	if len(args) == 0 {
		printKanbanUsage()
		return
	}

	subcmd := args[0]
	subArgs := args[1:]

	switch subcmd {
	case "init":
		fs := flag.NewFlagSet("kanban init", flag.ExitOnError)
		_ = fs.Parse(subArgs)
		epic := strings.TrimSpace(strings.Join(fs.Args(), " "))
		if epic == "" {
			epic = "Épico Autônomo"
		}
		b := kanban.NewBoard(epic)
		if err := b.Save(boardPath); err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao inicializar kanban em %s: %v\n", boardPath, err)
			os.Exit(1)
		}
		fmt.Printf("✅ Quadro Kanban inicializado em %s com o épico: %q\n", boardPath, epic)

	case "status":
		b, err := kanban.LoadBoard(boardPath)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Println("ℹ️ Nenhum quadro Kanban encontrado (.swarm/kanban.json). Inicialize com 'agy-swarm kanban init \"<épico>\"'")
				return
			}
			fmt.Fprintf(os.Stderr, "Erro ao ler quadro Kanban: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(b.FormatTable())

	case "add":
		fs := flag.NewFlagSet("kanban add", flag.ExitOnError)
		pID := fs.String("id", "", "Identificador único do cartão (ex: CARD-01)")
		pTitle := fs.String("title", "", "Título resumido do cartão")
		pDepends := fs.String("depends", "", "Dependências separadas por vírgula (ex: CARD-01,CARD-02)")
		pScope := fs.String("scope", "", "Pastas/módulos do escopo separados por vírgula (ex: apps/api,packages/core)")
		pTestCmd := fs.String("test-cmd", "", "Comando de teste específico do cartão")
		_ = fs.Parse(subArgs)

		spec := strings.TrimSpace(strings.Join(fs.Args(), " "))

		b, err := kanban.LoadBoard(boardPath)
		if err != nil {
			if os.IsNotExist(err) {
				b = kanban.NewBoard("Novo Épico")
			} else {
				fmt.Fprintf(os.Stderr, "Erro ao carregar quadro: %v\n", err)
				os.Exit(1)
			}
		}

		cardID := *pID
		if cardID == "" {
			cardID = fmt.Sprintf("CARD-%02d", len(b.Cards)+1)
		}

		title := *pTitle
		if title == "" {
			if len(spec) > 40 {
				title = spec[:37] + "..."
			} else {
				title = spec
			}
		}

		var deps []string
		if *pDepends != "" {
			for _, d := range strings.Split(*pDepends, ",") {
				d = strings.TrimSpace(d)
				if d != "" {
					deps = append(deps, d)
				}
			}
		}

		var scopes []string
		if *pScope != "" {
			for _, s := range strings.Split(*pScope, ",") {
				s = strings.TrimSpace(s)
				if s != "" {
					scopes = append(scopes, s)
				}
			}
		}

		card := kanban.Card{
			ID:        cardID,
			Title:     title,
			Status:    kanban.StatusBacklog,
			DependsOn: deps,
			Scope:     scopes,
			TestCmd:   *pTestCmd,
			Spec:      spec,
		}

		if err := b.AddCard(card); err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao adicionar cartão: %v\n", err)
			os.Exit(1)
		}

		if err := b.Save(boardPath); err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao salvar quadro: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("✅ Cartão %s (%s) adicionado ao Backlog do Kanban.\n", cardID, title)

	case "next":
		fs := flag.NewFlagSet("kanban next", flag.ExitOnError)
		pJSON := fs.Bool("json", false, "Retorna saída em formato JSON estruturado")
		_ = fs.Parse(subArgs)

		b, err := kanban.LoadBoard(boardPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao carregar quadro: %v\n", err)
			os.Exit(1)
		}

		next := b.NextEligible()
		if next == nil {
			if *pJSON {
				fmt.Println("{}")
			} else {
				fmt.Println("ℹ️ Nenhum cartão elegível no momento (todos concluídos ou bloqueados por dependências).")
			}
			return
		}

		if *pJSON {
			data, _ := json.MarshalIndent(next, "", "  ")
			fmt.Println(string(data))
		} else {
			fmt.Printf("🎯 PRÓXIMO CARTÃO DESIMPEDIDO: %s // %s\n", next.ID, next.Title)
			if len(next.DependsOn) > 0 {
				fmt.Printf("  • Dependências atendidas: %s\n", strings.Join(next.DependsOn, ", "))
			}
			if len(next.Scope) > 0 {
				fmt.Printf("  • Escopo/Módulos: %s\n", strings.Join(next.Scope, ", "))
			}
			if next.TestCmd != "" {
				fmt.Printf("  • Quality Gate: %s\n", next.TestCmd)
			}
			fmt.Printf("  • Spec: %s\n", next.Spec)
		}

	case "set":
		fs := flag.NewFlagSet("kanban set", flag.ExitOnError)
		pID := fs.String("id", "", "ID do cartão a atualizar (ex: CARD-01)")
		pStatus := fs.String("status", "", "Novo status (BACKLOG, IN_PROGRESS, DONE, BLOCKED)")
		pWinner := fs.String("winner", "", "Identificador do vencedor/branch integrado")
		pError := fs.String("error", "", "Mensagem de erro de falha ou bloqueio")
		_ = fs.Parse(subArgs)

		if *pID == "" || *pStatus == "" {
			fmt.Fprintln(os.Stderr, "Erro: flags -id e -status são obrigatórias.")
			os.Exit(1)
		}

		b, err := kanban.LoadBoard(boardPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao carregar quadro: %v\n", err)
			os.Exit(1)
		}

		status := kanban.CardStatus(strings.ToUpper(*pStatus))
		if err := b.UpdateCardStatus(*pID, status, *pWinner, *pError); err != nil {
			fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
			os.Exit(1)
		}

		if err := b.Save(boardPath); err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao salvar quadro: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("✅ Cartão %s atualizado para status %s.\n", *pID, status)

	case "step":
		// Pega o próximo cartão elegível, marca como IN_PROGRESS e executa o pipeline determinístico de forma síncrona
		b, err := kanban.LoadBoard(boardPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao carregar quadro: %v\n", err)
			os.Exit(1)
		}

		next := b.NextEligible()
		if next == nil {
			fmt.Println("ℹ️ Nenhum cartão elegível para execução no momento.")
			return
		}

		_ = b.UpdateCardStatus(next.ID, kanban.StatusInProgress, "", "")
		_ = b.Save(boardPath)

		taskPayload := fmt.Sprintf("[%s] %s: %s", next.ID, next.Title, next.Spec)
		fmt.Printf("⚡ Executando cartão %s (%s) via Pipeline de Verificação Adversarial...\n", next.ID, next.Title)

		// Executa o pipeline de forma síncrona
		var pipelineArgs []string
		pipelineArgs = append(pipelineArgs, "pipeline", "-auto-merge")
		if next.TestCmd != "" {
			pipelineArgs = append(pipelineArgs, "-test-cmd", next.TestCmd)
		}
		pipelineArgs = append(pipelineArgs, taskPayload)

		cmd := exec.Command(os.Args[0], pipelineArgs...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			errMsg := fmt.Sprintf("pipeline falhou: %v", err)
			_ = b.UpdateCardStatus(next.ID, kanban.StatusBlocked, "", errMsg)
			_ = b.Save(boardPath)
			fmt.Fprintf(os.Stderr, "❌ Cartão %s bloqueado: %s\n", next.ID, errMsg)
			os.Exit(1)
		}

		_ = b.UpdateCardStatus(next.ID, kanban.StatusDone, "pipeline", "")
		_ = b.Save(boardPath)
		fmt.Printf("✅ [%s] %s concluído e integrado com sucesso!\n", next.ID, next.Title)

	case "wave":
		fs := flag.NewFlagSet("kanban wave", flag.ExitOnError)
		pMaxParallel := fs.Int("max-parallel", 3, "Número máximo de cartões paralelos na onda")
		pAutoMerge := fs.Bool("auto-merge", true, "Executa fila de integração sequencial (Refinery) ao término da onda")
		pInline := fs.Bool("inline", false, "Executa inline no terminal atual sem despachar para o viewport")
		_ = fs.Parse(subArgs)

		if !*pInline && os.Getenv("TMUX") != "" {
			session := "agy-swarm"
			runtimeDir, _ := ensureRuntimeDir()
			vpFile := filepath.Join(runtimeDir, fmt.Sprintf("%s-viewport.pane", session))
			if vpData, err := os.ReadFile(vpFile); err == nil && len(strings.TrimSpace(string(vpData))) > 0 {
				vpPaneID := strings.TrimSpace(string(vpData))
				curPaneOut, err := exec.Command("tmux", "display-message", "-p", "#{pane_id}").Output()
				if err == nil {
					curPaneID := strings.TrimSpace(string(curPaneOut))
					if curPaneID != "" && curPaneID != vpPaneID {
						cmdToSend := fmt.Sprintf("agy-swarm kanban wave -inline -max-parallel %d", *pMaxParallel)
						if !*pAutoMerge {
							cmdToSend += " -auto-merge=false"
						}
						_ = exec.Command("tmux", "send-keys", "-t", vpPaneID, cmdToSend, "Enter").Run()
						fmt.Printf("⚡ Onda Kanban despachada para o Viewport Dinâmico (%s).\n", vpPaneID)
						fmt.Println("• Acompanhe a execução ao vivo no painel inferior direito ou janela 'wave' (Alt+w).")
						return
					}
				}
			}
		}

		if _, _, err := executeKanbanWave(cwd, *pMaxParallel, *pAutoMerge); err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao executar onda kanban: %v\n", err)
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "Subcomando kanban desconhecido: %s\n", subcmd)
		printKanbanUsage()
		os.Exit(1)
	}
}

func setupTmuxWaveWindow(cwd string, wave []kanban.Card, logsDir string) {
	if os.Getenv("TMUX") == "" {
		return
	}
	session := "agy-swarm"
	// Remove janela 'wave' de ondas anteriores para evitar empilhamento de panes e tail -f presos
	_ = exec.Command("tmux", "kill-window", "-t", fmt.Sprintf("%s:wave", session)).Run()
	// Cria janela em background para não roubar o foco do usuário (sem sequestro de foco)
	_ = exec.Command("tmux", "new-window", "-d", "-t", session, "-n", "wave", "-c", cwd).Run()

	for i, c := range wave {
		logPath := filepath.Join(logsDir, fmt.Sprintf("%s.log", c.ID))
		_ = os.WriteFile(logPath, []byte(fmt.Sprintf("Aguardando inicialização do cartão %s (%s)...\n", c.ID, c.Title)), 0644)

		if i > 0 {
			_ = exec.Command("tmux", "split-window", "-t", fmt.Sprintf("%s:wave", session), "-c", cwd).Run()
		}
		// Roda tail -f no log de streaming
		paneTarget := fmt.Sprintf("%s:wave.%d", session, i)
		_ = exec.Command("tmux", "send-keys", "-t", paneTarget, fmt.Sprintf("tail -f %q", logPath), "Enter").Run()
		_ = exec.Command("tmux", "select-pane", "-t", paneTarget, "-T", fmt.Sprintf("[%s] %s", c.ID, c.Title)).Run()
	}

	_ = exec.Command("tmux", "select-layout", "-t", fmt.Sprintf("%s:wave", session), "tiled").Run()
	fmt.Printf("📺 Visualização da onda configurada na janela Tmux 'wave' (%d panes tiled). Acesse com 'Ctrl+b n'.\n", len(wave))
}

func printKanbanUsage() {
	fmt.Fprintf(os.Stderr, `⚡ AGY-SWARM // KANBAN SWARM MANAGER
Uso:
  agy-swarm kanban init "<título do épico>"
  agy-swarm kanban status
  agy-swarm kanban next [--json]
  agy-swarm kanban add -id <ID> -title "<título>" [-depends "ID1,ID2"] [-scope "api/,core/"] [-test-cmd "<cmd>"] "<spec>"
  agy-swarm kanban set -id <ID> -status <BACKLOG|IN_PROGRESS|DONE|BLOCKED> [-winner "..."] [-error "..."]
  agy-swarm kanban step
  agy-swarm kanban wave [-max-parallel 3] [--auto-merge] [-inline]
`)
}

// executeKanbanWave executa uma onda de cartões elegíveis respeitando cotas, Red-Team TDD e isolamento de escopo.
func executeKanbanWave(cwd string, maxParallel int, autoMerge bool) (completed int, blocked int, err error) {
	boardPath := kanban.DefaultBoardPath(cwd)
	b, err := kanban.LoadBoard(boardPath)
	if err != nil {
		return 0, 0, fmt.Errorf("erro ao carregar quadro: %w", err)
	}

	wave := b.NextEligibleWave()
	if len(wave) == 0 {
		fmt.Println("ℹ️ Nenhum cartão elegível para a onda atual (todos concluídos ou bloqueados por dependências/escopo).")
		return 0, 0, nil
	}

	if maxParallel <= 0 {
		maxParallel = 3
	}
	if len(wave) > maxParallel {
		wave = wave[:maxParallel]
	}

	fmt.Println("⚡ AGY-SWARM // DISPARANDO ONDA PARALELA DE CARTÕES")
	fmt.Printf("• Cartões elegíveis nesta onda (%d): ", len(wave))
	var cardIDs []string
	for _, c := range wave {
		cardIDs = append(cardIDs, c.ID)
	}
	fmt.Println(strings.Join(cardIDs, ", "))
	fmt.Println(strings.Repeat("─", 68))

	// Atualiza os cartões para IN_PROGRESS
	for _, c := range wave {
		_ = b.UpdateCardStatus(c.ID, kanban.StatusInProgress, "", "")
	}
	_ = b.Save(boardPath)

	// Cria diretório de logs para streaming visual
	logsDir := filepath.Join(cwd, ".worktrees", "logs")
	_ = os.MkdirAll(logsDir, 0755)

	// Se tmux estiver ativo, cria janela visual 'wave' em background sem sequestro de foco
	setupTmuxWaveWindow(cwd, wave, logsDir)

	// Executa os pipelines em paralelo preservando o lease.Broker, Red-Team TDD e Failover 429
	hubClient := agyhub.NewClient("")
	broker := lease.NewBroker(hubClient)
	roles := config.AllRoles()

	type waveCardResult struct {
		card   kanban.Card
		winner *pipeline.CandidateResult
		err    error
	}

	resultsChan := make(chan waveCardResult, len(wave))
	var wg sync.WaitGroup

	for _, card := range wave {
		wg.Add(1)
		go func(c kanban.Card) {
			defer wg.Done()

			logFilePath := filepath.Join(logsDir, fmt.Sprintf("%s.log", c.ID))
			logFile, _ := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
			if logFile != nil {
				defer logFile.Close()
				_, _ = fmt.Fprintf(logFile, "=== AGY-SWARM PIPELINE // CARTÃO %s: %s ===\n\n", c.ID, c.Title)
			}

			testCmd := c.TestCmd
			if testCmd == "" {
				testCmd = detectDefaultTestCmd(cwd)
			}

			engineCfg := pipeline.EngineConfig{
				RepoRoot:  cwd,
				Task:      fmt.Sprintf("[%s] %s: %s", c.ID, c.Title, c.Spec),
				TestCmd:   testCmd,
				AutoMerge: false, // O merge é orquestrado sequencialmente na Refinery!
				Roles:     roles,
				CardID:    c.ID,
				LogWriter: logFile,
			}

			engine := pipeline.NewPipelineEngine(engineCfg, broker, nil)
			stageResults, execErr := engine.Execute(context.Background())

			var winner *pipeline.CandidateResult
			for _, sr := range stageResults {
				if sr.Winner != nil {
					winner = sr.Winner
				}
			}

			resultsChan <- waveCardResult{
				card:   c,
				winner: winner,
				err:    execErr,
			}
		}(card)
	}

	wg.Wait()
	close(resultsChan)

	// Fila de Integração Sequencial (The Refinery):
	fmt.Println("\n🔄 AGY-SWARM // REFINERY: INTEGRAÇÃO SEQUENCIAL DA ONDA")
	fmt.Println(strings.Repeat("─", 68))

	for res := range resultsChan {
		c := res.card
		if res.err != nil || res.winner == nil {
			errMsg := "pipeline falhou ou nenhum worker passou no Quality Gate"
			if res.err != nil {
				errMsg = res.err.Error()
			}
			_ = b.UpdateCardStatus(c.ID, kanban.StatusBlocked, "", errMsg)
			fmt.Printf("❌ [%s] %s: %s\n", c.ID, c.Title, errMsg)
			blocked++
			continue
		}

		winner := res.winner
		fmt.Printf("🏆 [%s] Vencedor selecionado: %s (%s) · Branch: %s\n", c.ID, winner.Role.Name, winner.AccountID, winner.Branch)

		if autoMerge {
			targetBranchOut, _ := exec.Command("git", "-C", cwd, "rev-parse", "--abbrev-ref", "HEAD").Output()
			targetBranch := strings.TrimSpace(string(targetBranchOut))
			if targetBranch == "" || targetBranch == "HEAD" {
				targetBranch = "main"
			}

			merger := worktree.NewMerger(cwd)

			// Rebase determinístico dentro da worktree ativa do vencedor sobre o branch alvo para absorver merges anteriores da mesma onda
			if err := merger.RebaseInWorktree(winner.WorktreeDir, targetBranch); err != nil {
				errMsg := fmt.Sprintf("conflito de rebase contra %s no branch %s: %v", targetBranch, winner.Branch, err)
				_ = b.UpdateCardStatus(c.ID, kanban.StatusBlocked, "", errMsg)
				fmt.Printf("⚠️ [%s] %s\n", c.ID, errMsg)
				blocked++
				continue
			}

			// Quality Gate pós-rebase antes do merge (compatível com comandos compostos via sh -c)
			testCmd := c.TestCmd
			if testCmd == "" {
				testCmd = detectDefaultTestCmd(cwd)
			}
			if testCmd != "" {
				testRun := exec.Command("sh", "-c", testCmd)
				testRun.Dir = winner.WorktreeDir
				if out, err := testRun.CombinedOutput(); err != nil {
					errMsg := fmt.Sprintf("Quality Gate falhou pós-rebase com comando: %s\n%s", testCmd, strings.TrimSpace(string(out)))
					_ = b.UpdateCardStatus(c.ID, kanban.StatusBlocked, "", errMsg)
					fmt.Printf("⚠️ [%s] %s\n", c.ID, errMsg)
					blocked++
					continue
				}
			}

			// Merge --no-ff seguro no branch alvo usando Merger
			mergeMsg := fmt.Sprintf("swarm(wave): integrando solucao de %s para %s (%s)", winner.Role.Name, c.ID, winner.AccountID)
			if err := merger.MergeBranch(context.Background(), winner.Branch, mergeMsg, ""); err != nil {
				errMsg := fmt.Sprintf("falha no merge: %v", err)
				_ = b.UpdateCardStatus(c.ID, kanban.StatusBlocked, "", errMsg)
				fmt.Printf("❌ [%s] %s\n", c.ID, errMsg)
				blocked++
				continue
			}

			// Expurga worktrees e branches descartadas deste cartão/namespace da onda
			cardWtMgr := worktree.NewNamespacedManager(cwd, roles, c.ID)
			_ = cardWtMgr.SafeCleanup(true)

			_ = b.UpdateCardStatus(c.ID, kanban.StatusDone, fmt.Sprintf("%s (%s)", winner.Role.Name, winner.AccountID), "")
			fmt.Printf("✅ [%s] Integrado com sucesso no branch %s!\n", c.ID, targetBranch)
			completed++
		} else {
			_ = b.UpdateCardStatus(c.ID, kanban.StatusDone, winner.Role.Name, "")
			fmt.Printf("ℹ️ [%s] Pronto para merge manual. Branch: %s\n", c.ID, winner.Branch)
			completed++
		}
	}

	_ = b.Save(boardPath)
	fmt.Println("\n" + b.FormatTable())
	return completed, blocked, nil
}
