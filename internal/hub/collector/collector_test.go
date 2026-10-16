package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/model"
)

func TestCollector_TailFile(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	// Criar arquivo pequeno (50 linhas)
	var sb strings.Builder
	for i := 1; i <= 50; i++ {
		sb.WriteString(fmt.Sprintf("I0914 01:00:%02d line %d\n", i%60, i))
	}
	if err := os.WriteFile(logPath, []byte(sb.String()), 0644); err != nil {
		t.Fatalf("failed to write test log: %v", err)
	}

	file, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("failed to open test log: %v", err)
	}
	defer file.Close()

	lines := tailFile(file, 20)
	if len(lines) != 20 {
		t.Fatalf("expected 20 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[len(lines)-1], "line 50") {
		t.Fatalf("expected last line to be line 50, got %s", lines[len(lines)-1])
	}
}

func TestCollector_TailFileLarge(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "large.log")

	// Criar arquivo com > 200 KB para testar o reverse seek
	f, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("failed to create large log: %v", err)
	}
	for i := 1; i <= 3000; i++ {
		fmt.Fprintf(f, "I0914 01:00:%02d [worker] verbose log entry number %d with extra padding padding padding\n", i%60, i)
	}
	f.Close()

	file, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("failed to open large log: %v", err)
	}
	defer file.Close()

	lines := tailFile(file, 100)
	if len(lines) != 100 {
		t.Fatalf("expected 100 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[len(lines)-1], "number 3000") {
		t.Fatalf("expected last line to be number 3000, got: %s", lines[len(lines)-1])
	}
}

func TestCollector_TailFile_Over64KBLine(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "huge_line.log")

	// Create a line of 120 KB (exceeding standard 64KB bufio.MaxScanTokenSize)
	hugeContent := strings.Repeat("x", 120*1024)
	content := fmt.Sprintf("I0914 01:00:01 normal line 1\nI0914 01:00:02 %s\nI0914 01:00:03 line after huge payload\n", hugeContent)
	if err := os.WriteFile(logPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write huge line log: %v", err)
	}

	file, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("failed to open log: %v", err)
	}
	defer file.Close()

	lines := tailFile(file, 10)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d (scanner aborted prematurely)", len(lines))
	}
	if !strings.Contains(lines[2], "line after huge payload") {
		t.Fatalf("expected last line to be read after huge line, got %s", lines[2])
	}
}

func TestCollector_EvaluateStatus(t *testing.T) {
	c := NewCollector()

	// 1. Perfil em cooldown
	pCooldown := &model.Profile{
		InCooldown:    true,
		CooldownUntil: time.Now().Add(5 * time.Minute),
	}
	c.evaluateStatus(pCooldown)
	if pCooldown.Status != model.StatusCooldown {
		t.Fatalf("expected StatusCooldown, got %s", pCooldown.Status)
	}

	// 2. Perfil com processo ativo
	pActive := &model.Profile{
		IsRunning:   true,
		RunningPIDs: []int{1234},
	}
	c.evaluateStatus(pActive)
	if pActive.Status != model.StatusActive {
		t.Fatalf("expected StatusActive, got %s", pActive.Status)
	}

	// 3. Perfil sem email
	pNoEmail := &model.Profile{
		Email: "",
	}
	c.evaluateStatus(pNoEmail)
	if pNoEmail.Status != model.StatusError {
		t.Fatalf("expected StatusError for missing email, got %s", pNoEmail.Status)
	}

	// 4. Perfil ocioso pronto
	pIdle := &model.Profile{
		Email: "ready@example.com",
	}
	c.evaluateStatus(pIdle)
	if pIdle.Status != model.StatusIdle {
		t.Fatalf("expected StatusIdle, got %s", pIdle.Status)
	}
}

func TestCollector_ConcurrentDatabaseMetrics(t *testing.T) {
	c := NewCollector()
	tmpDir := t.TempDir()

	p := &model.Profile{
		ID:         "test-prof",
		AppDataDir: tmpDir,
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.collectDatabaseMetrics(p)
		}()
	}
	wg.Wait()
}

func TestCollector_LogRateLimitNoFalsePositives(t *testing.T) {
	c := NewCollector()
	tmpDir := t.TempDir()

	// Create a log file with timestamp containing 429 in microseconds on an INFO line
	nowStr := time.Now().Format("0102 15:04:05")
	logContent := fmt.Sprintf("I%s.629429 266 manager.go:1304] Reloading system slash commands and skills\nI%s.429235 282 manager.go:907] Full redraw completed\n", nowStr, nowStr)
	if err := os.WriteFile(filepath.Join(tmpDir, "cli.log"), []byte(logContent), 0644); err != nil {
		t.Fatalf("failed to write log: %v", err)
	}

	p := &model.Profile{
		ID:         "test-false-pos",
		AppDataDir: tmpDir,
	}

	c.collectLogMetrics(p)
	if p.InCooldown {
		t.Fatalf("expected InCooldown to be false for INFO lines with 429 in timestamp, got true")
	}

	// Now append a real ERROR line with 429
	errLine := fmt.Sprintf("E%s.123456 100 http.go:50] status: 429 RESOURCE_EXHAUSTED\n", nowStr)
	f, _ := os.OpenFile(filepath.Join(tmpDir, "cli.log"), os.O_APPEND|os.O_WRONLY, 0644)
	_, _ = f.WriteString(errLine)
	f.Close()

	c.collectLogMetrics(p)
	if !p.InCooldown {
		t.Fatalf("expected InCooldown to be true for real error with 429, got false")
	}

	// Now simulate manual reset
	c.ResetCooldown(p.ID)
	p.InCooldown = false
	p.CooldownUntil = time.Time{}

	// Subsequent collectLogMetrics on same log should NOT set InCooldown again
	c.collectLogMetrics(p)
	if p.InCooldown {
		t.Fatalf("expected InCooldown to remain false after manual ResetCooldown, but was re-triggered")
	}
}
