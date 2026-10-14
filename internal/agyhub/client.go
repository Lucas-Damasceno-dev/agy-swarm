package agyhub

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// ProfileSummary representa o estado de uma conta no daemon do agy-hub.
type ProfileSummary struct {
	ID              string  `json:"id"`
	Alias           string  `json:"alias"`
	Status          string  `json:"status"` // "ACTIVE", "IDLE", "COOLDOWN", "ERROR"
	WeeklyPercent   float64 `json:"weekly_percent"`
	FiveHourPercent float64 `json:"five_hour_percent"`
	QuotaPercent    int     `json:"quota_percent"`
	WeeklyReset     string  `json:"weekly_reset"`
	FiveHourReset   string  `json:"five_hour_reset"`
}

// ClusterState representa a resposta do status do agy-hub.
type ClusterState struct {
	Profiles             []ProfileSummary `json:"profiles"`
	ClusterQuotaTokens   int              `json:"cluster_quota_tokens"`
	ClusterRemaining     int              `json:"cluster_remaining_tokens"`
	ClusterQuotaPercent  int              `json:"cluster_quota_percent"`
	ClusterWeeklyPercent float64          `json:"cluster_weekly_percent"`
	ResetCountdown       string           `json:"reset_countdown"`
}

// Client provê conexão com o daemon local do agy-hub.
type Client struct {
	SocketPath string
}

// NewClient cria um novo cliente para o daemon do agy-hub.
func NewClient(socketPath string) *Client {
	if socketPath == "" {
		socketPath = "/tmp/agy-hub.sock"
	}
	return &Client{SocketPath: socketPath}
}

// IsAvailable verifica se o daemon está respondendo no socket unix.
func (c *Client) IsAvailable() bool {
	conn, err := net.DialTimeout("unix", c.SocketPath, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// GetClusterState consulta o daemon do agy-hub e retorna o estado de todas as contas e cotas.
func (c *Client) GetClusterState() (*ClusterState, error) {
	conn, err := net.DialTimeout("unix", c.SocketPath, 1*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := fmt.Fprintf(conn, "status\n"); err != nil {
		return nil, err
	}

	reader := bufio.NewReader(conn)
	var state ClusterState
	if err := json.NewDecoder(reader).Decode(&state); err != nil {
		return nil, err
	}
	return &state, nil
}

// GetQuarantinedAccounts retorna um mapa das contas com 429 ou erro (status != ACTIVE e != IDLE).
func (c *Client) GetQuarantinedAccounts() (map[string]string, error) {
	state, err := c.GetClusterState()
	if err != nil {
		return nil, err
	}

	quarantined := make(map[string]string)
	for _, p := range state.Profiles {
		if p.Status == "COOLDOWN" || p.Status == "ERROR" {
			quarantined[p.ID] = p.Status
		}
	}
	return quarantined, nil
}
