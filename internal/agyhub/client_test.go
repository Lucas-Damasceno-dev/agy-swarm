package agyhub

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestClient_IsAvailable_FalseWhenNoSocket(t *testing.T) {
	client := NewClient("/tmp/non-existent-agyhub-test.sock")
	if client.IsAvailable() {
		t.Fatal("expected IsAvailable to return false for non-existent socket")
	}
}

func TestClient_MockDaemon(t *testing.T) {
	sockDir, err := os.MkdirTemp("", "agyhub-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(sockDir)

	sockPath := filepath.Join(sockDir, "test.sock")
	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to listen on unix sock: %v", err)
	}
	defer listener.Close()

	// Servidor Mock
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				n, _ := c.Read(buf)
				cmd := string(buf[:n])
				if cmd == "status\n" {
					resp := ClusterState{
						ClusterQuotaTokens:   10000000,
						ClusterRemaining:     7500000,
						ClusterQuotaPercent:  75,
						ClusterWeeklyPercent: 80.5,
						Profiles: []ProfileSummary{
							{
								ID:            "account1",
								Alias:         "agy-1",
								Status:        "ACTIVE",
								WeeklyPercent: 80.5,
							},
							{
								ID:            "account2",
								Alias:         "agy-2",
								Status:        "COOLDOWN",
								WeeklyPercent: 0.0,
							},
							{
								ID:            "account3",
								Alias:         "agy-3",
								Status:        "ERROR",
								WeeklyPercent: 0.0,
							},
						},
					}
					data, _ := json.Marshal(resp)
					_, _ = fmt.Fprintf(c, "%s\n", data)
				}
			}(conn)
		}
	}()

	client := NewClient(sockPath)
	if !client.IsAvailable() {
		t.Fatal("expected IsAvailable to return true when mock daemon is running")
	}

	state, err := client.GetClusterState()
	if err != nil {
		t.Fatalf("unexpected error from GetClusterState: %v", err)
	}

	if state.ClusterRemaining != 7500000 {
		t.Errorf("expected ClusterRemaining 7500000, got %d", state.ClusterRemaining)
	}
	if len(state.Profiles) != 3 {
		t.Fatalf("expected 3 profiles, got %d", len(state.Profiles))
	}

	quarantined, err := client.GetQuarantinedAccounts()
	if err != nil {
		t.Fatalf("unexpected error from GetQuarantinedAccounts: %v", err)
	}

	if len(quarantined) != 2 {
		t.Fatalf("expected 2 quarantined accounts, got %d", len(quarantined))
	}
	if quarantined["account2"] != "COOLDOWN" {
		t.Errorf("expected account2 to be COOLDOWN, got %s", quarantined["account2"])
	}
	if quarantined["account3"] != "ERROR" {
		t.Errorf("expected account3 to be ERROR, got %s", quarantined["account3"])
	}
}
