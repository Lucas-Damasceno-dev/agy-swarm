package quarantine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/discovery"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/model"
)

type Manager struct {
	cliProxyDir string
}

func NewManager() *Manager {
	realHome := discovery.GetRealHome()
	return NewManagerWithDir(filepath.Join(realHome, ".cli-proxy-api"))
}

func NewManagerWithDir(dir string) *Manager {
	return &Manager{cliProxyDir: dir}
}

// SyncQuarantine updates the "disabled" flag in ~/.cli-proxy-api/antigravity-<email>.json
func (m *Manager) SyncQuarantine(profiles []*model.Profile) (changedCount int, err error) {
	cliProxyDir := m.cliProxyDir
	if cliProxyDir == "" {
		realHome := discovery.GetRealHome()
		cliProxyDir = filepath.Join(realHome, ".cli-proxy-api")
	}

	for _, p := range profiles {
		if p.Email == "" {
			continue
		}

		targetFile := filepath.Join(cliProxyDir, fmt.Sprintf("antigravity-%s.json", p.Email))
		data, err := os.ReadFile(targetFile)
		if err != nil {
			continue
		}

		var payload map[string]interface{}
		if err := json.Unmarshal(data, &payload); err != nil {
			continue
		}

		shouldDisable := p.InCooldown || p.Status == model.StatusError
		currentDisabled, _ := payload["disabled"].(bool)

		if currentDisabled != shouldDisable {
			payload["disabled"] = shouldDisable
			if encoded, err := json.MarshalIndent(payload, "", "  "); err == nil {
				if err := os.WriteFile(targetFile, encoded, 0644); err == nil {
					changedCount++
				}
			}
		}
	}

	return changedCount, nil
}
