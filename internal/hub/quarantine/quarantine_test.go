package quarantine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/model"
)

func createMockProfileJSON(t *testing.T, dir, email string, disabled bool) string {
	filePath := filepath.Join(dir, fmt.Sprintf("antigravity-%s.json", email))
	data := map[string]interface{}{
		"email":    email,
		"disabled": disabled,
		"type":     "antigravity",
	}
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal mock json: %v", err)
	}
	if err := os.WriteFile(filePath, bytes, 0644); err != nil {
		t.Fatalf("failed to write mock json: %v", err)
	}
	return filePath
}

func readDisabledFlag(t *testing.T, filePath string) bool {
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	return payload["disabled"].(bool)
}

func TestQuarantine_DynamicDisabledFlag(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManagerWithDir(tmpDir)

	email1 := "user1@example.com"
	email2 := "user2@example.com"

	file1 := createMockProfileJSON(t, tmpDir, email1, false)
	file2 := createMockProfileJSON(t, tmpDir, email2, false)

	p1 := &model.Profile{Email: email1, Status: model.StatusIdle, InCooldown: false}
	p2 := &model.Profile{Email: email2, Status: model.StatusIdle, InCooldown: false}

	// Cenário 1: Tudo saudável, nenhuma alteração
	changed, err := mgr.SyncQuarantine([]*model.Profile{p1, p2})
	if err != nil || changed != 0 {
		t.Fatalf("expected 0 changes for healthy profiles, got changed=%d, err=%v", changed, err)
	}

	// Cenário 2: p1 entra em cooldown -> disabled deve ir para true
	p1.InCooldown = true
	changed, err = mgr.SyncQuarantine([]*model.Profile{p1, p2})
	if err != nil || changed != 1 {
		t.Fatalf("expected 1 change when p1 goes to cooldown, got changed=%d, err=%v", changed, err)
	}
	if !readDisabledFlag(t, file1) {
		t.Fatalf("expected file1 disabled flag to be true")
	}
	if readDisabledFlag(t, file2) {
		t.Fatalf("expected file2 disabled flag to remain false")
	}

	// Cenário 3: p2 entra em StatusError -> disabled deve ir para true
	p2.Status = model.StatusError
	changed, err = mgr.SyncQuarantine([]*model.Profile{p1, p2})
	if err != nil || changed != 1 {
		t.Fatalf("expected 1 change for p2 status error, got changed=%d, err=%v", changed, err)
	}
	if !readDisabledFlag(t, file2) {
		t.Fatalf("expected file2 disabled flag to be true")
	}

	// Cenário 4: Ambos se recuperam -> disabled deve voltar para false
	p1.InCooldown = false
	p2.Status = model.StatusIdle
	changed, err = mgr.SyncQuarantine([]*model.Profile{p1, p2})
	if err != nil || changed != 2 {
		t.Fatalf("expected 2 changes when both recover, got changed=%d, err=%v", changed, err)
	}
	if readDisabledFlag(t, file1) || readDisabledFlag(t, file2) {
		t.Fatalf("expected both files to have disabled=false after recovery")
	}
}

func TestQuarantine_MissingOrInvalidFiles(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManagerWithDir(tmpDir)

	// Arquivo inválido (JSON corrompido)
	corruptPath := filepath.Join(tmpDir, "antigravity-corrupt@example.com.json")
	_ = os.WriteFile(corruptPath, []byte("NOT_A_VALID_JSON{{{"), 0644)

	pCorrupt := &model.Profile{Email: "corrupt@example.com", InCooldown: true}
	pMissing := &model.Profile{Email: "missing@example.com", InCooldown: true}
	pEmptyEmail := &model.Profile{Email: "", InCooldown: true}

	// Não deve quebrar nem panic
	changed, err := mgr.SyncQuarantine([]*model.Profile{pCorrupt, pMissing, pEmptyEmail})
	if err != nil {
		t.Fatalf("unexpected error on missing/invalid files: %v", err)
	}
	if changed != 0 {
		t.Fatalf("expected 0 changes for non-existent/corrupted files, got %d", changed)
	}
}

func TestQuarantine_ConcurrentSync(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManagerWithDir(tmpDir)

	email := "concurrent@example.com"
	createMockProfileJSON(t, tmpDir, email, false)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			p := &model.Profile{Email: email, InCooldown: idx%2 == 0}
			_, _ = mgr.SyncQuarantine([]*model.Profile{p})
		}(i)
	}
	wg.Wait()
}
