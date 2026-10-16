package kanban

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKanbanLifecycle(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "kanban-test-*")
	if err != nil {
		t.Fatalf("falha ao criar tempdir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	boardPath := filepath.Join(tempDir, ".swarm", "kanban.json")
	b := NewBoard("Implementar Sistema de Eventos Zenith")

	c1 := Card{
		ID:    "CARD-01",
		Title: "Definir structs de Eventos",
		Spec:  "Criar tipos Event e Dispatcher em internal/events/types.go",
	}
	c2 := Card{
		ID:        "CARD-02",
		Title:     "Implementar Publish/Subscribe",
		DependsOn: []string{"CARD-01"},
		Spec:      "Implementar canal assíncrono com buffer",
	}

	if err := b.AddCard(c1); err != nil {
		t.Fatalf("erro ao adicionar c1: %v", err)
	}
	if err := b.AddCard(c2); err != nil {
		t.Fatalf("erro ao adicionar c2: %v", err)
	}

	// Tentativa de duplicar ID
	if err := b.AddCard(Card{ID: "CARD-01", Title: "Duplicado"}); err == nil {
		t.Errorf("esperava erro ao adicionar ID duplicado")
	}

	// Salva e carrega
	if err := b.Save(boardPath); err != nil {
		t.Fatalf("erro ao salvar board: %v", err)
	}

	loaded, err := LoadBoard(boardPath)
	if err != nil {
		t.Fatalf("erro ao carregar board: %v", err)
	}
	if loaded.Epic != "Implementar Sistema de Eventos Zenith" {
		t.Errorf("épico incorreto: %q", loaded.Epic)
	}
	if len(loaded.Cards) != 2 {
		t.Fatalf("esperava 2 cartões, obteve %d", len(loaded.Cards))
	}

	// Resolução do DAG: Primeiro elegível deve ser CARD-01
	next := loaded.NextEligible()
	if next == nil || next.ID != "CARD-01" {
		t.Fatalf("esperava CARD-01 como próximo elegível, obteve %+v", next)
	}

	// Conclui CARD-01
	if err := loaded.UpdateCardStatus("CARD-01", StatusDone, "arch (account-02)", ""); err != nil {
		t.Fatalf("erro ao atualizar CARD-01: %v", err)
	}

	// Próximo elegível deve agora ser CARD-02
	next = loaded.NextEligible()
	if next == nil || next.ID != "CARD-02" {
		t.Fatalf("esperava CARD-02 como próximo elegível após CARD-01 DONE, obteve %+v", next)
	}

	// Falha CARD-02
	if err := loaded.UpdateCardStatus("CARD-02", StatusBlocked, "", "test suite compilation failed"); err != nil {
		t.Fatalf("erro ao bloquear CARD-02: %v", err)
	}

	// Agora nenhum cartão deve estar elegível
	if next = loaded.NextEligible(); next != nil {
		t.Fatalf("esperava nenhum elegível, obteve %+v", next)
	}

	table := loaded.FormatTable()
	if !strings.Contains(table, "CARD-01") || !strings.Contains(table, "BLOCKED") {
		t.Errorf("tabela deve conter cartões e status: %s", table)
	}
}

func TestKanbanWaveResolution(t *testing.T) {
	b := NewBoard("Épico com Múltiplas Ondas Paralelas")
	// Onda 1: CARD-01 e CARD-02 são independentes
	_ = b.AddCard(Card{ID: "CARD-01", Title: "DB Schema", Scope: []string{"db/"}})
	_ = b.AddCard(Card{ID: "CARD-02", Title: "Auth API", Scope: []string{"api/"}})
	// Onda 2: CARD-03 depende de CARD-01; CARD-04 depende de CARD-01 e CARD-02
	_ = b.AddCard(Card{ID: "CARD-03", Title: "User Repo", DependsOn: []string{"CARD-01"}})
	_ = b.AddCard(Card{ID: "CARD-04", Title: "E2E Flow", DependsOn: []string{"CARD-01", "CARD-02"}})

	// Primeira onda: deve retornar exatamente CARD-01 e CARD-02
	wave1 := b.NextEligibleWave()
	if len(wave1) != 2 {
		t.Fatalf("esperava 2 cartões na onda 1, obteve %d", len(wave1))
	}
	if wave1[0].ID != "CARD-01" || wave1[1].ID != "CARD-02" {
		t.Errorf("cartões incorretos na onda 1: %+v", wave1)
	}

	// Conclui CARD-01
	_ = b.UpdateCardStatus("CARD-01", StatusDone, "arch", "")

	// Agora apenas CARD-02 (ainda pendente) e CARD-03 (dependência CARD-01 satisfeita)
	wave2 := b.NextEligibleWave()
	if len(wave2) != 2 {
		t.Fatalf("esperava 2 cartões elegíveis, obteve %d", len(wave2))
	}
	ids := map[string]bool{wave2[0].ID: true, wave2[1].ID: true}
	if !ids["CARD-02"] || !ids["CARD-03"] {
		t.Errorf("esperava CARD-02 e CARD-03, obteve %+v", wave2)
	}

	// Conclui CARD-02 e CARD-03
	_ = b.UpdateCardStatus("CARD-02", StatusDone, "hacker", "")
	_ = b.UpdateCardStatus("CARD-03", StatusDone, "sec", "")

	// Agora CARD-04 deve ser a única na onda
	wave3 := b.NextEligibleWave()
	if len(wave3) != 1 || wave3[0].ID != "CARD-04" {
		t.Fatalf("esperava apenas CARD-04 na onda 3, obteve %+v", wave3)
	}
}

func TestDisjointScopeLockInWave(t *testing.T) {
	b := NewBoard("Scope Lock Test")
	// CARD-01 e CARD-02 tocam na mesma pasta sem dependência explícita
	_ = b.AddCard(Card{ID: "CARD-01", Title: "Lease Broker", Scope: []string{"internal/lease"}})
	_ = b.AddCard(Card{ID: "CARD-02", Title: "Lease Fix", Scope: []string{"internal/lease/broker.go"}})
	// CARD-03 toca em pasta ortogonal
	_ = b.AddCard(Card{ID: "CARD-03", Title: "Tmux Driver", Scope: []string{"internal/tmux"}})

	wave := b.NextEligibleWave()
	if len(wave) != 2 {
		t.Fatalf("esperava 2 cartões na onda (CARD-01 e CARD-03), obteve %d", len(wave))
	}
	if wave[0].ID != "CARD-01" || wave[1].ID != "CARD-03" {
		t.Errorf("cartões com escopo colidente entraram na mesma onda: %+v", wave)
	}
}
