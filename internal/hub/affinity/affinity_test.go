package affinity

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestAffinity_RecordAndWarmHit(t *testing.T) {
	tmpDir := t.TempDir()
	affinityFile := filepath.Join(tmpDir, "affinity.json")
	mgr := NewManagerWithPath(affinityFile)

	workspace := "/path/to/project-alpha"
	mgr.Record(workspace, "account3", "agy-3")

	profID, alias, warm := mgr.GetWarmAffinity(workspace)
	if !warm {
		t.Fatalf("expected warm cache hit, got warm=%v", warm)
	}
	if profID != "account3" || alias != "agy-3" {
		t.Fatalf("unexpected affinity result: profID=%s, alias=%s", profID, alias)
	}
}

func TestAffinity_TTLExpiration(t *testing.T) {
	tmpDir := t.TempDir()
	affinityFile := filepath.Join(tmpDir, "affinity.json")
	mgr := NewManagerWithPath(affinityFile)

	workspace := "/path/to/project-expired"
	// Gravar com timestamp de 5 minutos e 1 segundo atrás
	pastTime := time.Now().Add(-5*time.Minute - 1*time.Second)
	mgr.RecordAt(workspace, "account2", "agy-2", pastTime)

	profID, alias, warm := mgr.GetWarmAffinity(workspace)
	if warm {
		t.Fatalf("expected warm=false for expired TTL, got warm=true (profID=%s, alias=%s)", profID, alias)
	}
	if profID != "" || alias != "" {
		t.Fatalf("expected empty strings on expired cache, got profID=%s, alias=%s", profID, alias)
	}
}

func TestAffinity_PersistenceReload(t *testing.T) {
	tmpDir := t.TempDir()
	affinityFile := filepath.Join(tmpDir, "affinity.json")

	mgr1 := NewManagerWithPath(affinityFile)
	mgr1.Record("/workspace/persisted", "account4", "agy-4")

	// Criar nova instância apontando para o mesmo arquivo e checar reload
	mgr2 := NewManagerWithPath(affinityFile)
	profID, alias, warm := mgr2.GetWarmAffinity("/workspace/persisted")
	if !warm || profID != "account4" || alias != "agy-4" {
		t.Fatalf("failed reloading persisted affinity: warm=%v, id=%s, alias=%s", warm, profID, alias)
	}
}

func TestAffinity_EmptyWorkspace(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManagerWithPath(filepath.Join(tmpDir, "affinity.json"))

	mgr.Record("", "account1", "agy-1")
	profID, alias, warm := mgr.GetWarmAffinity("")
	if warm || profID != "" || alias != "" {
		t.Fatalf("expected no-op for empty workspace, got warm=%v, profID=%s", warm, profID)
	}
}

func TestAffinity_ConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManagerWithPath(filepath.Join(tmpDir, "affinity.json"))

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		ws := filepath.Join("/ws", string(rune('a'+i)))
		go func(w string) {
			defer wg.Done()
			mgr.Record(w, "account1", "agy-1")
		}(ws)

		go func(w string) {
			defer wg.Done()
			_, _, _ = mgr.GetWarmAffinity(w)
		}(ws)
	}
	wg.Wait()
}
