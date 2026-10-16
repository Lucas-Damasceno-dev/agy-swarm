package model

import (
	"fmt"
	"time"
)

// AccountStatus represents health state of an account
type AccountStatus string

const (
	StatusActive   AccountStatus = "ACTIVE"   // Currently running tasks
	StatusIdle     AccountStatus = "IDLE"     // Ready, no load
	StatusCooldown AccountStatus = "COOLDOWN" // 429/ResourceExhausted
	StatusError    AccountStatus = "ERROR"    // Token expired / config error
)

// Profile represents an Antigravity account instance
type Profile struct {
	ID                  string        `json:"id"`                    // e.g. "default", "account2", "account3", ...
	Alias               string        `json:"alias"`                 // e.g. "agy-1", "agy-2", ...
	Email               string        `json:"email"`                 // User's Google email
	HomeDir             string        `json:"home_dir"`              // Isolated HOME dir
	AppDataDir          string        `json:"app_data_dir"`          // Path to .gemini/antigravity-cli
	Status              AccountStatus `json:"status"`                // ACTIVE, IDLE, COOLDOWN, ERROR
	StatusReason        string        `json:"status_reason"`         // Description of status
	IsRunning           bool          `json:"is_running"`            // Has running process
	RunningPIDs         []int         `json:"running_pids"`          // Process IDs
	InCooldown          bool          `json:"in_cooldown"`           // Currently in rate-limit cooldown
	CooldownUntil       time.Time     `json:"cooldown_until"`        // Timestamp until cooldown ends
	TotalConversations  int           `json:"total_conversations"`   // Total conversations in DB
	ActiveConversations int           `json:"active_conversations"`  // Conversations with not_fully_idle = 1
	TotalSteps          int           `json:"total_steps"`           // Total LLM steps executed
	RecentRequests5m    int           `json:"recent_requests_5m"`    // Requests in last 5 minutes
	RPM                 int           `json:"rpm"`                   // Requests in last 1 minute
	RPD                 int           `json:"rpd"`                   // Requests in last 24 hours
	HourlyRequests      int           `json:"hourly_requests"`       // Requests in current hour
	HourlyRateLimits    int           `json:"hourly_rate_limits"`    // Rate limits (429) in current hour
	EstTokens24h        int           `json:"est_tokens_24h"`        // Estimated tokens used in last 24h
	DailyQuotaTokens    int           `json:"daily_quota_tokens"`    // Daily quota limit (default 2.5M)
	RemainingTokens     int           `json:"remaining_tokens"`      // Remaining tokens for current 24h cycle
	QuotaPercent        int           `json:"quota_percent"`         // Percentage of daily quota remaining (0-100)
	RemainingSteps      int           `json:"remaining_steps"`       // Estimated remaining steps
	FiveHourPercent     float64       `json:"five_hour_percent"`     // Real 5-Hour Gemini quota remaining (0-100%)
	FiveHourReset       string        `json:"five_hour_reset"`       // Formatted time until 5-Hour refresh (e.g. "3h 59m")
	WeeklyPercent       float64       `json:"weekly_percent"`        // Real Weekly Claude/GPT quota remaining (0-100%)
	WeeklyReset         string        `json:"weekly_reset"`          // Formatted time until Weekly refresh (e.g. "148h 40m")
	Sparkline           string        `json:"sparkline"`             // 24-hour activity sparkline
	LastActive          time.Time     `json:"last_active"`           // Last activity timestamp
	Priority            int           `json:"priority"`              // Failover priority (100 highest)
	ProxySynced         bool          `json:"proxy_synced"`          // Present in ~/.cli-proxy-api/
	TokenExpiry         time.Time     `json:"token_expiry"`          // OAuth token expiry
	RecentLogLines      []string      `json:"recent_log_lines"`      // Last tail of cli.log for live viewer
	RecentConversations []ConversationPreview `json:"recent_conversations"`
}

// ConversationPreview holds summary of recent session
type ConversationPreview struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	StepCount    int       `json:"step_count"`
	LastModified time.Time `json:"last_modified"`
	Active       bool      `json:"active"`
}

// ClusterState holds the global status across all accounts and proxy
type ClusterState struct {
	Profiles               []*Profile `json:"profiles"`
	ProxyRunning           bool       `json:"proxy_running"`
	ProxyPID               int        `json:"proxy_pid"`
	ProxyPort              int        `json:"proxy_port"`
	ProxyEndpoints         []string   `json:"proxy_endpoints"`
	ProxyRPM               int        `json:"proxy_rpm"`
	HermesRunning          bool       `json:"hermes_running"`
	HermesPID              int        `json:"hermes_pid"`
	HermesStatus           string     `json:"hermes_status"`
	ClusterQuotaTokens     int        `json:"cluster_quota_tokens"`     // Total cluster daily quota
	ClusterRemainingTokens int        `json:"cluster_remaining_tokens"` // Total cluster remaining tokens
	ClusterQuotaPercent    int        `json:"cluster_quota_percent"`    // Total cluster remaining percentage
	ClusterFiveHourPercent float64    `json:"cluster_five_hour_percent"`// Cluster average 5-Hour Gemini quota
	ClusterWeeklyPercent   float64    `json:"cluster_weekly_percent"`   // Cluster average Weekly Claude/GPT quota
	TotalSteps             int        `json:"total_steps"`
	ActiveJobs             int        `json:"active_jobs"`
	ResetCountdown         string     `json:"reset_countdown"`
	LastUpdated            time.Time  `json:"last_updated"`
}

// TimeToQuotaReset returns remaining duration and formatted string until next daily quota reset (Midnight PDT: 07:00 UTC)
func TimeToQuotaReset() string {
	now := time.Now().UTC()
	nextReset := time.Date(now.Year(), now.Month(), now.Day(), 7, 0, 0, 0, time.UTC)
	if now.After(nextReset) {
		nextReset = nextReset.Add(24 * time.Hour)
	}
	rem := time.Until(nextReset).Round(time.Minute)
	h := int(rem.Hours())
	m := int(rem.Minutes()) % 60
	return fmt.Sprintf("%02dh %02dm", h, m)
}
