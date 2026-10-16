package orchestrator

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/affinity"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/model"
)

func newTestOrchestrator(t *testing.T) (*Orchestrator, *affinity.Manager) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "orchestrator_state.json")
	affFile := filepath.Join(tmpDir, "affinity.json")
	affMgr := affinity.NewManagerWithPath(affFile)
	orch := NewOrchestratorWithConfig(stateFile, affMgr)
	return orch, affMgr
}

func TestOrchestrator_LeastLoaded(t *testing.T) {
	orch, _ := newTestOrchestrator(t)

	// Score: RunningPIDs*50 + RecentRequests5m*10 + ActiveConversations*15 - Priority/10
	// p1: 1*50 + 2*10 + 0 - 5 = 65
	// p2: 0*50 + 1*10 + 0 - 1 = 9  -> MENOR SCORE
	// p3: 2*50 + 0*10 + 1*15 - 10 = 105
	p1 := &model.Profile{ID: "acc1", Alias: "agy-1", RunningPIDs: []int{101}, RecentRequests5m: 2, Priority: 50, Status: model.StatusActive, WeeklyPercent: 50.0, RemainingTokens: 500000}
	p2 := &model.Profile{ID: "acc2", Alias: "agy-2", RunningPIDs: []int{}, RecentRequests5m: 1, Priority: 10, Status: model.StatusIdle, WeeklyPercent: 80.0, RemainingTokens: 800000}
	p3 := &model.Profile{ID: "acc3", Alias: "agy-3", RunningPIDs: []int{102, 103}, RecentRequests5m: 0, ActiveConversations: 1, Priority: 100, Status: model.StatusActive, WeeklyPercent: 30.0, RemainingTokens: 300000}

	profiles := []*model.Profile{p1, p2, p3}

	best, reason, err := orch.SelectBest(profiles, StrategyLeastLoaded)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if best.ID != "acc2" {
		t.Fatalf("expected acc2 as least loaded, got %s (reason: %s)", best.ID, reason)
	}
	if !strings.Contains(reason, "Semanal") {
		t.Fatalf("expected reason to contain 'Semanal', got: %s", reason)
	}
}

func TestOrchestrator_RoundRobin(t *testing.T) {
	orch, _ := newTestOrchestrator(t)

	p1 := &model.Profile{ID: "acc1", Alias: "agy-1", Status: model.StatusIdle}
	p2 := &model.Profile{ID: "acc2", Alias: "agy-2", Status: model.StatusIdle}
	p3 := &model.Profile{ID: "acc3", Alias: "agy-3", Status: model.StatusIdle}
	profiles := []*model.Profile{p1, p2, p3}

	// 1ª seleção: deve escolher p1 (lastIdx = -1 -> ( -1 + 1 ) % 3 = 0 -> p1)
	s1, _, err := orch.SelectBest(profiles, StrategyRoundRobin)
	if err != nil || s1.ID != "acc1" {
		t.Fatalf("step 1: expected acc1, got %v (err: %v)", s1, err)
	}

	// 2ª seleção: deve escolher p2
	s2, _, err := orch.SelectBest(profiles, StrategyRoundRobin)
	if err != nil || s2.ID != "acc2" {
		t.Fatalf("step 2: expected acc2, got %v (err: %v)", s2, err)
	}

	// 3ª seleção: deve escolher p3
	s3, _, err := orch.SelectBest(profiles, StrategyRoundRobin)
	if err != nil || s3.ID != "acc3" {
		t.Fatalf("step 3: expected acc3, got %v (err: %v)", s3, err)
	}

	// 4ª seleção: ciclo reinicia em p1
	s4, _, err := orch.SelectBest(profiles, StrategyRoundRobin)
	if err != nil || s4.ID != "acc1" {
		t.Fatalf("step 4: expected acc1, got %v (err: %v)", s4, err)
	}
}

func TestOrchestrator_Priority(t *testing.T) {
	orch, _ := newTestOrchestrator(t)

	p1 := &model.Profile{ID: "acc1", Alias: "agy-1", Priority: 50, Status: model.StatusIdle}
	p2 := &model.Profile{ID: "acc2", Alias: "agy-2", Priority: 200, Status: model.StatusIdle}
	p3 := &model.Profile{ID: "acc3", Alias: "agy-3", Priority: 80, Status: model.StatusIdle}
	profiles := []*model.Profile{p1, p2, p3}

	best, reason, err := orch.SelectBest(profiles, StrategyPriority)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if best.ID != "acc2" {
		t.Fatalf("expected acc2 with highest priority 200, got %s", best.ID)
	}
	if !strings.Contains(reason, "Highest priority (200)") {
		t.Fatalf("expected priority reason, got %s", reason)
	}
}

