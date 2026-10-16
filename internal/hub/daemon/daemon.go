package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/collector"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/model"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/orchestrator"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/quarantine"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/syncer"
)

func GetDefaultSocketPath() string {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir != "" {
		dir := filepath.Join(runtimeDir, "agy-hub")
		_ = os.MkdirAll(dir, 0700)
		return filepath.Join(dir, "agy-hub.sock")
	}
	return "/tmp/agy-hub.sock"
}

var SocketPath = GetDefaultSocketPath()

type Server struct {
	socketPath   string
	collector    *collector.Collector
	orchestrator *orchestrator.Orchestrator
	syncer       *syncer.Syncer
	quarantine   *quarantine.Manager
	listener     net.Listener
	mu           sync.RWMutex
	cachedState  *model.ClusterState
	running      atomic.Bool
	stopOnce     sync.Once
	stopChan     chan struct{}
	wg           sync.WaitGroup
}

func NewServer() *Server {
	return NewServerWithSocket(SocketPath)
}

func NewServerWithSocket(socketPath string) *Server {
	return &Server{
		socketPath:   socketPath,
		collector:    collector.NewCollector(),
		orchestrator: orchestrator.NewOrchestrator(),
		syncer:       syncer.NewSyncer(),
		quarantine:   quarantine.NewManager(),
		stopChan:     make(chan struct{}),
	}
}

// Start runs the daemon loop and listens on the configured unix socket
func (s *Server) Start() error {
	_ = os.Remove(s.socketPath)

	l, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("falha ao criar unix socket %s: %w", s.socketPath, err)
	}
	s.listener = l
	_ = os.Chmod(s.socketPath, 0600)
	s.running.Store(true)

	// Background ticker for telemetry and quarantine (with immediate initial collect)
	go func() {
		if st, err := s.collector.CollectAll(); err == nil {
			s.mu.Lock()
			s.cachedState = st
			s.mu.Unlock()
			_, _ = s.quarantine.SyncQuarantine(st.Profiles)
			s.dumpStateFile(st)
		}
		s.loop()
	}()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-sigChan:
			s.Stop()
			os.Exit(0)
		case <-s.stopChan:
			return
		}
	}()

	fmt.Printf("⚡ Daemon agy-hub ativo e escutando em unix://%s\n", s.socketPath)

	for s.running.Load() {
		conn, err := l.Accept()
		if err != nil {
			if !s.running.Load() {
				break
			}
			continue
		}
		s.wg.Add(1)
		go func(c net.Conn) {
			defer s.wg.Done()
			s.handleConnection(c)
		}(conn)
	}

	return nil
}

func (s *Server) Stop() {
	s.stopOnce.Do(func() {
		s.running.Store(false)
		close(s.stopChan)
		if s.listener != nil {
			_ = s.listener.Close()
		}
		s.wg.Wait()
		_ = os.Remove(s.socketPath)
	})
}

func (s *Server) loop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			if !s.running.Load() {
				return
			}
			st, err := s.collector.CollectAll()
			if err == nil {
				s.mu.Lock()
				s.cachedState = st
				s.mu.Unlock()

				// Sync quarantine into Hermes
				_, _ = s.quarantine.SyncQuarantine(st.Profiles)

				// Export runtime state for external consumers (e.g. scripts, polybar, waybar)
				s.dumpStateFile(st)
			}
		}
	}
}

