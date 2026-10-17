package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/agyhub"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/kanban"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/lease"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/notify"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/pipeline"
)

// AuditIssue representa uma proposta de melhoria emitida por um especialista na Fase Map.
type AuditIssue struct {
	Title    string   `json:"title"`
	Scope    []string `json:"scope"`
	Spec     string   `json:"spec"`
	Severity string   `json:"severity"` // HIGH, MED, LOW
	Role     string   `json:"role,omitempty"`
}

// handleAutopilot orquestra o ciclo completo de autonomia: Map -> Reduce -> Execute -> Retrospectiva.
func handleAutopilot() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao obter diretório de trabalho: %v\n", err)
		os.Exit(1)
	}

	fs := flag.NewFlagSet("autopilot", flag.ExitOnError)
	pMaxWaves := fs.Int("max-waves", 5, "Número máximo de ciclos/ondas a executar")
	pMaxParallel := fs.Int("max-parallel", 3, "Número máximo de cartões em paralelo por onda")
	pAutoMerge := fs.Bool("auto-merge", true, "Executa integração automática pós-rebase e teste")
	pTestCmd := fs.String("test-cmd", "", "Comando de teste para Quality Gate (detectado se vazio)")
	pSeverity := fs.String("severity", "MED", "Corte mínimo de severidade para novos cartões (HIGH, MED, LOW)")

	args := os.Args[1:]
	if len(args) > 0 && args[0] == "autopilot" {
		args = args[1:]
	}
	_ = fs.Parse(args)

	testCmd := *pTestCmd
	if testCmd == "" {
		testCmd = detectDefaultTestCmd(cwd)
	}

	boardPath := kanban.DefaultBoardPath(cwd)
	lastHead := getGitHead(cwd)
	sterileCycles := 0

	fmt.Println("⚡ AGY-SWARM // MODO AUTOPILOT (DESCOBERTA FEDERADA & ONDAS ADVERSARIAIS)")
	fmt.Printf("• Diretório: %s\n", cwd)
	fmt.Printf("• Quality Gate: %s\n", testCmd)
	fmt.Printf("• Teto de Ciclos: %d ondas (max-parallel: %d)\n", *pMaxWaves, *pMaxParallel)
	fmt.Println(strings.Repeat("─", 68))

	for waveIdx := 1; waveIdx <= *pMaxWaves; waveIdx++ {
		fmt.Printf("\n🚀 AUTOPILOT // CICLO %d/%d\n", waveIdx, *pMaxWaves)
		fmt.Println(strings.Repeat("─", 68))

		b, err := kanban.LoadBoard(boardPath)
		hasEligible := false
		if err == nil {
			eligible := b.NextEligibleWave()
			if len(eligible) > 0 {
				hasEligible = true
			}
		}

		if !hasEligible {
			fmt.Println("🔍 FASE 1: MAP // Despachando auditoria silenciosa com 5 especialistas...")
			issues, err := runAuditMap(cwd, testCmd)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Aviso na auditoria: %v\n", err)
			}

			if len(issues) == 0 {
				fmt.Println("✨ FASE 2: REDUCE // Nenhum problema ou débito detectado pelos 5 auditores.")
				fmt.Println("🏁 Autopilot concluído com sucesso: repositório íntegro e suíte de testes aprovada.")
				return
			}

			fmt.Printf("📋 FASE 2: REDUCE & VETO // Maestro consolidando %d apontamento(s) com corte de severidade %s...\n", len(issues), *pSeverity)
			newCards := consolidateIssuesToCards(issues, b, *pSeverity, testCmd)
			if len(newCards) == 0 {
				fmt.Println("ℹ️ Todas as sugestões foram descartadas ou vetadas pelo Maestro (abaixo do corte ou cosméticas).")
				fmt.Println("🏁 Autopilot concluído com sucesso.")
				return
			}

			if b == nil {
				b = kanban.NewBoard("Épico Autônomo Autopilot")
			}
			for _, c := range newCards {
				if err := b.AddCard(c); err != nil {
					fmt.Fprintf(os.Stderr, "Aviso ao adicionar cartão %s: %v\n", c.ID, err)
				}
			}
			if err := b.Save(boardPath); err != nil {
				fmt.Fprintf(os.Stderr, "Erro ao persistir kanban board: %v\n", err)
				return
			}
			fmt.Printf("✅ %d novo(s) cartão(ões) gerado(s) com escopos ortogonais e adicionados ao Kanban.\n", len(newCards))
		}

		// FASE 3: EXECUTE
		fmt.Println("\n⚡ FASE 3: EXECUTE // Disparando onda paralela adversarial...")
		completed, blocked, err := executeKanbanWave(cwd, *pMaxParallel, *pAutoMerge)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao executar onda: %v\n", err)
		}

		// FASE 4: Retrospectiva & Circuit Breaker
		newHead := getGitHead(cwd)
		if newHead == lastHead {
			sterileCycles++
			if sterileCycles >= 2 {
				fmt.Println("🛑 Circuit Breaker: 2 ondas consecutivas sem alterações integradas no branch principal.")
				fmt.Println("Interrompendo autopilot para proteger cotas e evitar doom-loops.")
				notify.SendDesktop("AGY-SWARM Autopilot", "Circuit Breaker acionado: 2 ciclos sem alterações integradas.")
				return
			}
		} else {
			sterileCycles = 0
			lastHead = newHead
		}

		if blocked > 0 {
			fmt.Printf("⚠️ %d cartão(ões) bloqueado(s) nesta onda. Maestro analisando causas para a próxima rodada.\n", blocked)
		}

		if completed == 0 && blocked == 0 {
			fmt.Println("ℹ️ Nenhum cartão restante para execução.")
			notify.SendDesktop("AGY-SWARM Autopilot", "Épico concluído! Todos os cartões foram processados.")
			return
		}

		time.Sleep(1 * time.Second)
	}

	fmt.Printf("\n🏁 Teto de %d ciclos atingido no Autopilot.\n", *pMaxWaves)
	notify.SendDesktop("AGY-SWARM Autopilot", fmt.Sprintf("Autopilot finalizou após %d ciclos de ondas.", *pMaxWaves))
}

