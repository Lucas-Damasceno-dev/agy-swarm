package metrics

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestMetrics_InitAndRecordSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_metrics.db")

	store, err := NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Snapshot 1 (10 requests na hora atual)
	err = store.RecordSnapshot("account1", 10, 50, 60000, 0)
	if err != nil {
		t.Fatalf("failed to record snapshot: %v", err)
	}

	// Snapshot 2 (novo valor cumulativo da hora atual: 15 requests)
	err = store.RecordSnapshot("account1", 15, 55, 70000, 1)
	if err != nil {
		t.Fatalf("failed to record second snapshot: %v", err)
	}

	// Snapshot 3 (idempotência: reenviando 15 requests não deve inflar para 30)
	err = store.RecordSnapshot("account1", 15, 55, 70000, 1)
	if err != nil {
		t.Fatalf("failed to record third snapshot: %v", err)
	}

	// Consulta sparkline
	sparkData := store.GetSparklineData("account1")
	if len(sparkData) != 24 {
		t.Fatalf("expected 24 hourly points, got %d", len(sparkData))
	}

	// O último ponto (hora atual) deve registrar MAX = 15 requests sem corrupção multiplicativa
	currentHourRequests := sparkData[23]
	if currentHourRequests != 15 {
		t.Fatalf("expected current hour requests to be 15, got %d", currentHourRequests)
	}
}

func TestMetrics_RenderSparkline(t *testing.T) {
	// Caso 1: Todos zeros -> 24 espaços
	zeros := make([]int, 24)
	outZeros := RenderSparkline(zeros)
	if outZeros != strings.Repeat(" ", 24) {
		t.Fatalf("expected 24 spaces for all zeros, got '%s' (len: %d)", outZeros, len(outZeros))
	}

	// Caso 2: Padrão com valores
	data := []int{
		0, 0, 0, 0, 0, 0, 0, 0,
		1, 2, 3, 4, 5, 6, 7, 8,
		8, 8, 8, 8, 8, 8, 8, 8,
	}
	out := RenderSparkline(data)
	runes := []rune(out)
	if len(runes) != 24 {
		t.Fatalf("expected 24 runes, got %d", len(runes))
	}

	// Primeiros 8 devem ser espaços
	for i := 0; i < 8; i++ {
		if runes[i] != ' ' {
			t.Fatalf("expected space at index %d, got '%c'", i, runes[i])
		}
	}

	// Último ponto (valor 8, que é o máximo) deve ser '█'
	if runes[23] != '█' {
		t.Fatalf("expected max bar '█' at index 23, got '%c'", runes[23])
	}
}

func TestMetrics_ConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "concurrent_metrics.db")

	store, err := NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func(id int) {
			defer wg.Done()
			_ = store.RecordSnapshot("account1", id, id*10, id*1000, 0)
		}(i)

		go func() {
			defer wg.Done()
			_ = store.GetSparklineData("account1")
		}()
	}
	wg.Wait()
}

func TestMetrics_RecordExecutionAndLeaderboard(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "leaderboard_test.db")

	store, err := NewStoreWithPath(dbPath)
	if err != nil {
		t.Fatalf("falha ao criar store: %v", err)
	}
	defer store.Close()

	// Account 1: 2 vitórias, 1 falha
	_ = store.RecordExecution("account1", "CARD-01", "Dev1", "Implementar Auth", "WINNER", 1200, 450, "")
	_ = store.RecordExecution("account1", "CARD-02", "Dev1", "Cache Redis", "WINNER", 800, 200, "")
	_ = store.RecordExecution("account1", "CARD-03", "Dev1", "Refactor Logs", "FAILED", 500, 0, "syntax error")

	// Account 2: 1 vitória
	_ = store.RecordExecution("account2", "CARD-01", "Dev2", "Implementar Auth", "REJECTED", 1500, 300, "")
	_ = store.RecordExecution("account2", "CARD-03", "Dev2", "Refactor Logs", "WINNER", 1000, 150, "")

	board, err := store.GetLeaderboard()
	if err != nil {
		t.Fatalf("erro ao consultar leaderboard: %v", err)
	}
	if len(board) != 2 {
		t.Fatalf("esperava 2 contas no leaderboard, obteve %d", len(board))
	}

	// Primeiro lugar deve ser account1 (2 vitórias)
	if board[0].ProfileID != "account1" {
		t.Errorf("esperava account1 em primeiro, obteve %s", board[0].ProfileID)
	}
	if board[0].Wins != 2 || board[0].TotalRuns != 3 {
		t.Errorf("métricas incorretas para account1: %+v", board[0])
	}
	if board[0].WinRate < 66.0 || board[0].WinRate > 67.0 {
		t.Errorf("winrate incorreta para account1: %f", board[0].WinRate)
	}
	if board[0].TotalDiffBytes != 650 {
		t.Errorf("total diff bytes incorreto para account1: %d", board[0].TotalDiffBytes)
	}

	// Segundo lugar: account2 (1 vitória, 2 runs)
	if board[1].ProfileID != "account2" || board[1].Wins != 1 {
		t.Errorf("métricas incorretas para account2: %+v", board[1])
	}
}
