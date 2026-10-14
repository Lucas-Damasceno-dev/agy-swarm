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

	stateFile := t.TempDir() + "/leases.json"
	broker := NewBroker(provider, stateFile)
	ctx := context.Background()

	// Deve alocar a conta mais saudável primeiro (account2: maior cota)
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

	stateFile := t.TempDir() + "/leases.json"
	broker := NewBroker(provider, stateFile)
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

func TestBrokerPersistenceCrossProcess(t *testing.T) {
	stateFile := t.TempDir() + "/shared-leases.json"
	provider := &mockAccountProvider{
		available: true,
		state: &agyhub.ClusterState{
			Profiles: []agyhub.ProfileSummary{
				{ID: "account2", WeeklyPercent: 90, FiveHourPercent: 80},
				{ID: "account3", WeeklyPercent: 70, FiveHourPercent: 60},
			},
		},
	}

	// Processo A (ex: Pipeline Engine)
	brokerA := NewBroker(provider, stateFile)
	lease, err := brokerA.Acquire(context.Background(), config.RoleHacker, "StageBestOfN")
	if err != nil {
		t.Fatalf("falha ao adquirir lease em brokerA: %v", err)
	}

	// Processo B (ex: Dashboard --watch)
	brokerB := NewBroker(provider, stateFile)
	snaps := brokerB.Snapshots()

	var account2Snap *AccountStatusSnapshot
	for _, s := range snaps {
		if s.AccountID == "account2" {
			account2Snap = &s
			break
		}
	}

	if account2Snap == nil {
		t.Fatalf("account2 não encontrada em snaps de brokerB")
	}
	if account2Snap.Status != "BUSY" {
		t.Errorf("esperava status BUSY em brokerB após aquisição em brokerA, obteve %s", account2Snap.Status)
	}
	if account2Snap.CurrentRole != config.RoleHacker {
		t.Errorf("esperava role RoleHacker em brokerB, obteve %s", account2Snap.CurrentRole)
	}

	// Libera pelo Processo A
	brokerA.Release(lease)

	// Processo B deve enxergar liberado
	snapsAfter := brokerB.Snapshots()
	for _, s := range snapsAfter {
		if s.AccountID == "account2" && s.Status != "FREE" {
			t.Errorf("esperava account2 FREE em brokerB após release em brokerA, obteve %s", s.Status)
		}
	}
}

func TestBrokerSkipCooldownOrErrorStatus(t *testing.T) {
	stateFile := t.TempDir() + "/leases.json"
	provider := &mockAccountProvider{
		available: true,
		state: &agyhub.ClusterState{
			Profiles: []agyhub.ProfileSummary{
				// account2 tem cota alta mas está em COOLDOWN no hub
				{ID: "account2", WeeklyPercent: 99, FiveHourPercent: 99, Status: "COOLDOWN"},
				// account3 tem status ACTIVE e cota normal
				{ID: "account3", WeeklyPercent: 70, FiveHourPercent: 70, Status: "ACTIVE"},
			},
		},
	}

	broker := NewBroker(provider, stateFile)
	lease, err := broker.Acquire(context.Background(), config.RoleHacker, "StageBestOfN")
	if err != nil {
		t.Fatalf("falha ao adquirir lease: %v", err)
	}

	// Deve pular account2 (COOLDOWN) e escolher account3
	if lease.AccountID != "account3" {
		t.Errorf("esperava pular account2 em COOLDOWN e alocar account3, mas obteve %s", lease.AccountID)
	}

	// Snapshot deve reportar account2 em COOLDOWN
	snaps := broker.Snapshots()
	for _, s := range snaps {
		if s.AccountID == "account2" && s.Status != "COOLDOWN" {
			t.Errorf("esperava status COOLDOWN para account2 no snapshot, obteve %s", s.Status)
		}
	}
}