func runAuditMap(cwd string, testCmd string) ([]AuditIssue, error) {
	roles := []config.RoleType{
		config.RoleArch,
		config.RoleSec,
		config.RoleQA,
		config.RoleBench,
		config.RoleRefactor,
	}

	var allIssues []AuditIssue
	var mu sync.Mutex
	var wg sync.WaitGroup

	hubClient := agyhub.NewClient("")
	broker := lease.NewBroker(hubClient)
	runner := pipeline.NewOSProcessRunner()
	dispatcher := pipeline.NewEphemeralDispatcher(runner, broker)

	for _, rt := range roles {
		wg.Add(1)
		go func(roleType config.RoleType) {
			defer wg.Done()
			roleCfg := findRoleByType(roleType)

			prompt := fmt.Sprintf(`Você é o especialista %s (%s).
Analise os arquivos do repositório em modo estritamente LEITURA.
Identifique até 2 problemas reais ou melhorias estritamente necessárias sob seu viés:
- arch: acoplamento, contratos quebrados, violações de separação de camadas.
- sec: vulnerabilidades SAST, input sanitization, permissões indevidas, vazamento de segredos.
- qa: ausência de testes em funções críticas, regressões não cobertas.
- bench: gargalos de memória, concorrência insegura, vazamentos de goroutine/recursos.
- refactor: dead-code real, duplicação flagrante, complexidade ciclomática estéril.

RESPONDA EXCLUSIVAMENTE COM UM ARRAY JSON NO SEGUINTE FORMATO:
[
  {
    "title": "resumo de até 40 caracteres",
    "scope": ["pasta/ou/arquivo.go"],
    "spec": "descrição concisa do problema e do que implementar",
    "severity": "HIGH"
  }
]
Se não encontrar problemas graves, retorne apenas [].`, roleCfg.Name, roleCfg.Type)

			promptFile, err := pipeline.WritePromptFile(cwd, fmt.Sprintf("audit-%s", roleType), prompt)
			if err != nil {
				return
			}
			defer os.Remove(promptFile)

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()

			out, exitCode, activeLease, err := dispatcher.DispatchWithFailover(ctx, cwd, roleCfg, "0_AUDIT_MAP", promptFile, 3*time.Minute)
			if activeLease != nil {
				defer broker.Release(activeLease)
			}
			if err != nil || exitCode != 0 {
				return
			}

			issues := parseAuditJSON(out, string(roleType))
			if len(issues) > 0 {
				mu.Lock()
				allIssues = append(allIssues, issues...)
				mu.Unlock()
			}
		}(rt)
	}

	wg.Wait()
	return allIssues, nil
}

