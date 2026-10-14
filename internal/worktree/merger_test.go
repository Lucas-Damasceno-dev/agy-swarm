package worktree

import (
	"testing"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
)

func TestPipelineOrder(t *testing.T) {
	if len(PipelineOrder) != 4 {
		t.Fatalf("esperado 4 papéis no pipeline de merge, obtido: %d", len(PipelineOrder))
	}
	if PipelineOrder[0] != config.RoleArch {
		t.Errorf("primeiro merge deve ser arch, obtido: %s", PipelineOrder[0])
	}
	if PipelineOrder[1] != config.RoleHacker {
		t.Errorf("segundo merge deve ser hacker, obtido: %s", PipelineOrder[1])
	}
	if PipelineOrder[2] != config.RoleQA {
		t.Errorf("terceiro merge deve ser qa, obtido: %s", PipelineOrder[2])
	}
	if PipelineOrder[3] != config.RoleSec {
		t.Errorf("quarto merge deve ser sec, obtido: %s", PipelineOrder[3])
	}
}
