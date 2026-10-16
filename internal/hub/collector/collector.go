package collector

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/discovery"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/metrics"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/model"

	_ "modernc.org/sqlite"
)

var (
	pidRegex          = regexp.MustCompile(`^[0-9]+$`)
	reqPattern        = regexp.MustCompile(`([0-9]{4} [0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]+).*streamGenerateContent`)
	rateLimitPattern  = regexp.MustCompile(`(?i)(\b429\b|RESOURCE_EXHAUSTED|quota[_\s]exceeded|rate[_\s]limit)`)
	timePrefixPattern = regexp.MustCompile(`^[IWEF]([0-9]{4} [0-9]{2}:[0-9]{2}:[0-9]{2})`)
)

type dbCacheEntry struct {
	modTime             time.Time
	totalConversations  int
	totalSteps          int
	activeConversations int
	lastActive          time.Time
	estTokens24h        int
	recentConversations []model.ConversationPreview
}

type quotaCacheEntry struct {
	fetchedAt     time.Time
	fiveHourPct   float64
	fiveHourReset string
	weeklyPct     float64
	weeklyReset   string
}

// Collector gathers telemetry, metrics, and health states
type Collector struct {
	httpClient      *http.Client
	metricsStore    *metrics.Store
	mu              sync.RWMutex
	dbCache         map[string]dbCacheEntry
	quotaCache      map[string]quotaCacheEntry
	lastManualReset map[string]time.Time
	lastProxyPID    int
}

func NewCollector() *Collector {
	store, _ := metrics.NewStore()
	return &Collector{
		httpClient: &http.Client{
			Timeout: 4 * time.Second,
		},
		metricsStore:    store,
		dbCache:         make(map[string]dbCacheEntry),
		quotaCache:      make(map[string]quotaCacheEntry),
		lastManualReset: make(map[string]time.Time),
	}
}

// ResetCooldown records manual unblock timestamp so subsequent log parses ignore older 429 entries
func (c *Collector) ResetCooldown(profileID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastManualReset == nil {
		c.lastManualReset = make(map[string]time.Time)
	}
	now := time.Now()
	cleaned := strings.ToLower(strings.TrimSpace(profileID))
	if cleaned == "" || cleaned == "all" || cleaned == "*" {
		c.lastManualReset["*"] = now
	} else {
		c.lastManualReset[cleaned] = now
	}
}

// CollectAll updates cluster state and all profiles
func (c *Collector) CollectAll() (*model.ClusterState, error) {
	profiles, err := discovery.DiscoverProfiles()
	if err != nil {
		return nil, err
	}

	state := &model.ClusterState{
		Profiles:       profiles,
		ProxyPort:      8317,
		ResetCountdown: model.TimeToQuotaReset(),
		LastUpdated:    time.Now(),
	}

	// Check cli-proxy-api service status
	c.checkProxyStatus(state)

	// Check hermes service status
	c.checkHermesStatus(state)

	var sum5h, sumWeekly float64
	var count5h, countWeekly int

	// Collect individual metrics for each profile in parallel
	var wg sync.WaitGroup
	for _, p := range profiles {
		wg.Add(1)
		go func(prof *model.Profile) {
			defer wg.Done()
			c.collectDatabaseMetrics(prof)
			c.collectLogMetrics(prof)
			c.evaluateStatus(prof)
			c.fetchRealUserQuota(prof)
		}(p)
	}
	wg.Wait()

	// Aggregate metrics and compute quotas
	for _, p := range profiles {
		// Record in metrics store & compute sparkline
		if c.metricsStore != nil {
			_ = c.metricsStore.RecordSnapshot(p.ID, p.HourlyRequests, p.TotalSteps, p.EstTokens24h, p.HourlyRateLimits)
			sparkData := c.metricsStore.GetSparklineData(p.ID)
			p.Sparkline = metrics.RenderSparkline(sparkData)
		}

		// Calculate daily quota & remaining tokens (2.5M tokens/day per account)
		p.DailyQuotaTokens = 2500000
		p.RemainingTokens = p.DailyQuotaTokens - p.EstTokens24h
		if p.RemainingTokens < 0 {
			p.RemainingTokens = 0
		}
		p.RemainingSteps = p.RemainingTokens / 1250

		// Prefer real cloud quota if available
		if p.FiveHourPercent > 0 {
			p.QuotaPercent = int(p.FiveHourPercent)
			sum5h += p.FiveHourPercent
			count5h++
		} else if p.WeeklyPercent > 0 {
			p.QuotaPercent = int(p.WeeklyPercent)
		} else {
			p.QuotaPercent = int((int64(p.RemainingTokens) * 100) / int64(p.DailyQuotaTokens))
		}

		if p.WeeklyPercent > 0 {
			sumWeekly += p.WeeklyPercent
			countWeekly++
		}

		state.TotalSteps += p.TotalSteps
		state.ClusterQuotaTokens += p.DailyQuotaTokens
		state.ClusterRemainingTokens += p.RemainingTokens
		if p.IsRunning {
			state.ActiveJobs += len(p.RunningPIDs)
		}
	}

	if count5h > 0 {
		state.ClusterFiveHourPercent = sum5h / float64(count5h)
		state.ClusterQuotaPercent = int(state.ClusterFiveHourPercent)
	} else if state.ClusterQuotaTokens > 0 {
		state.ClusterQuotaPercent = int((int64(state.ClusterRemainingTokens) * 100) / int64(state.ClusterQuotaTokens))
	}

	if countWeekly > 0 {
		state.ClusterWeeklyPercent = sumWeekly / float64(countWeekly)
	}

	return state, nil
}

