package syncer

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/model"
)

func TestEnsureRoundRobin(t *testing.T) {
	tempHome, err := os.MkdirTemp("", "syncer-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempHome)

	cfgDir := filepath.Join(tempHome, ".cli-proxy-api")
	_ = os.MkdirAll(cfgDir, 0755)
	cfgFile := filepath.Join(cfgDir, "config.yaml")

	initialContent := "server:\n  port: 8317\nrouting:\n  strategy: \"priority\"\n"
	_ = os.WriteFile(cfgFile, []byte(initialContent), 0644)

	s := NewSyncer()
	s.ensureRoundRobin(tempHome)

	updatedContent, err := os.ReadFile(cfgFile)
	if err != nil {
		t.Fatalf("failed to read updated config: %v", err)
	}

	if !strings.Contains(string(updatedContent), `"round-robin"`) {
		t.Errorf("expected config to contain round-robin, got:\n%s", string(updatedContent))
	}
}

func TestSyncAllAccounts(t *testing.T) {
	tempHome, err := os.MkdirTemp("", "syncer-accounts-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempHome)

	origRealHome := os.Getenv("REAL_HOME")
	defer os.Setenv("REAL_HOME", origRealHome)
	os.Setenv("REAL_HOME", tempHome)

	appDataDir := filepath.Join(tempHome, "appdata")
	_ = os.MkdirAll(appDataDir, 0755)

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	claims := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"syncer-test@example.com"}`))
	fakeJWT := fmt.Sprintf("%s.%s.", header, claims)

	tokenPayload := map[string]interface{}{
		"id_token": fakeJWT,
		"token": map[string]string{
			"access_token":  "mock-access",
			"refresh_token": "mock-refresh",
			"expiry":        time.Now().Add(1 * time.Hour).Format(time.RFC3339),
		},
	}
	tokenBytes, _ := json.Marshal(tokenPayload)
	_ = os.WriteFile(filepath.Join(appDataDir, "antigravity-oauth-token"), tokenBytes, 0644)

	profiles := []*model.Profile{
		{
			ID:         "account2",
			Alias:      "agy-2",
			AppDataDir: appDataDir,
			Priority:   50,
			Status:     model.StatusIdle,
		},
	}

	s := NewSyncer()
	synced, err := s.SyncAllAccounts(profiles)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(synced) != 1 {
		t.Fatalf("expected 1 synced account, got %d", len(synced))
	}

	targetFile := filepath.Join(tempHome, ".cli-proxy-api", "antigravity-syncer-test@example.com.json")
	if _, err := os.Stat(targetFile); err != nil {
		t.Errorf("expected target sync file to exist at %s: %v", targetFile, err)
	}
}
