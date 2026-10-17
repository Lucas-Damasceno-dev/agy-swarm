package daemon

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDaemon_LifecycleAndCommands(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "daemon_test.sock")

	server := NewServerWithSocket(sockPath)

	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Start()
	}()

	// Aguardar servidor subir
	var stateErr error
	for i := 0; i < 20; i++ {
		time.Sleep(25 * time.Millisecond)
		_, stateErr = FetchStateFromSocket(sockPath)
		if stateErr == nil {
			break
		}
	}
	if stateErr != nil {
		t.Fatalf("failed connecting to daemon socket: %v", stateErr)
	}

	// Testar comando status
	st, err := FetchStateFromSocket(sockPath)
	if err != nil {
		t.Fatalf("expected successful status fetch, got %v", err)
	}
	if st == nil {
		t.Fatalf("expected non-nil cluster state")
	}

	// Parar servidor
	server.Stop()

	select {
	case err := <-errChan:
		if err != nil {
			t.Fatalf("unexpected start error: %v", err)
		}
	case <-time.After(1 * time.Second):
	}
}

func TestDaemon_GracefulReconnection(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "reconnect_test.sock")

	// 1. Iniciar servidor 1
	s1 := NewServerWithSocket(sockPath)
	go func() { _ = s1.Start() }()

	// Aguardar subir
	for i := 0; i < 20; i++ {
		time.Sleep(25 * time.Millisecond)
		if _, err := FetchStateFromSocket(sockPath); err == nil {
			break
		}
	}

	// 2. Simular reinício do daemon (Stop s1, e Start s2 após delay curto)
	s1.Stop()

	// Goroutine que inicia novo daemon após 60ms
	go func() {
		time.Sleep(60 * time.Millisecond)
		s2 := NewServerWithSocket(sockPath)
		defer s2.Stop()
		_ = s2.Start()
	}()

	// 3. O cliente tenta buscar o estado com retry/backoff automático
	// Deve sobreviver à janela de restart do daemon graciosamente
	st, err := FetchStateWithRetries(sockPath, 6, 25*time.Millisecond)
	if err != nil {
		t.Fatalf("graceful reconnection failed after daemon restart: %v", err)
	}
	if st == nil {
		t.Fatalf("expected state after graceful reconnection")
	}
}

func TestDaemon_ConcurrentClients(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "concurrent_test.sock")

	server := NewServerWithSocket(sockPath)
	go func() { _ = server.Start() }()
	defer server.Stop()

	// Aguardar subir
	for i := 0; i < 20; i++ {
		time.Sleep(25 * time.Millisecond)
		if _, err := FetchStateFromSocket(sockPath); err == nil {
			break
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < 15; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = FetchStateFromSocket(sockPath)
		}()
	}
	wg.Wait()
}

func TestDaemon_HTTPServer(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "http_test.sock")

	server := NewServerWithSocket(sockPath)
	server.SetHTTPPort(18329) // porta temporária
	go func() { _ = server.Start() }()
	defer server.Stop()

	// Aguarda HTTP server subir
	var healthResp *http.Response
	var err error
	for i := 0; i < 20; i++ {
		time.Sleep(30 * time.Millisecond)
		healthResp, err = http.Get("http://127.0.0.1:18329/health")
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("falha ao conectar no health check HTTP: %v", err)
	}
	defer healthResp.Body.Close()

	if healthResp.StatusCode != http.StatusOK {
		t.Errorf("esperava status 200 no /health, obteve %d", healthResp.StatusCode)
	}
	body, _ := io.ReadAll(healthResp.Body)
	if !strings.Contains(string(body), `"status":"healthy"`) {
		t.Errorf("resposta inesperada do /health: %s", string(body))
	}

	// Testa /metrics
	metricsResp, err := http.Get("http://127.0.0.1:18329/metrics")
	if err != nil {
		t.Fatalf("falha ao consultar /metrics: %v", err)
	}
	defer metricsResp.Body.Close()

	if metricsResp.StatusCode != http.StatusOK {
		t.Errorf("esperava status 200 no /metrics, obteve %d", metricsResp.StatusCode)
	}
	mBody, _ := io.ReadAll(metricsResp.Body)
	if !strings.Contains(string(mBody), "agy_cluster_remaining_tokens") {
		t.Errorf("métricas não contêm agy_cluster_remaining_tokens: %s", string(mBody))
	}

	// Testa /board
	boardResp, err := http.Get("http://127.0.0.1:18329/board")
	if err != nil {
		t.Fatalf("falha ao consultar /board: %v", err)
	}
	defer boardResp.Body.Close()
	if boardResp.StatusCode != http.StatusOK {
		t.Errorf("esperava status 200 no /board, obteve %d", boardResp.StatusCode)
	}
	bBody, _ := io.ReadAll(boardResp.Body)
	if !strings.Contains(string(bBody), "AGY-SWARM // CLUSTER CONTROL PLANE") {
		t.Errorf("/board não retornou o HTML do painel: %s", string(bBody))
	}

	// Testa /api/kanban
	kanbanResp, err := http.Get("http://127.0.0.1:18329/api/kanban")
	if err != nil {
		t.Fatalf("falha ao consultar /api/kanban: %v", err)
	}
	defer kanbanResp.Body.Close()
	if kanbanResp.StatusCode != http.StatusOK {
		t.Errorf("esperava status 200 no /api/kanban, obteve %d", kanbanResp.StatusCode)
	}
}