func (s *Server) dumpStateFile(st *model.ClusterState) {
	if st == nil {
		return
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = "/run/user/1000"
	}
	targetDir := filepath.Join(runtimeDir, "agy-hub")
	_ = os.MkdirAll(targetDir, 0755)
	tmpFile := filepath.Join(targetDir, "state.json.tmp")
	destFile := filepath.Join(targetDir, "state.json")

	if data, err := json.MarshalIndent(st, "", "  "); err == nil {
		if err := os.WriteFile(tmpFile, data, 0644); err == nil {
			_ = os.Rename(tmpFile, destFile)
		}
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		return
	}
	cmd := strings.TrimSpace(scanner.Text())

	switch {
	case cmd == "status":
		s.mu.RLock()
		st := s.cachedState
		var data []byte
		var err error
		if st != nil {
			data, err = json.Marshal(st)
		}
		s.mu.RUnlock()

		if err != nil || data == nil {
			if fresh, fErr := s.collector.CollectAll(); fErr == nil {
				s.mu.Lock()
				s.cachedState = fresh
				s.mu.Unlock()
				_ = json.NewEncoder(conn).Encode(fresh)
				return
			}
			_ = json.NewEncoder(conn).Encode(st)
			return
		}
		_, _ = conn.Write(append(data, '\n'))

	case cmd == "which":
		s.mu.RLock()
		var profiles []*model.Profile
		if s.cachedState != nil && len(s.cachedState.Profiles) > 0 {
			profiles = make([]*model.Profile, len(s.cachedState.Profiles))
			copy(profiles, s.cachedState.Profiles)
		}
		s.mu.RUnlock()

		if len(profiles) == 0 {
			_ = json.NewEncoder(conn).Encode(map[string]interface{}{
				"error": "no profiles available",
			})
			return
		}
		best, reason, err := s.orchestrator.SelectBest(profiles, orchestrator.StrategyLeastLoaded)
		resp := map[string]interface{}{
			"best":   best,
			"reason": reason,
			"error":  err,
		}
		_ = json.NewEncoder(conn).Encode(resp)

	case cmd == "sync":
		s.mu.RLock()
		var profiles []*model.Profile
		if s.cachedState != nil && len(s.cachedState.Profiles) > 0 {
			profiles = make([]*model.Profile, len(s.cachedState.Profiles))
			copy(profiles, s.cachedState.Profiles)
		}
		s.mu.RUnlock()

		if len(profiles) > 0 {
			synced, err := s.syncer.SyncAllAccounts(profiles)
			_ = s.syncer.EnsureProxyRunning()
			_ = json.NewEncoder(conn).Encode(map[string]interface{}{
				"synced": synced,
				"error":  err,
			})
		} else {
			_ = json.NewEncoder(conn).Encode(map[string]interface{}{
				"synced": []string{},
				"error":  "no profiles available to sync",
			})
		}

	case strings.HasPrefix(cmd, "reset-cooldown"):
		parts := strings.Fields(cmd)
		targetID := ""
		if len(parts) > 1 {
			targetID = parts[1]
		}
		s.collector.ResetCooldown(targetID)

		s.mu.Lock()
		if s.cachedState != nil {
			for _, p := range s.cachedState.Profiles {
				matches := targetID == "" || targetID == "all" || targetID == "*" ||
					strings.EqualFold(p.ID, targetID) ||
					strings.EqualFold(p.Alias, targetID) ||
					(p.Email != "" && strings.EqualFold(p.Email, targetID))
				if matches {
					p.InCooldown = false
					p.CooldownUntil = time.Time{}
					if p.Status == model.StatusCooldown {
						p.Status = model.StatusIdle
						p.StatusReason = "Ready / Standby"
					}
				}
			}
			_, _ = s.quarantine.SyncQuarantine(s.cachedState.Profiles)
		}
		s.mu.Unlock()
		_ = json.NewEncoder(conn).Encode(map[string]interface{}{"status": "ok", "message": "cooldown reset", "target": targetID})

	default:
		_ = json.NewEncoder(conn).Encode(map[string]string{"error": "unknown command"})
	}
}

// FetchStateFromDaemon attempts to read cached state from socket with graceful retry reconnection
func FetchStateFromDaemon() (*model.ClusterState, error) {
	st, err := FetchStateFromSocket(GetDefaultSocketPath())
	if err == nil {
		return st, nil
	}
	defaultPath := GetDefaultSocketPath()
	if defaultPath != "/tmp/agy-hub.sock" {
		if st2, err2 := FetchStateFromSocket("/tmp/agy-hub.sock"); err2 == nil {
			return st2, nil
		}
	}
	return nil, err
}

// FetchStateFromSocket attempts to read state from a specific socket with graceful reconnect
func FetchStateFromSocket(socketPath string) (*model.ClusterState, error) {
	return FetchStateWithRetries(socketPath, 3, 30*time.Millisecond)
}

// FetchStateWithRetries connects to daemon with retries and exponential backoff
func FetchStateWithRetries(socketPath string, maxAttempts int, initialBackoff time.Duration) (*model.ClusterState, error) {
	var lastErr error
	backoff := initialBackoff

	for attempt := 0; attempt < maxAttempts; attempt++ {
		conn, err := net.DialTimeout("unix", socketPath, 150*time.Millisecond)
		if err == nil {
			_ = conn.SetDeadline(time.Now().Add(300 * time.Millisecond))
			if _, err := fmt.Fprintf(conn, "status\n"); err == nil {
				var state model.ClusterState
				if err := json.NewDecoder(conn).Decode(&state); err == nil {
					_ = conn.Close()
					return &state, nil
				}
				lastErr = err
			} else {
				lastErr = err
			}
			_ = conn.Close()
		} else {
			lastErr = err
		}

		if attempt < maxAttempts-1 {
			time.Sleep(backoff)
			backoff *= 2
		}
	}
	return nil, fmt.Errorf("daemon connection failed after %d attempts: %w", maxAttempts, lastErr)
}