func TestOrchestrator_CooldownBypass(t *testing.T) {
	orch, _ := newTestOrchestrator(t)

	// p1 em cooldown ativo (termina em 10m)
	// p2 saudável
	// p3 com StatusError
	p1 := &model.Profile{
		ID:            "acc1",
		Alias:         "agy-1",
		Priority:      100,
		InCooldown:    true,
		CooldownUntil: time.Now().Add(10 * time.Minute),
		Status:        model.StatusCooldown,
	}
	p2 := &model.Profile{
		ID:       "acc2",
		Alias:    "agy-2",
		Priority: 10,
		Status:   model.StatusIdle,
	}
	p3 := &model.Profile{
		ID:       "acc3",
		Alias:    "agy-3",
		Priority: 100,
		Status:   model.StatusError,
	}

	best, _, err := orch.SelectBest([]*model.Profile{p1, p2, p3}, StrategyPriority)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if best.ID != "acc2" {
		t.Fatalf("expected cooldown/error bypass selecting acc2, got %s", best.ID)
	}
}

func TestOrchestrator_AllInCooldown(t *testing.T) {
	orch, _ := newTestOrchestrator(t)

	now := time.Now()
	// p1 expira em 10 minutos
	p1 := &model.Profile{
		ID:            "acc1",
		Alias:         "agy-1",
		InCooldown:    true,
		CooldownUntil: now.Add(10 * time.Minute),
	}
	// p2 expira em 2 minutos (menor cooldown restante)
	p2 := &model.Profile{
		ID:            "acc2",
		Alias:         "agy-2",
		InCooldown:    true,
		CooldownUntil: now.Add(2 * time.Minute),
	}

	best, reason, err := orch.SelectBest([]*model.Profile{p1, p2}, StrategyLeastLoaded)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if best.ID != "acc2" {
		t.Fatalf("expected acc2 with earliest cooldown expiration, got %s", best.ID)
	}
	if !strings.Contains(reason, "All profiles in cooldown") {
		t.Fatalf("expected all in cooldown warning, got %s", reason)
	}
}

func TestOrchestrator_WorkspaceCacheAffinity(t *testing.T) {
	orch, affMgr := newTestOrchestrator(t)

	workspace := "/home/user/project-cached"
	affMgr.Record(workspace, "acc3", "agy-3")

	p1 := &model.Profile{ID: "acc1", Alias: "agy-1", RunningPIDs: []int{}, Priority: 100, WeeklyPercent: 80.0, RemainingTokens: 800000}
	p2 := &model.Profile{ID: "acc2", Alias: "agy-2", RunningPIDs: []int{}, Priority: 100, WeeklyPercent: 80.0, RemainingTokens: 800000}
	p3 := &model.Profile{ID: "acc3", Alias: "agy-3", RunningPIDs: []int{10}, Priority: 10, WeeklyPercent: 80.0, RemainingTokens: 800000} // Menor prioridade, mas com afinidade e cotas saudáveis!

	best, reason, err := orch.SelectBestWithWorkspace([]*model.Profile{p1, p2, p3}, StrategyPriority, workspace)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if best.ID != "acc3" {
		t.Fatalf("expected cache affinity hit to choose acc3, got %s", best.ID)
	}
	if !strings.Contains(reason, "Cache Affinity Hit") {
		t.Fatalf("expected cache affinity reason, got %s", reason)
	}

	// Se acc3 já tiver 3 PIDs em execução, não deve pegar o hit de cache
	p3.RunningPIDs = []int{1, 2, 3}
	best2, _, err := orch.SelectBestWithWorkspace([]*model.Profile{p1, p2, p3}, StrategyPriority, workspace)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if best2.ID == "acc3" {
		t.Fatalf("expected affinity to be bypassed when running PIDs >= 3")
	}
}

func TestOrchestrator_EmptyProfiles(t *testing.T) {
	orch, _ := newTestOrchestrator(t)
	_, _, err := orch.SelectBest([]*model.Profile{}, StrategyLeastLoaded)
	if err == nil {
		t.Fatalf("expected error when selecting from empty slice")
	}
}

func TestOrchestrator_PreviewBestDoesNotPersistState(t *testing.T) {
	orch, _ := newTestOrchestrator(t)
	p := &model.Profile{ID: "acc1", Alias: "agy-1", Status: model.StatusIdle, WeeklyPercent: 50.0, RemainingTokens: 500000}

	// 1. PreviewBest should select acc1 without creating or touching state file
	best, _, err := orch.PreviewBest([]*model.Profile{p}, StrategyLeastLoaded)
	if err != nil || best == nil {
		t.Fatalf("unexpected error on PreviewBest: %v", err)
	}
	state := orch.loadState()
	if state.LastSelectedAlias != "" {
		t.Fatalf("expected state file to remain empty after PreviewBest, got %s", state.LastSelectedAlias)
	}

	// 2. SelectBest should mutate state file
	best, _, err = orch.SelectBest([]*model.Profile{p}, StrategyLeastLoaded)
	if err != nil || best == nil {
		t.Fatalf("unexpected error on SelectBest: %v", err)
	}
	state = orch.loadState()
	if state.LastSelectedAlias != "agy-1" {
		t.Fatalf("expected state file to record agy-1 after SelectBest, got %s", state.LastSelectedAlias)
	}
}
