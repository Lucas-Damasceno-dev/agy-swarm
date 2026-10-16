package discovery

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExtractNumber(t *testing.T) {
	cases := []struct {
		input    string
		expected int
	}{
		{"account2", 2},
		{"account10", 10},
		{"agy-8", 8},
		{"default", 0},
		{"", 0},
	}

	for _, c := range cases {
		got := extractNumber(c.input)
		if got != c.expected {
			t.Errorf("extractNumber(%q) = %d; expected %d", c.input, got, c.expected)
		}
	}
}

func TestGetRealHome(t *testing.T) {
	// Testa quando REAL_HOME está definido
	origRealHome := os.Getenv("REAL_HOME")
	defer os.Setenv("REAL_HOME", origRealHome)

	os.Setenv("REAL_HOME", "/custom/real/home")
	if got := GetRealHome(); got != "/custom/real/home" {
		t.Errorf("expected /custom/real/home, got %s", got)
	}

	// Testa subprofile HOME
	os.Unsetenv("REAL_HOME")
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	os.Setenv("HOME", "/custom/base/.config/agy-profiles/account5")
	if got := GetRealHome(); got != "/custom/base" {
		t.Errorf("expected /custom/base, got %s", got)
	}
}

func TestExtractEmailFromToken(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "token-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tokenFile := filepath.Join(tempDir, "token.json")

	// Criar JWT fake
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	claims := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"dev@example.com"}`))
	fakeJWT := fmt.Sprintf("%s.%s.", header, claims)

	now := time.Now().Format(time.RFC3339)
	payload := map[string]interface{}{
		"id_token": fakeJWT,
		"token": map[string]string{
			"expiry": now,
		},
	}

	raw, _ := json.Marshal(payload)
	_ = os.WriteFile(tokenFile, raw, 0644)

	email, expiry := ExtractEmailFromToken(tokenFile)
	if email != "dev@example.com" {
		t.Errorf("expected email dev@example.com, got %s", email)
	}
	if expiry.IsZero() {
		t.Error("expected non-zero expiry")
	}
}