func parseAuditJSON(rawOutput, role string) []AuditIssue {
	jsonRegex := regexp.MustCompile(`(?s)\[\s*\{.*\}\s*\]`)
	match := jsonRegex.FindString(rawOutput)
	if match == "" {
		return nil
	}

	var issues []AuditIssue
	if err := json.Unmarshal([]byte(match), &issues); err != nil {
		return nil
	}

	for i := range issues {
		issues[i].Role = role
		issues[i].Title = strings.TrimSpace(issues[i].Title)
		issues[i].Spec = strings.TrimSpace(issues[i].Spec)
		issues[i].Severity = strings.ToUpper(strings.TrimSpace(issues[i].Severity))
		if issues[i].Severity == "" {
			issues[i].Severity = "MED"
		}
	}

	return issues
}

func consolidateIssuesToCards(issues []AuditIssue, b *kanban.Board, minSeverity string, defaultTestCmd string) []kanban.Card {
	var cards []kanban.Card
	seenTitles := make(map[string]bool)

	sevRank := map[string]int{"HIGH": 3, "MED": 2, "LOW": 1}
	minRank := sevRank[strings.ToUpper(minSeverity)]
	if minRank == 0 {
		minRank = 2 // MED default
	}

	existingCount := 0
	if b != nil {
		existingCount = len(b.Cards)
		for _, c := range b.Cards {
			seenTitles[strings.ToLower(c.Title)] = true
		}
	}

	for _, issue := range issues {
		rank := sevRank[strings.ToUpper(issue.Severity)]
		if rank < minRank {
			continue // Abaixo do corte
		}

		// Veto de segurança / arquiteto: descarta refatorações cosméticas
		if issue.Role == string(config.RoleRefactor) && rank < 3 {
			lowerSpec := strings.ToLower(issue.Spec)
			if strings.Contains(lowerSpec, "renomear") || strings.Contains(lowerSpec, "comentário") || strings.Contains(lowerSpec, "formatação") {
				continue // veto cosmético
			}
		}

		cleanTitle := strings.TrimSpace(issue.Title)
		if cleanTitle == "" || seenTitles[strings.ToLower(cleanTitle)] {
			continue // deduplica
		}
		seenTitles[strings.ToLower(cleanTitle)] = true

		existingCount++
		cardID := fmt.Sprintf("CARD-%02d", existingCount)

		scopes := issue.Scope
		if len(scopes) == 0 {
			scopes = []string{"*"}
		}

		cards = append(cards, kanban.Card{
			ID:        cardID,
			Title:     cleanTitle,
			Status:    kanban.StatusBacklog,
			Scope:     scopes,
			TestCmd:   defaultTestCmd,
			Spec:      fmt.Sprintf("[%s // %s] %s", strings.ToUpper(issue.Role), issue.Severity, issue.Spec),
			UpdatedAt: time.Now().UTC(),
		})
	}

	return cards
}

func findRoleByType(rt config.RoleType) config.RoleConfig {
	for _, r := range config.AllRoles() {
		if r.Type == rt {
			return r
		}
	}
	return config.RoleConfig{Type: rt, Name: string(rt)}
}

func getGitHead(cwd string) string {
	out, err := exec.Command("git", "-C", cwd, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
