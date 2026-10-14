package lease

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/agyhub"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/quota"
)

// AllKnownAccounts lista as 10 contas Google AI Pro suportadas pelo cluster.
var AllKnownAccounts = []string{
	"default",
	"account2",
	"account3",
	"account4",
	"account5",
	"account6",
	"account7",
	"account8",
	"account9",
	"account10",
}

// LeaseStatus define o estado atual de um lease.
type LeaseStatus string

const (
	StatusAcquired LeaseStatus = "ACQUIRED"
	StatusReleased LeaseStatus = "RELEASED"
	StatusCooldown LeaseStatus = "COOLDOWN"
	StatusFailed   LeaseStatus = "FAILED"
)

// Lease representa uma alocação temporária e efêmera de uma conta do pool para uma tarefa.
type Lease struct {
	ID         string          `json:"id"`
	AccountID  string          `json:"account_id"`
	Role       config.RoleType `json:"role"`
	Stage      string          `json:"stage"`
	AcquiredAt time.Time       `json:"acquired_at"`
	Status     LeaseStatus     `json:"status"`
	Score      float64         `json:"score"`
}

// AccountProvider interface desacoplada para obter o estado do cluster do agy-hub.
type AccountProvider interface {
	IsAvailable() bool
	GetClusterState() (*agyhub.ClusterState, error)
}

// Broker gerencia o pool elástico de 10 contas com alocação dinâmica e failover em 429.
type Broker struct {
	mu            sync.RWMutex
	client        AccountProvider
	accounts      []string
	activeLeases  map[string]*Lease     // accountID -> *Lease
	cooldowns     map[string]time.Time  // accountID -> expiração do cooldown
	leaseCounter  int64
	stateFilePath string
}

func defaultStateFilePath() string {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	var baseDir string
	if runtimeDir != "" {
		baseDir = filepath.Join(runtimeDir, "agy-swarm")
	} else {
		baseDir = filepath.Join(os.TempDir(), fmt.Sprintf("agy-swarm-run-%d", os.Getuid()))
	}
	_ = os.MkdirAll(baseDir, 0700)
	return filepath.Join(baseDir, "leases-state.json")
}

// NewBroker cria uma nova instância do Lease Broker sobre as 10 contas.
func NewBroker(client AccountProvider, statePath ...string) *Broker {
	if client == nil {
		client = agyhub.NewClient("")
	}
	accs := make([]string, len(AllKnownAccounts))
	copy(accs, AllKnownAccounts)

	sf := defaultStateFilePath()
	if len(statePath) > 0 {
		sf = statePath[0]
	}

	return &Broker{
		client:        client,
		accounts:      accs,
		activeLeases:  make(map[string]*Lease),
		cooldowns:     make(map[string]time.Time),
		stateFilePath: sf,
	}
}

// SetStateFilePath atualiza o caminho do arquivo de estado (útil para testes isolados).
func (b *Broker) SetStateFilePath(path string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stateFilePath = path
}

type brokerStateFile struct {
	ActiveLeases map[string]*Lease    `json:"active_leases"`
	Cooldowns    map[string]time.Time `json:"cooldowns"`
	LeaseCounter int64                `json:"lease_counter"`
}

func (b *Broker) withFileLock(fn func() error) error {
	if b.stateFilePath == "" {
		return fn()
	}
	dir := filepath.Dir(b.stateFilePath)
	_ = os.MkdirAll(dir, 0700)
	lockPath := filepath.Join(dir, "leases.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fn()
	}
	defer f.Close()

	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	return fn()
}

func (b *Broker) loadStateLocked() {
	if b.stateFilePath == "" {
		return
	}
	data, err := os.ReadFile(b.stateFilePath)
	if err != nil {
		return
	}
	var state brokerStateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return
	}
	if state.ActiveLeases != nil {
		b.activeLeases = state.ActiveLeases
	}
	if state.Cooldowns != nil {
		b.cooldowns = state.Cooldowns
	}
	if state.LeaseCounter > b.leaseCounter {
		b.leaseCounter = state.LeaseCounter
	}
}

