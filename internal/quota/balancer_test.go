package quota

import (
	"testing"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/agyhub"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
)

func TestCalculateScore(t *testing.T) {
	// 50% weekly + 100% 5h => 100 * (0.5 + 0.5 * 0.5) = 100 * 0.75 = 75.0
	s := CalculateScore(50.0, 100.0)
	if s != 75.0 {
		t.Errorf("esperado 75.0, obtido: %f", s)
	}

	// Hard cutoff: 5h crítica de 8% com semanal de 100% deve retornar 0.0 (evita alocar conta à beira de 429)
	sCutoff := CalculateScore(100.0, 8.0)
	if sCutoff != 0.0 {
		t.Errorf("esperado 0.0 com hard cutoff de 5h (<10%%), obtido: %f", sCutoff)
	}

	// Hard cutoff: semanal crítica de 3% deve retornar 0.0
	sCutoffWeek := CalculateScore(3.0, 90.0)
	if sCutoffWeek != 0.0 {
		t.Errorf("esperado 0.0 com hard cutoff semanal (<5%%), obtido: %f", sCutoffWeek)
	}
}

func TestAssignRolesFallback(t *testing.T) {
	// Força socket fechado para validar fallback determinístico
	offlineClient := agyhub.NewClient("/tmp/socket-offline-para-teste.sock")
	balancer := NewBalancer(offlineClient)
	roles := config.DefaultRoles()

	reassigned, err := balancer.AssignRoles(roles)
	if err != nil {
		t.Fatalf("esperado sem erro no fallback, obtido: %v", err)
	}
	if len(reassigned) != len(roles) {
		t.Fatalf("quantidade de papéis alterada")
	}
	if reassigned[0].Profile != "default" {
		t.Errorf("esperado perfil default em fallback, obtido: %s", reassigned[0].Profile)
	}
}

func TestFindBestReserve(t *testing.T) {
	pool := []AccountScore{
		{ID: "account6", Score: 40.0},
		{ID: "account7", Score: 85.5},
		{ID: "account8", Score: 60.0},
	}

	best := FindBestReserve(pool)
	if best == nil || best.ID != "account7" {
		t.Fatalf("esperado account7 como melhor reserva, obtido: %v", best)
	}

	emptyBest := FindBestReserve(nil)
	if emptyBest != nil {
		t.Errorf("esperado nil para pool vazio, obtido: %v", emptyBest)
	}
}

