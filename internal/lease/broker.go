package lease

import (
	"context"
	"fmt"
	"sort"
	"sync"
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
	ID         string
	AccountID  string
	Role       config.RoleType
	Stage      string
	AcquiredAt time.Time
	Status     LeaseStatus
	Score      float64
}

// AccountProvider interface desacoplada para obter o estado do cluster do agy-hub.
type AccountProvider interface {
	IsAvailable() bool
	GetClusterState() (*agyhub.ClusterState, error)
}

// Broker gerencia o pool elástico de 10 contas com alocação dinâmica e failover em 429.
type Broker struct {
	mu           sync.RWMutex
	client       AccountProvider
	accounts     []string
	activeLeases map[string]*Lease     // accountID -> *Lease
	cooldowns    map[string]time.Time  // accountID -> expiração do cooldown
	leaseCounter int64
}

// NewBroker cria uma nova instância do Lease Broker sobre as 10 contas.
func NewBroker(client AccountProvider) *Broker {
	if client == nil {
		client = agyhub.NewClient("")
	}
	accs := make([]string, len(AllKnownAccounts))
	copy(accs, AllKnownAccounts)

	return &Broker{
		client:       client,
		accounts:     accs,
		activeLeases: make(map[string]*Lease),
		cooldowns:    make(map[string]time.Time),
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
				weekly = p.WeeklyPercent
				fiveHour = p.FiveHourPercent
				daily = p.QuotaPercent
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

	b.cleanExpiredCooldownsLocked()
	scores := b.CalculateAccountScores()

	// Candidatas elegíveis: não em lease ativo e não em cooldown
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
		candidates = append(candidates, candidate{accountID: acc, score: sc})
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("nenhuma conta disponível no pool das 10 contas (todas ocupadas ou em cooldown)")
	}

	// Ordena candidatas por score decrescente (maior cota primeiro)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	// Seleciona a melhor conta
	best := candidates[0]

	b.leaseCounter++
	lease := &Lease{
		ID:         fmt.Sprintf("lease-%d-%s", b.leaseCounter, best.accountID),
		AccountID:  best.accountID,
		Role:       role,
		Stage:      stage,
		AcquiredAt: time.Now(),
		Status:     StatusAcquired,
		Score:      best.score,
	}

	b.activeLeases[best.accountID] = lease
	return lease, nil
}

// Release devolve a conta para o pool.
func (b *Broker) Release(lease *Lease) {
	if lease == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	lease.Status = StatusReleased
	delete(b.activeLeases, lease.AccountID)
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

	lease.Status = StatusCooldown
	b.cooldowns[lease.AccountID] = time.Now().Add(duration)
	delete(b.activeLeases, lease.AccountID)
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
	b.mu.RLock()
	defer b.mu.RUnlock()

	scores := b.CalculateAccountScores()
	now := time.Now()

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
		}

		snaps = append(snaps, snap)
	}

	return snaps
}