func (b *Broker) saveStateLocked() {
	if b.stateFilePath == "" {
		return
	}
	state := brokerStateFile{
		ActiveLeases: b.activeLeases,
		Cooldowns:    b.cooldowns,
		LeaseCounter: b.leaseCounter,
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	tmpFile := fmt.Sprintf("%s.tmp.%d", b.stateFilePath, os.Getpid())
	if err := os.WriteFile(tmpFile, data, 0600); err == nil {
		_ = os.Rename(tmpFile, b.stateFilePath)
	}
}

// AccountStatusSnapshot resume o estado de uma conta para telemetria no dashboard.
type AccountStatusSnapshot struct {
	AccountID       string
	Status          string // "FREE", "BUSY", "COOLDOWN"
	CurrentRole     config.RoleType
	CurrentStage    string
	Score           float64
	WeeklyPercent   float64
	FiveHourPercent float64
}

// CalculateAccountScores calcula a pontuação de saúde das 10 contas disponíveis.
func (b *Broker) CalculateAccountScores() map[string]quota.AccountScore {
	scores := make(map[string]quota.AccountScore)

	var profiles map[string]agyhub.ProfileSummary
	clusterAvailable := b.client.IsAvailable()
	if clusterAvailable {
		if st, err := b.client.GetClusterState(); err == nil {
			profiles = make(map[string]agyhub.ProfileSummary)
			for _, p := range st.Profiles {
				profiles[p.ID] = p
			}
		}
	}

	for _, acc := range b.accounts {
		weekly := 100.0
		fiveHour := 100.0
		daily := 100

		if clusterAvailable && profiles != nil {
			if p, ok := profiles[acc]; ok {
				if p.Status == "COOLDOWN" || p.Status == "ERROR" {
					weekly = 0.0
					fiveHour = 0.0
					daily = 0
				} else {
					weekly = p.WeeklyPercent
					fiveHour = p.FiveHourPercent
					daily = p.QuotaPercent
				}
			} else {
				// Se o cluster respondeu mas não tem dados desta conta, pontuação mínima
				weekly = 0.0
				fiveHour = 0.0
				daily = 0
			}
		}

		sc := quota.CalculateScore(weekly, fiveHour)
		scores[acc] = quota.AccountScore{
			ID:              acc,
			WeeklyPercent:   weekly,
			FiveHourPercent: fiveHour,
			DailyPercent:    daily,
			Score:           sc,
		}
	}

	return scores
}

// Acquire aloca a melhor conta saudável das 10 para o papel e estágio solicitados.
func (b *Broker) Acquire(ctx context.Context, role config.RoleType, stage string) (*Lease, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	var lease *Lease
	var leaseErr error

	_ = b.withFileLock(func() error {
		b.loadStateLocked()
		b.cleanExpiredCooldownsLocked()
		scores := b.CalculateAccountScores()

		// Candidatas elegíveis: não em lease ativo, não em cooldown e com cota positiva
		type candidate struct {
			accountID string
			score     float64
		}
		var candidates []candidate

		for _, acc := range b.accounts {
			if _, busy := b.activeLeases[acc]; busy {
				continue
			}
			if exp, onCooldown := b.cooldowns[acc]; onCooldown && time.Now().Before(exp) {
				continue
			}
			sc := scores[acc].Score
			if sc <= 0 {
				continue
			}
			candidates = append(candidates, candidate{accountID: acc, score: sc})
		}

		if len(candidates) == 0 {
			leaseErr = fmt.Errorf("nenhuma conta disponível no pool das 10 contas (todas ocupadas, em cooldown ou sem cota)")
			return leaseErr
		}

		// Ordena candidatas por score decrescente (maior cota primeiro)
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].score > candidates[j].score
		})

		// Seleciona a melhor conta
		best := candidates[0]

		b.leaseCounter++
		lease = &Lease{
			ID:         fmt.Sprintf("lease-%d-%s", b.leaseCounter, best.accountID),
			AccountID:  best.accountID,
			Role:       role,
			Stage:      stage,
			AcquiredAt: time.Now(),
			Status:     StatusAcquired,
			Score:      best.score,
		}

		b.activeLeases[best.accountID] = lease
		b.saveStateLocked()
		return nil
	})

	if leaseErr != nil {
		return nil, leaseErr
	}
	return lease, nil
}

// Release devolve a conta para o pool.
func (b *Broker) Release(lease *Lease) {
	if lease == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	_ = b.withFileLock(func() error {
		b.loadStateLocked()
		lease.Status = StatusReleased
		delete(b.activeLeases, lease.AccountID)
		b.saveStateLocked()
		return nil
	})
}

// Report429 marca a conta em cooldown temporário e revoga o lease ativo.
func (b *Broker) Report429(lease *Lease, duration time.Duration) {
	if lease == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	if duration <= 0 {
		duration = 5 * time.Minute
	}

	_ = b.withFileLock(func() error {
		b.loadStateLocked()
		lease.Status = StatusCooldown
		b.cooldowns[lease.AccountID] = time.Now().Add(duration)
		delete(b.activeLeases, lease.AccountID)
		b.saveStateLocked()
		return nil
	})
}

// Failover reporta 429 na conta com falha e adquire imediatamente uma nova conta saudável.
func (b *Broker) Failover(ctx context.Context, failedLease *Lease) (*Lease, error) {
	b.Report429(failedLease, 5*time.Minute)
	return b.Acquire(ctx, failedLease.Role, failedLease.Stage)
}

// cleanExpiredCooldownsLocked remove cooldowns cujo tempo já expirou.
func (b *Broker) cleanExpiredCooldownsLocked() {
	now := time.Now()
	for acc, exp := range b.cooldowns {
		if now.After(exp) {
			delete(b.cooldowns, acc)
		}
	}
}

// Snapshots retorna o estado consolidado das 10 contas para exibição no dashboard.
func (b *Broker) Snapshots() []AccountStatusSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()

	_ = b.withFileLock(func() error {
		b.loadStateLocked()
		b.cleanExpiredCooldownsLocked()
		return nil
	})

	scores := b.CalculateAccountScores()
	now := time.Now()

	var profiles map[string]agyhub.ProfileSummary
	if b.client.IsAvailable() {
		if st, err := b.client.GetClusterState(); err == nil {
			profiles = make(map[string]agyhub.ProfileSummary)
			for _, p := range st.Profiles {
				profiles[p.ID] = p
			}
		}
	}

	var snaps []AccountStatusSnapshot
	for _, acc := range b.accounts {
		snap := AccountStatusSnapshot{
			AccountID:       acc,
			Status:          "FREE",
			Score:           scores[acc].Score,
			WeeklyPercent:   scores[acc].WeeklyPercent,
			FiveHourPercent: scores[acc].FiveHourPercent,
		}

		if lease, busy := b.activeLeases[acc]; busy {
			snap.Status = "BUSY"
			snap.CurrentRole = lease.Role
			snap.CurrentStage = lease.Stage
		} else if exp, cd := b.cooldowns[acc]; cd && now.Before(exp) {
			snap.Status = "COOLDOWN"
		} else if profiles != nil {
			if p, ok := profiles[acc]; ok && (p.Status == "COOLDOWN" || p.Status == "ERROR") {
				snap.Status = "COOLDOWN"
			}
		}

		snaps = append(snaps, snap)
	}

	return snaps
}
