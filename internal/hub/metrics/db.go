package metrics

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/discovery"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func NewStore() (*Store, error) {
	realHome := discovery.GetRealHome()
	cfgDir := filepath.Join(realHome, ".config/agy-hub")
	_ = os.MkdirAll(cfgDir, 0755)
	return NewStoreWithPath(filepath.Join(cfgDir, "metrics.db"))
}

func NewStoreWithPath(dbPath string) (*Store, error) {
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0755)
	}

	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_timeout=5000", dbPath))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *Store) initSchema() error {
	pragmas := `
	PRAGMA journal_mode = WAL;
	PRAGMA busy_timeout = 5000;
	`
	if _, err := s.db.Exec(pragmas); err != nil {
		return err
	}

	schema := `
	CREATE TABLE IF NOT EXISTS hourly_metrics (
		timestamp INTEGER NOT NULL,
		profile_id TEXT NOT NULL,
		requests INTEGER DEFAULT 0,
		steps INTEGER DEFAULT 0,
		tokens INTEGER DEFAULT 0,
		rate_limits INTEGER DEFAULT 0,
		PRIMARY KEY(timestamp, profile_id)
	);

	CREATE TABLE IF NOT EXISTS execution_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		executed_at INTEGER NOT NULL,
		profile_id TEXT NOT NULL,
		card_id TEXT,
		role TEXT,
		prompt_preview TEXT,
		status TEXT NOT NULL,
		duration_ms INTEGER DEFAULT 0,
		diff_bytes INTEGER DEFAULT 0,
		error_msg TEXT
	);
	`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	// Migrações tolerantes para tabelas legadas já existentes
	_, _ = s.db.Exec(`ALTER TABLE execution_history ADD COLUMN card_id TEXT;`)
	_, _ = s.db.Exec(`ALTER TABLE execution_history ADD COLUMN role TEXT;`)
	_, _ = s.db.Exec(`ALTER TABLE execution_history ADD COLUMN diff_bytes INTEGER DEFAULT 0;`)
	return nil
}

// RecordExecution registra o resultado de uma execução de agente/worktree
func (s *Store) RecordExecution(profileID, cardID, role, promptPreview, status string, durationMs int64, diffBytes int, errorMsg string) error {
	query := `
	INSERT INTO execution_history (executed_at, profile_id, card_id, role, prompt_preview, status, duration_ms, diff_bytes, error_msg)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.Exec(query, time.Now().Unix(), profileID, cardID, role, promptPreview, status, durationMs, diffBytes, errorMsg)
	return err
}

// LeaderboardEntry agrupa métricas consolidadas de desempenho e taxa de vitória por conta
type LeaderboardEntry struct {
	ProfileID      string  `json:"profile_id"`
	TotalRuns      int     `json:"total_runs"`
	Wins           int     `json:"wins"`
	WinRate        float64 `json:"win_rate"`
	AvgDurationMs  int64   `json:"avg_duration_ms"`
	TotalDiffBytes int     `json:"total_diff_bytes"`
}

// GetLeaderboard retorna o ranking de desempenho consolidado das contas
func (s *Store) GetLeaderboard() ([]LeaderboardEntry, error) {
	query := `
	SELECT 
		profile_id,
		COUNT(*) as total_runs,
		SUM(CASE WHEN status IN ('WINNER', 'DONE', 'MERGED') THEN 1 ELSE 0 END) as wins,
		AVG(duration_ms) as avg_duration,
		SUM(diff_bytes) as total_diff
	FROM execution_history
	WHERE profile_id != ''
	GROUP BY profile_id
	ORDER BY wins DESC, total_runs DESC
	`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []LeaderboardEntry
	for rows.Next() {
		var e LeaderboardEntry
		var avgDur sql.NullFloat64
		var totalDiff sql.NullInt64
		if err := rows.Scan(&e.ProfileID, &e.TotalRuns, &e.Wins, &avgDur, &totalDiff); err == nil {
			if e.TotalRuns > 0 {
				e.WinRate = (float64(e.Wins) / float64(e.TotalRuns)) * 100.0
			}
			if avgDur.Valid {
				e.AvgDurationMs = int64(avgDur.Float64)
			}
			if totalDiff.Valid {
				e.TotalDiffBytes = int(totalDiff.Int64)
			}
			entries = append(entries, e)
		}
	}
	return entries, nil
}


// RecordSnapshot records a metrics point for the current hour
func (s *Store) RecordSnapshot(profileID string, requests, steps, tokens, rateLimits int) error {
	// Truncate to hour
	hourTs := time.Now().Truncate(time.Hour).Unix()

	query := `
	INSERT INTO hourly_metrics (timestamp, profile_id, requests, steps, tokens, rate_limits)
	VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(timestamp, profile_id) DO UPDATE SET
		requests = MAX(hourly_metrics.requests, excluded.requests),
		steps = excluded.steps,
		tokens = excluded.tokens,
		rate_limits = MAX(hourly_metrics.rate_limits, excluded.rate_limits)
	`
	_, err := s.db.Exec(query, hourTs, profileID, requests, steps, tokens, rateLimits)
	return err
}

// GetSparklineData returns 24 integer values for the past 24 hours
func (s *Store) GetSparklineData(profileID string) []int {
	now := time.Now().Truncate(time.Hour)
	result := make([]int, 24)

	hourMap := make(map[int64]int)
	startTs := now.Add(-23 * time.Hour).Unix()

	rows, err := s.db.Query(`
		SELECT timestamp, requests
		FROM hourly_metrics
		WHERE profile_id = ? AND timestamp >= ?
		ORDER BY timestamp ASC
	`, profileID, startTs)
	if err != nil {
		return result
	}
	defer rows.Close()

	for rows.Next() {
		var ts int64
		var reqs int
		if err := rows.Scan(&ts, &reqs); err == nil {
			hourMap[ts] = reqs
		}
	}

	for i := 0; i < 24; i++ {
		t := now.Add(time.Duration(i-23) * time.Hour).Unix()
		result[i] = hourMap[t]
	}

	return result
}

// RenderSparkline converts 24 hourly points into a unicode bar graph
func RenderSparkline(data []int) string {
	bars := []rune{' ', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	maxVal := 0
	for _, v := range data {
		if v > maxVal {
			maxVal = v
		}
	}

	if maxVal == 0 {
		return "                        " // 24 spaces
	}

	runes := make([]rune, len(data))
	for i, v := range data {
		if v == 0 {
			runes[i] = ' '
			continue
		}
		idx := int((float64(v) / float64(maxVal)) * float64(len(bars)-1))
		if idx < 0 {
			idx = 0
		} else if idx >= len(bars) {
			idx = len(bars) - 1
		}
		runes[i] = bars[idx]
	}

	return string(runes)
}
