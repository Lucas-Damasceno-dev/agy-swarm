package config

import (
	"strings"
	"testing"
)

func TestParseRolesDefault(t *testing.T) {
	roles, err := ParseRoles("", 0)
	if err != nil {
		t.Fatalf("esperado sem erro, obtido: %v", err)
	}
	if len(roles) != 5 {
		t.Fatalf("esperado 5 papéis default, obtido: %d", len(roles))
	}
	if roles[0].Type != RoleLead {
		t.Errorf("primeiro papel deve ser lead, obtido: %s", roles[0].Type)
	}
}

func TestParseRolesCustom(t *testing.T) {
	roles, err := ParseRoles("arch,qa", 0)
	if err != nil {
		t.Fatalf("esperado sem erro, obtido: %v", err)
	}
	// Deve incluir Maestro (Lead) + Arch + QA
	if len(roles) != 3 {
		t.Fatalf("esperado 3 papéis, obtido: %d", len(roles))
	}
	if roles[1].Type != RoleArch || roles[2].Type != RoleQA {
		t.Errorf("papéis inesperados: %+v", roles)
	}
}

func TestParseRolesCount(t *testing.T) {
	roles, err := ParseRoles("", 3)
	if err != nil {
		t.Fatalf("esperado sem erro, obtido: %v", err)
	}
	if len(roles) != 3 {
		t.Fatalf("esperado 3 papéis, obtido: %d", len(roles))
	}
}

func TestParseRolesTenAgents(t *testing.T) {
	roles, err := ParseRoles("", 10)
	if err != nil {
		t.Fatalf("esperado sem erro para 10 agentes, obtido: %v", err)
	}
	if len(roles) != 10 {
		t.Fatalf("esperado 10 papéis, obtido: %d", len(roles))
	}
	if roles[9].Type != RoleAdversary {
		t.Errorf("décimo papel esperado adversary, obtido: %s", roles[9].Type)
	}
}


func TestParseRolesInvalid(t *testing.T) {
	_, err := ParseRoles("invalido", 0)
	if err == nil {
		t.Errorf("esperado erro para papel desconhecido")
	}
}

func TestGeneratePersonaRule(t *testing.T) {
	all := DefaultRoles()
	rule := GeneratePersonaRule(all[1], all) // Arch
	if !strings.Contains(rule, "Arquiteto") {
		t.Errorf("regra deve conter nome da persona Arquiteto")
	}
	if !strings.Contains(rule, "swarm-arch") {
		t.Errorf("regra deve conter identificador de projeto swarm-arch")
	}
	if !strings.Contains(rule, "ai-memory") {
		t.Errorf("regra deve conter instruções do ai-memory")
	}
}

func TestMaestroPromptsAntiCodingGuard(t *testing.T) {
	roles := DefaultRoles()

	// Prompt interativo
	inter := GenerateInteractiveMaestroPrompt(roles)
	if !strings.Contains(inter, "NUNCA EDITA OU CRIA CÓDIGO FONTE DIRETAMENTE") {
		t.Errorf("prompt interativo deve proibir expressamente edição direta de código")
	}
	if !strings.Contains(inter, "agy-swarm dispatch") {
		t.Errorf("prompt interativo deve instruir o Maestro a usar 'agy-swarm dispatch'")
	}

	if !strings.Contains(inter, "KANBAN SWARM") {
		t.Errorf("prompt interativo deve conter instruções de Kanban Swarm")
	}
	if !strings.Contains(inter, "agy-swarm kanban") {
		t.Errorf("prompt interativo deve referenciar comandos 'agy-swarm kanban'")
	}

	// Prompt inicial com tarefa
	initP := GenerateInitialMaestroPrompt(roles, "Minha tarefa de teste")
	if !strings.Contains(initP, "NUNCA EDITA OU CRIA CÓDIGO FONTE DIRETAMENTE") {
		t.Errorf("prompt inicial deve proibir expressamente edição direta de código")
	}
	if !strings.Contains(initP, "Minha tarefa de teste") {
		t.Errorf("prompt inicial deve conter a tarefa do usuário")
	}
	if !strings.Contains(initP, "agy-swarm kanban") {
		t.Errorf("prompt inicial deve referenciar comandos 'agy-swarm kanban'")
	}
}

