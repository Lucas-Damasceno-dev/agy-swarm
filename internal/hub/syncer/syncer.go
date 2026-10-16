package syncer

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/discovery"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/model"
)

type Syncer struct{}

func NewSyncer() *Syncer {
	return &Syncer{}
}

// SyncAllAccounts writes all discovered profile tokens into ~/.cli-proxy-api/
func (s *Syncer) SyncAllAccounts(profiles []*model.Profile) ([]string, error) {
	realHome := discovery.GetRealHome()
	cliProxyDir := filepath.Join(realHome, ".cli-proxy-api")
	_ = os.MkdirAll(cliProxyDir, 0755)

	var synced []string

	for _, p := range profiles {
		tokenPath := filepath.Join(p.AppDataDir, "antigravity-oauth-token")
		data, err := os.ReadFile(tokenPath)
		if err != nil {
			continue
		}

		var raw struct {
			AuthMethod string `json:"auth_method"`
			IDToken    string `json:"id_token"`
			Token      struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
				Expiry       string `json:"expiry"`
				TokenType    string `json:"token_type"`
			} `json:"token"`
		}

		if err := json.Unmarshal(data, &raw); err != nil {
			continue
		}

		email, _ := discovery.ExtractEmailFromToken(tokenPath)
		if email == "" {
			continue
		}

		expiresIn := 3599
		if raw.Token.Expiry != "" {
			if exp, err := time.Parse(time.RFC3339, raw.Token.Expiry); err == nil {
				secs := int(time.Until(exp).Seconds())
				if secs > 0 {
					expiresIn = secs
				} else {
					expiresIn = 0
				}
			}
		}

		targetFile := filepath.Join(cliProxyDir, fmt.Sprintf("antigravity-%s.json", email))
		payload := map[string]interface{}{
			"access_token":  raw.Token.AccessToken,
			"refresh_token": raw.Token.RefreshToken,
			"expires_in":    expiresIn,
			"expired":       raw.Token.Expiry,
			"timestamp":     time.Now().UnixMilli(),
			"type":          "antigravity",
			"email":         email,
			"project_id":    "aicode-consumers",
			"disabled":      p.InCooldown || p.Status == model.StatusError,
			"priority":      p.Priority,
		}

		encoded, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			continue
		}

		if err := os.WriteFile(targetFile, encoded, 0644); err == nil {
			synced = append(synced, fmt.Sprintf("%s (%s, Prio: %d)", p.Alias, email, p.Priority))
		}
	}

	// Ensure config.yaml has round-robin routing
	s.ensureRoundRobin(realHome)

	return synced, nil
}

func (s *Syncer) ensureRoundRobin(realHome string) {
	cfgPaths := []string{
		filepath.Join(realHome, ".config/cli-proxy-api/config.yaml"),
		filepath.Join(realHome, ".cli-proxy-api/config.yaml"),
	}

	for _, cp := range cfgPaths {
		data, err := os.ReadFile(cp)
		if err != nil {
			continue
		}
		str := string(data)
		if stringsContains(str, "routing:") {
			re := regexp.MustCompile(`(?s)(routing:.*?strategy:\s*)["']?[a-zA-Z0-9_-]+["']?`)
			newStr := re.ReplaceAllString(str, `${1}"round-robin"`)
			if newStr != str {
				_ = os.WriteFile(cp, []byte(newStr), 0644)
			}
		} else {
			str += "\nrouting:\n  strategy: \"round-robin\"\n"
			_ = os.WriteFile(cp, []byte(str), 0644)
		}
	}
}

// EnsureProxyRunning starts the cli-proxy-api service only if not already active.
// If already running, it avoids dropping active connections or clearing model catalogs,
// relying on cli-proxy-api's native inotify/fsnotify file watcher to reload auth tokens.
func (s *Syncer) EnsureProxyRunning() error {
	if err := exec.Command("systemctl", "--user", "is-active", "--quiet", "cli-proxy-api").Run(); err == nil {
		return nil
	}
	cmd := exec.Command("systemctl", "--user", "start", "cli-proxy-api")
	return cmd.Run()
}

// RestartProxyService triggers an explicit systemctl --user restart cli-proxy-api
func (s *Syncer) RestartProxyService() error {
	cmd := exec.Command("systemctl", "--user", "restart", "cli-proxy-api")
	return cmd.Run()
}

func stringsContains(s, substr string) bool {
	return len(s) >= len(substr) && regexp.MustCompile(regexp.QuoteMeta(substr)).MatchString(s)
}
