package lease

import (
	"context"
	"testing"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/agyhub"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
)

type mockAccountProvider struct {
	available bool
	state     *agyhub.ClusterState
	err       error
}

func (m *mockAccountProvider) IsAvailable() bool {
	return m.available
}

func (m *mockAccountProvider) GetClusterState() (*agyhub.ClusterState, error) {
	return m.state, m.err
}

func TestBrokerAcquireAndRelease(t *testing.T) {
	provider := &mockAccountProvider{
		available: true,
		state: &agyhub.ClusterState{
			Profiles: []agyhub.ProfileSummary{
				{ID: "default", WeeklyPercent: 50, FiveHourPercent: 40},
				{ID: "account2", WeeklyPercent: 90, FiveHourPercent: 85},
				{ID: "account3", WeeklyPercent: 70, FiveHourPercent: 60},
			},
		},
	}

	broker := NewBroker(provider)
	ctx := context.Background()

	// Deve alocar a conta mais saudável primeiro (account2: 90*0.7 + 85*0.3 = 63 + 25.5 = 88.5)
	lease, err := broker.Acquire(ctx, config.RoleHacker, "StageBestOfN")
	if err != nil {
		t.Fatalf("falha ao adquirir lease: %v", err)
	}

	if lease.AccountID != "account2" {
		t.Errorf("esperava account2 com maior score, obteve %s", lease.AccountID)
	}
	if lease.Status != StatusAcquired {
		t.Errorf("status inesperado: %s", lease.Status)
	}

	// Segundo lease não deve reutilizar account2 enquanto ocupada
	lease2, err := broker.Acquire(ctx, config.RoleQA, "StageRedTeam")
	if err != nil {
		t.Fatalf("falha ao adquirir segundo lease: %v", err)
	}
	if lease2.AccountID == lease.AccountID {
		t.Errorf("segundo lease adquiriu a mesma conta em uso: %s", lease2.AccountID)
	}

	// Libera o primeiro lease
	broker.Release(lease)
	if lease.Status != StatusReleased {
		t.Errorf("status esperado RELEASED, obteve %s", lease.Status)
	}

	// Agora account2 deve estar disponível novamente
	lease3, err := broker.Acquire(ctx, config.RoleArch, "StageContract")
	if err != nil {
		t.Fatalf("falha ao adquirir terceiro lease: %v", err)
	}
	if lease3.AccountID != "account2" {
		t.Errorf("esperava account2 de volta após release, obteve %s", lease3.AccountID)
	}
}

func TestBrokerFailover429(t *testing.T) {
	provider := &mockAccountProvider{
		available: true,
		state: &agyhub.ClusterState{
			Profiles: []agyhub.ProfileSummary{
				{ID: "account2", WeeklyPercent: 95, FiveHourPercent: 90},
				{ID: "account3", WeeklyPercent: 80, FiveHourPercent: 75},
			},
		},
	}

	broker := NewBroker(provider)
	ctx := context.Background()

	lease, err := broker.Acquire(ctx, config.RoleHacker, "StageBestOfN")
	if err != nil {
		t.Fatalf("falha ao adquirir lease: %v", err)
	}
	if lease.AccountID != "account2" {
		t.Fatalf("esperava account2, obteve %s", lease.AccountID)
	}

	// Simula 429 e Failover
	newLease, err := broker.Failover(ctx, lease)
	if err != nil {
		t.Fatalf("failover falhou: %v", err)
	}

	if newLease.AccountID == "account2" {
		t.Errorf("failover não trocou a conta com 429: ainda %s", newLease.AccountID)
	}
	if newLease.Role != config.RoleHacker || newLease.Stage != "StageBestOfN" {
		t.Errorf("failover alterou metadados do lease: role=%s stage=%s", newLease.Role, newLease.Stage)
	}

	// Confirma snapshots
	snaps := broker.Snapshots()
	foundOld := false
	for _, s := range snaps {
		if s.AccountID == "account2" {
			foundOld = true
			if s.Status != "COOLDOWN" {
				t.Errorf("status da conta antiga deve ser COOLDOWN, obteve %s", s.Status)
			}
		}
	}
	if !foundOld {
		t.Errorf("account2 não encontrada nos snapshots")
	}
}
