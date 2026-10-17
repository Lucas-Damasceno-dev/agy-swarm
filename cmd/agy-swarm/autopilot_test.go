package main

import (
	"testing"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/kanban"
)

func TestParseAuditJSON_WithSurroundingNoise(t *testing.T) {
	raw := `Analisando repositório...
Aqui está o resultado da auditoria:
[
  {
    "title": "Corrigir race condition no cache",
    "scope": ["internal/cache"],
    "spec": "Adicionar sync.RWMutex no acesso ao mapa",
    "severity": "high"
  }
]
Fim da execução.`

	issues := parseAuditJSON(raw, "bench")
	if len(issues) != 1 {
		t.Fatalf("esperava 1 issue extraída, obteve %d", len(issues))
	}
	if issues[0].Role != "bench" {
		t.Errorf("esperava role 'bench', obteve %q", issues[0].Role)
	}
	if issues[0].Severity != "HIGH" {
		t.Errorf("esperava severity normalizada 'HIGH', obteve %q", issues[0].Severity)
	}
}

func TestParseAuditJSON_MultipleJSONBlocks(t *testing.T) {
	raw := `Exemplo de schema que não deve corromper o parsing:
[
  { "title": "exemplo falso", "invalid_field": 123 }
]

Aqui está o relatório real:
` + "```json\n" + `[
  {
    "title": "Vazamento de descritores no daemon",
    "scope": ["internal/hub/daemon"],
    "spec": "Fechar conn de socket nos handlers de erro",
    "severity": "CRITICAL"
  }
]
` + "```\nFim do relatório."

	issues := parseAuditJSON(raw, "sec")
	if len(issues) != 1 {
		t.Fatalf("esperava 1 issue extraída do bloco markdown correto, obteve %d", len(issues))
	}
	if issues[0].Title != "Vazamento de descritores no daemon" {
		t.Errorf("título incorreto: %q", issues[0].Title)
	}
	if issues[0].Severity != "CRITICAL" {
		t.Errorf("severidade incorreta: %q", issues[0].Severity)
	}
}

func TestConsolidateIssuesToCards_VetoAndScopeFallback(t *testing.T) {
	board := kanban.NewBoard("Teste Autopilot")
	_ = board.AddCard(kanban.Card{
		ID:    "CARD-01",
		Title: "Tarefa Já Existente",
	})

	issues := []AuditIssue{
		{
			// Deve ser descartada: duplicada no board
			Title:    "tarefa já existente",
			Scope:    []string{"cmd/"},
			Spec:     "Spec duplicada",
			Severity: "HIGH",
			Role:     "arch",
		},
		{
			// Deve ser descartada: veto cosmético de refactor (MED + "renomear")
			Title:    "Melhorar nomes de variáveis",
			Scope:    []string{"internal/config"},
			Spec:     "Renomear variáveis locais para clareza",
			Severity: "MED",
			Role:     "refactor",
		},
		{
			// Deve ser descartada: severidade abaixo do corte MED
			Title:    "Pequeno ajuste",
			Scope:    []string{"internal/notify"},
			Spec:     "Detalhe menor",
			Severity: "LOW",
			Role:     "qa",
		},
		{
			// Deve ser aprovada e receber fallback de escopo ["*"]
			Title:    "Sanitizar argumentos de execução",
			Scope:    nil,
			Spec:     "Validar inputs antes de construir comando",
			Severity: "HIGH",
			Role:     "sec",
		},
	}

	cards := consolidateIssuesToCards(issues, board, "MED", "go test ./...")
	if len(cards) != 1 {
		t.Fatalf("esperava exatamente 1 cartão aprovado após filtros e vetos, obteve %d", len(cards))
	}

	c := cards[0]
	if c.ID != "CARD-02" {
		t.Errorf("esperava ID sequencial CARD-02, obteve %q", c.ID)
	}
	if len(c.Scope) != 1 || c.Scope[0] != "*" {
		t.Errorf("esperava fallback de escopo ['*'], obteve %+v", c.Scope)
	}
}
