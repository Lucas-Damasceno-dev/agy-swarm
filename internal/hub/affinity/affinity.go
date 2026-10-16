package affinity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/discovery"
)

const CacheTTL = 5 * time.Minute

type Entry struct {
	ProfileAlias string    `json:"profile_alias"`
	ProfileID    string    `json:"profile_id"`
	LastUsed     time.Time `json:"last_used"`
	Workspace    string    `json:"workspace"`
}

type Manager struct {
	mu       sync.Mutex
	filePath string
	entries  map[string]Entry
}

func NewManager() *Manager {
	realHome := discovery.GetRealHome()
	cfgDir := filepath.Join(realHome, ".config/agy-hub")
	_ = os.MkdirAll(cfgDir, 0755)
	return NewManagerWithPath(filepath.Join(cfgDir, "workspace_affinity.json"))
}

func NewManagerWithPath(filePath string) *Manager {
	dir := filepath.Dir(filePath)
	_ = os.MkdirAll(dir, 0755)

	m := &Manager{
		filePath: filePath,
		entries:  make(map[string]Entry),
	}
	m.load()
	return m
}

func (m *Manager) load() {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.filePath)
	if err == nil {
		_ = json.Unmarshal(data, &m.entries)
	}
}

func (m *Manager) save() {
	data, err := json.MarshalIndent(m.entries, "", "  ")
	if err == nil {
		tmpPath := m.filePath + ".tmp"
		if err := os.WriteFile(tmpPath, data, 0644); err == nil {
			_ = os.Rename(tmpPath, m.filePath)
		}
	}
}

// Record records that a workspace was executed on a profile
func (m *Manager) Record(workspace, profileID, profileAlias string) {
	m.RecordAt(workspace, profileID, profileAlias, time.Now())
}

// RecordAt records affinity with a specific timestamp (useful for testing TTL)
func (m *Manager) RecordAt(workspace, profileID, profileAlias string, t time.Time) {
	if workspace == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	m.entries[workspace] = Entry{
		ProfileAlias: profileAlias,
		ProfileID:    profileID,
		LastUsed:     t,
		Workspace:    workspace,
	}
	m.save()
}

// GetWarmAffinity returns the profile if workspace has a warm KV prompt cache (<5m)
func (m *Manager) GetWarmAffinity(workspace string) (profileID string, alias string, warm bool) {
	if workspace == "" {
		return "", "", false
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, exists := m.entries[workspace]
	if !exists {
		return "", "", false
	}

	age := time.Since(entry.LastUsed)
	if age < CacheTTL {
		return entry.ProfileID, entry.ProfileAlias, true
	}

	return "", "", false
}
