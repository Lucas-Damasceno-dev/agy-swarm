package quota

import (
	"testing"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/agyhub"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
)

func TestCalculateScore(t *testing.T) {
	// 50% weekly + 100% 5h => (50 * 0.7) + (100 * 0.3) = 35 + 30 = 65
	s := CalculateScore(50.0, 100.0)
	if s != 65.0 {
		t.Errorf("esperado 65.0, obtido: %f", s)
	}

	// Penalidade quando 5h < 20%
	// 80% weekly + 10% 5h => (80 * 0.7) + (10 * 0.3) - 50 = 56 + 3 - 50 = 9
	sPen := CalculateScore(80.0, 10.0)
	if sPen != 9.0 {
		t.Errorf("esperado 9.0 com penalidade de 5h, obtido: %f", sPen)
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