func (c *Collector) checkProxyStatus(state *model.ClusterState) {
	resp, err := c.httpClient.Get(fmt.Sprintf("http://127.0.0.1:%d/", state.ProxyPort))
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			state.ProxyRunning = true
			var body struct {
				Endpoints []string `json:"endpoints"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err == nil {
				state.ProxyEndpoints = body.Endpoints
			}
		}
	}

	// Check cached PID first to avoid scanning /proc on every tick
	c.mu.RLock()
	cachedPID := c.lastProxyPID
	c.mu.RUnlock()

	if cachedPID > 0 {
		cmdBytes, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", cachedPID))
		if err == nil && strings.Contains(string(cmdBytes), "cli-proxy-api-bin") {
			state.ProxyPID = cachedPID
			state.ProxyRunning = true
			return
		}
	}

	// Find cli-proxy-api PID from /proc if cached is invalid
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() || !pidRegex.MatchString(entry.Name()) {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		cmdBytes, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		if err != nil {
			continue
		}
		if strings.Contains(string(cmdBytes), "cli-proxy-api-bin") {
			state.ProxyPID = pid
			state.ProxyRunning = true
			c.mu.Lock()
			c.lastProxyPID = pid
			c.mu.Unlock()
			break
		}
	}
}

func (c *Collector) checkHermesStatus(state *model.ClusterState) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() || !pidRegex.MatchString(entry.Name()) {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		cmdBytes, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		if err != nil {
			continue
		}
		cmdline := string(cmdBytes)
		if (strings.Contains(cmdline, "hermes_cli") || strings.Contains(cmdline, "hermes-agent")) && strings.Contains(cmdline, "gateway") {
			state.HermesRunning = true
			state.HermesPID = pid
			state.HermesStatus = "ONLINE (Telegram Gateway)"
			return
		}
	}
	state.HermesRunning = false
	state.HermesStatus = "OFFLINE"
}

func (c *Collector) collectDatabaseMetrics(p *model.Profile) {
	dbPath := filepath.Join(p.AppDataDir, "conversation_summaries.db")
	stat, err := os.Stat(dbPath)
	if err != nil {
		return
	}

	// Check mtime cache to avoid SQLite file IO when unchanged
	c.mu.RLock()
	cached, hasCache := c.dbCache[p.ID]
	c.mu.RUnlock()

	if hasCache && cached.modTime.Equal(stat.ModTime()) {
		p.TotalConversations = cached.totalConversations
		p.TotalSteps = cached.totalSteps
		p.ActiveConversations = cached.activeConversations
		p.LastActive = cached.lastActive
		p.EstTokens24h = cached.estTokens24h
		p.RecentConversations = cached.recentConversations
		return
	}

	dsn := fmt.Sprintf("file:%s?mode=ro&_timeout=1000", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return
	}
	defer db.Close()

	var totalConvs, totalSteps, activeConvs int
	var maxModifiedStr string

	row := db.QueryRow(`
		SELECT 
			count(*), 
			coalesce(sum(step_count), 0), 
			coalesce(sum(CASE WHEN not_fully_idle = 1 THEN 1 ELSE 0 END), 0),
			coalesce(max(last_modified_time), '')
		FROM conversation_summaries
	`)
	_ = row.Scan(&totalConvs, &totalSteps, &activeConvs, &maxModifiedStr)

	p.TotalConversations = totalConvs
	p.TotalSteps = totalSteps
	p.ActiveConversations = activeConvs

	// 24h step estimation
	oneDayAgo := time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
	var steps24h int
	row24h := db.QueryRow(`
		SELECT coalesce(sum(step_count), 0)
		FROM conversation_summaries
		WHERE last_modified_time >= ?
	`, oneDayAgo)
	_ = row24h.Scan(&steps24h)

	// Estimate ~1,250 tokens per step average for modern coding agent turns
	p.EstTokens24h = steps24h * 1250

	if maxModifiedStr != "" {
		if t, err := time.Parse("2006-01-02 15:04:05.999999-07:00", maxModifiedStr); err == nil {
			p.LastActive = t
		} else if t, err := time.Parse(time.RFC3339, maxModifiedStr); err == nil {
			p.LastActive = t
		}
	}

	// Auto-heal stale active conversation records if process is dead and inactive
	if !p.IsRunning && activeConvs > 0 && (p.LastActive.IsZero() || time.Since(p.LastActive) > 5*time.Minute) {
		rwDsn := fmt.Sprintf("file:%s?_timeout=2000", dbPath)
		if rwDb, err := sql.Open("sqlite", rwDsn); err == nil {
			_, _ = rwDb.Exec("PRAGMA busy_timeout = 2000; UPDATE conversation_summaries SET not_fully_idle = 0 WHERE not_fully_idle = 1")
			_ = rwDb.Close()
			activeConvs = 0
			p.ActiveConversations = 0
		}
	}

	// Fetch 5 recent conversations
	var recentConvs []model.ConversationPreview
	rows, err := db.Query(`
		SELECT conversation_id, title, step_count, last_modified_time, not_fully_idle
		FROM conversation_summaries
		ORDER BY last_modified_time DESC
		LIMIT 5
	`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var conv model.ConversationPreview
			var modStr string
			var notIdle int
			if err := rows.Scan(&conv.ID, &conv.Title, &conv.StepCount, &modStr, &notIdle); err == nil {
				conv.Active = notIdle == 1
				if t, err := time.Parse(time.RFC3339, modStr); err == nil {
					conv.LastModified = t
				}
				recentConvs = append(recentConvs, conv)
			}
		}
	}
	p.RecentConversations = recentConvs

	// Save to in-memory cache
	c.mu.Lock()
	c.dbCache[p.ID] = dbCacheEntry{
		modTime:             stat.ModTime(),
		totalConversations:  totalConvs,
		totalSteps:          totalSteps,
		activeConversations: activeConvs,
		lastActive:          p.LastActive,
		estTokens24h:        p.EstTokens24h,
		recentConversations: recentConvs,
	}
	c.mu.Unlock()
}

func (c *Collector) collectLogMetrics(p *model.Profile) {
	logPath := filepath.Join(p.AppDataDir, "cli.log")
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		return
	}

	file, err := os.Open(logPath)
	if err != nil {
		return
	}
	defer file.Close()

	// Read last 2000 lines to ensure accurate hourly calculation under heavy usage
	lines := tailFile(file, 2000)

	// Save last 50 lines for Live Log Viewer
	if len(lines) > 50 {
		p.RecentLogLines = lines[len(lines)-50:]
	} else {
		p.RecentLogLines = lines
	}

	now := time.Now()
	oneMinAgo := now.Add(-1 * time.Minute)
	fiveMinAgo := now.Add(-5 * time.Minute)
	oneDayAgo := now.Add(-24 * time.Hour)
	hourStart := now.Truncate(time.Hour)

	currentYear := now.Year()

	c.mu.RLock()
	resetTime := c.lastManualReset[strings.ToLower(p.ID)]
	if r, ok := c.lastManualReset[strings.ToLower(p.Alias)]; ok && r.After(resetTime) {
		resetTime = r
	}
	if p.Email != "" {
		if r, ok := c.lastManualReset[strings.ToLower(p.Email)]; ok && r.After(resetTime) {
			resetTime = r
		}
	}
	if starTime, ok := c.lastManualReset["*"]; ok && starTime.After(resetTime) {
		resetTime = starTime
	}
	c.mu.RUnlock()

	p.HourlyRequests = 0
	p.HourlyRateLimits = 0

	for _, line := range lines {
		// Check rate limits / 429 (only Warning 'W' or Error 'E' lines; never match timestamps or INFO 'I' lines)
		if len(line) > 0 && (line[0] == 'E' || line[0] == 'W') {
			var msg string
			if idx := strings.Index(line, "] "); idx != -1 {
				msg = line[idx+2:]
			} else {
				msg = line
			}

			if rateLimitPattern.MatchString(msg) {
				if match := timePrefixPattern.FindStringSubmatch(line); len(match) > 1 {
					tStr := fmt.Sprintf("%d%s", currentYear, match[1])
					if logTime, err := time.ParseInLocation("20060102 15:04:05", tStr, time.Local); err == nil {
						// Year rollover protection
						if logTime.After(now.Add(5 * time.Minute)) {
							logTime = logTime.AddDate(-1, 0, 0)
						}
						if !logTime.Before(hourStart) {
							p.HourlyRateLimits++
						}
						// Ignore 429 logged before or at manual reset
						if !resetTime.IsZero() && !logTime.After(resetTime) {
							continue
						}
						if logTime.After(now.Add(-3 * time.Minute)) {
							p.InCooldown = true
							p.CooldownUntil = logTime.Add(3 * time.Minute)
							p.StatusReason = "Rate limit detected (429/ResourceExhausted)"
						}
					}
				}
			}
		}

		// Count requests across sliding windows
		if reqPattern.MatchString(line) {
			if match := timePrefixPattern.FindStringSubmatch(line); len(match) > 1 {
				tStr := fmt.Sprintf("%d%s", currentYear, match[1])
				if logTime, err := time.ParseInLocation("20060102 15:04:05", tStr, time.Local); err == nil {
					// Year rollover protection
					if logTime.After(now.Add(5 * time.Minute)) {
						logTime = logTime.AddDate(-1, 0, 0)
					}
					if !logTime.Before(hourStart) {
						p.HourlyRequests++
					}
					if logTime.After(oneMinAgo) {
						p.RPM++
					}
					if logTime.After(fiveMinAgo) {
						p.RecentRequests5m++
					}
					if logTime.After(oneDayAgo) {
						p.RPD++
					}
				}
			}
		}
	}
}

func (c *Collector) evaluateStatus(p *model.Profile) {
	if p.InCooldown && time.Now().Before(p.CooldownUntil) {
		p.Status = model.StatusCooldown
		remaining := time.Until(p.CooldownUntil).Round(time.Second)
		p.StatusReason = fmt.Sprintf("Cooldown: %s remaining", remaining)
		return
	}
	if p.InCooldown && time.Now().After(p.CooldownUntil) {
		p.InCooldown = false
	}

	if p.IsRunning {
		p.Status = model.StatusActive
		p.StatusReason = fmt.Sprintf("Running (PID: %v)", p.RunningPIDs)
		return
	}

	if p.ActiveConversations > 0 {
		if p.IsRunning || (!p.LastActive.IsZero() && time.Since(p.LastActive) < 2*time.Minute) {
			p.Status = model.StatusActive
			p.StatusReason = fmt.Sprintf("%d active conversation(s)", p.ActiveConversations)
			return
		}
	}

	if p.Email == "" {
		p.Status = model.StatusError
		p.StatusReason = "OAuth Token not found / not logged in"
		return
	}

	p.Status = model.StatusIdle
	p.StatusReason = "Ready / Standby"
}

func tailFile(r io.ReadSeeker, maxLines int) []string {
	const maxTailBytes = 1024 * 1024 // 1 MB buffer for high throughput log history
	size, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return nil
	}

	seekOffset := int64(0)
	skipFirst := false
	if size > maxTailBytes {
		seekOffset = size - maxTailBytes
		skipFirst = true
	}

	if _, err := r.Seek(seekOffset, io.SeekStart); err != nil {
		return nil
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	if skipFirst && scanner.Scan() {
		// Discard potentially partial line at window boundary
	}

	var ring []string
	for scanner.Scan() {
		ring = append(ring, scanner.Text())
		if len(ring) > maxLines {
			ring = ring[1:]
		}
	}
	return ring
}

func formatResetDuration(resetTimeStr string) string {
	if resetTimeStr == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, resetTimeStr)
	if err != nil {
		return ""
	}
	dur := time.Until(t)
	if dur <= 0 {
		return "refreshing"
	}
	hours := int(dur.Hours())
	mins := int(dur.Minutes()) % 60
	if hours > 0 {
		return fmt.Sprintf("%dh %02dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

func getAccessTokenForProfile(p *model.Profile) string {
	realHome := discovery.GetRealHome()

	// 1. Check ~/.cli-proxy-api/antigravity-<email>.json
	if p.Email != "" {
		proxyTokenPath := filepath.Join(realHome, ".cli-proxy-api", fmt.Sprintf("antigravity-%s.json", p.Email))
		if data, err := os.ReadFile(proxyTokenPath); err == nil {
			var raw struct {
				AccessToken string `json:"access_token"`
			}
			if err := json.Unmarshal(data, &raw); err == nil && raw.AccessToken != "" {
				return raw.AccessToken
			}
		}
	}

	// 2. Check token in appDataDir
	appTokenPath := filepath.Join(p.AppDataDir, "antigravity-oauth-token")
	if data, err := os.ReadFile(appTokenPath); err == nil {
		var raw struct {
			Token struct {
				AccessToken string `json:"access_token"`
			} `json:"token"`
			AccessToken string `json:"access_token"`
		}
		if err := json.Unmarshal(data, &raw); err == nil {
			if raw.Token.AccessToken != "" {
				return raw.Token.AccessToken
			}
			if raw.AccessToken != "" {
				return raw.AccessToken
			}
		}
	}

	return ""
}

type cloudCodeQuotaResponse struct {
	Groups []struct {
		DisplayName string `json:"displayName"`
		Buckets     []struct {
			BucketID          string  `json:"bucketId"`
			DisplayName       string  `json:"displayName"`
			Window            string  `json:"window"`
			ResetTime         string  `json:"resetTime"`
			RemainingFraction float64 `json:"remainingFraction"`
		} `json:"buckets"`
	} `json:"groups"`
}

func (c *Collector) fetchRealUserQuota(p *model.Profile) {
	// Check cache (TTL 45s)
	c.mu.RLock()
	cached, hasCache := c.quotaCache[p.ID]
	c.mu.RUnlock()

	if hasCache && time.Since(cached.fetchedAt) < 45*time.Second {
		p.FiveHourPercent = cached.fiveHourPct
		p.FiveHourReset = cached.fiveHourReset
		p.WeeklyPercent = cached.weeklyPct
		p.WeeklyReset = cached.weeklyReset
		return
	}

	token := getAccessTokenForProfile(p)
	if token == "" {
		return
	}

	req, err := http.NewRequest("POST", "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary", bytes.NewBufferString("{}"))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "antigravity")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Use stale cache if request fails
		if hasCache {
			p.FiveHourPercent = cached.fiveHourPct
			p.FiveHourReset = cached.fiveHourReset
			p.WeeklyPercent = cached.weeklyPct
			p.WeeklyReset = cached.weeklyReset
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if hasCache {
			p.FiveHourPercent = cached.fiveHourPct
			p.FiveHourReset = cached.fiveHourReset
			p.WeeklyPercent = cached.weeklyPct
			p.WeeklyReset = cached.weeklyReset
		}
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}

	var quotaResp cloudCodeQuotaResponse
	if err := json.Unmarshal(body, &quotaResp); err != nil {
		return
	}

	var fiveHourPct, weeklyPct float64
	var fiveHourReset, weeklyReset string
	found5h, foundWeekly := false, false

	for _, g := range quotaResp.Groups {
		for _, b := range g.Buckets {
			if b.BucketID == "gemini-5h" || (strings.Contains(b.BucketID, "5h") && !found5h) {
				fiveHourPct = b.RemainingFraction * 100.0
				fiveHourReset = formatResetDuration(b.ResetTime)
				found5h = true
			}
			if b.BucketID == "gemini-weekly" || (strings.Contains(b.BucketID, "weekly") && !foundWeekly) {
				weeklyPct = b.RemainingFraction * 100.0
				weeklyReset = formatResetDuration(b.ResetTime)
				foundWeekly = true
			}
		}
	}

	p.FiveHourPercent = fiveHourPct
	p.FiveHourReset = fiveHourReset
	p.WeeklyPercent = weeklyPct
	p.WeeklyReset = weeklyReset

	// Cache result
	c.mu.Lock()
	c.quotaCache[p.ID] = quotaCacheEntry{
		fetchedAt:     time.Now(),
		fiveHourPct:   fiveHourPct,
		fiveHourReset: fiveHourReset,
		weeklyPct:     weeklyPct,
		weeklyReset:   weeklyReset,
	}
	c.mu.Unlock()
}
