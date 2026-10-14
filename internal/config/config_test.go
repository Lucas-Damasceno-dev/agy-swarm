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
