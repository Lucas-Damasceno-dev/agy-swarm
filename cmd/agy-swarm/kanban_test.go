package main

import (
	"testing"
)

func TestParsePlanJSON_ObjectFormatWithMarkdownFences(t *testing.T) {
	raw := "Aqui está o plano arquitetural para o épico:\n```json\n" +
		`{
  "epic": "Sistema de Cache Distribuído",
  "cards": [
    {
      "id": "CARD-01",
      "title": "Interfaces de Armazenamento",
      "spec": "Definir interface Store com Get, Set e Delete",
      "scope": ["internal/cache"],
      "depends_on": []
    },
    {
      "id": "CARD-02",
      "title": "Implementação Redis",
      "spec": "Implementar Store usando Redis client",
      "scope": ["internal/cache/redis"],
      "depends_on": ["CARD-01"]
    }
  ]
}` + "\n```\nQualquer dúvida me avise."

	plan, err := parsePlanJSON(raw)
	if err != nil {
		t.Fatalf("erro inesperado ao parsear JSON de plano: %v", err)
	}
	if len(plan.Cards) != 2 {
		t.Fatalf("esperava 2 cartões, obteve %d", len(plan.Cards))
	}
	if plan.Cards[0].ID != "CARD-01" || plan.Cards[0].Title != "Interfaces de Armazenamento" {
		t.Errorf("dados incorretos no CARD-01: %+v", plan.Cards[0])
	}
	if len(plan.Cards[1].DependsOn) != 1 || plan.Cards[1].DependsOn[0] != "CARD-01" {
		t.Errorf("dependência incorreta no CARD-02: %+v", plan.Cards[1].DependsOn)
	}
}

func TestParsePlanJSON_ArrayFormat(t *testing.T) {
	raw := `Segue o array de cartões:
[
  {
    "id": "CARD-01",
    "title": "Setup Inicial",
    "spec": "Configurar migrations e schema",
    "scope": ["internal/db"]
  }
]
Fim.`

	plan, err := parsePlanJSON(raw)
	if err != nil {
		t.Fatalf("erro inesperado ao parsear array JSON: %v", err)
	}
	if len(plan.Cards) != 1 {
		t.Fatalf("esperava 1 cartão, obteve %d", len(plan.Cards))
	}
	if plan.Cards[0].ID != "CARD-01" {
		t.Errorf("ID incorreto: %s", plan.Cards[0].ID)
	}
}

func TestParsePlanJSON_InvalidOrEmpty(t *testing.T) {
	raw := "Não foi possível gerar um plano estruturado no momento."
	_, err := parsePlanJSON(raw)
	if err == nil {
		t.Fatal("esperava erro ao parsear texto sem JSON estruturado")
	}
}
