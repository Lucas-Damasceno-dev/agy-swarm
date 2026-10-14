package quota

import (
	"sort"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/agyhub"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
)

// AccountScore armazena os dados de cota e o score calculado para uma conta.
type AccountScore struct {
	ID              string
	Alias           string
	WeeklyPercent   float64
	FiveHourPercent float64
	DailyPercent    int
	Score           float64
}

// CalculateScore aplica a fórmula governada pelo gargalo mais restrito contra 429 (janela de 5h).
// Aplica Hard Cutoff imediato se a cota residual de 5h estiver crítica (<10%) ou semanal (<5%).
func CalculateScore(weekly, fiveHour float64) float64 {
	// Hard Cutoff de segurança contra 429 imediato
	if fiveHour < 10.0 || weekly < 5.0 {
		return 0.0
	}
	// Fórmula multiplicativa: 5h é o gargalo limitante primário; a cota semanal escala o multiplicador (0.5 a 1.0)
	score := fiveHour * (0.5 + 0.5*(weekly/100.0))
	return score
}

// Balancer atribui papéis a contas do Google AI Pro com base nas cotas reais.
type Balancer struct {
	Client *agyhub.Client
}

// NewBalancer cria uma nova instância do balanceador de cotas.
func NewBalancer(client *agyhub.Client) *Balancer {
	if client == nil {
		client = agyhub.NewClient("")
	}
	return &Balancer{Client: client}
}

// ClusterAllocation armazena os papéis balanceados e o pool de contas reservas (hot-standby).
type ClusterAllocation struct {
	Roles       []config.RoleConfig
	StandbyPool []AccountScore
}

// AssignClusterRoles distribui as contas do cluster com priorização inteligente:
// - Maestro recebe a conta com MENOR cota restante (apenas orquestra/planeja)
// - Hacker/Coder recebe a conta com MAIOR cota restante (gera código intensivamente)
// - Especialistas intermediários recebem contas equilibradas
// - Contas excedentes (ex: 5 ativas de 10) formam o pool de Hot-Standby.
func (b *Balancer) AssignClusterRoles(roles []config.RoleConfig) (ClusterAllocation, error) {
	allocation := ClusterAllocation{
		Roles:       roles,
		StandbyPool: make([]AccountScore, 0),
	}

	if !b.Client.IsAvailable() {
		return allocation, nil // Fallback gracioso: mantém os perfis default
	}

	state, err := b.Client.GetClusterState()
	if err != nil || len(state.Profiles) == 0 {
		return allocation, nil
	}

	var scores []AccountScore
	for _, p := range state.Profiles {
		// Ignora contas em quarentena ou erro grave
		if p.Status == "COOLDOWN" || p.Status == "ERROR" {
			continue
		}
		sc := CalculateScore(p.WeeklyPercent, p.FiveHourPercent)
		scores = append(scores, AccountScore{
			ID:              p.ID,
			Alias:           p.Alias,
			WeeklyPercent:   p.WeeklyPercent,
			FiveHourPercent: p.FiveHourPercent,
			DailyPercent:    p.QuotaPercent,
			Score:           sc,
		})
	}

	if len(scores) < len(roles) {
		return allocation, nil // Não há contas saudáveis suficientes para o mapeamento
	}

	// Ordena as contas do menor score (menor cota) para o maior score (maior cota)
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].Score < scores[j].Score
	})

	reassigned := make([]config.RoleConfig, len(roles))
	copy(reassigned, roles)

	usedIndices := make(map[int]bool)

	// 1. Maestro sempre recebe a conta de menor cota disponível (scores[0])
	for i := range reassigned {
		if reassigned[i].Type == config.RoleLead {
			reassigned[i].Profile = scores[0].ID
			usedIndices[0] = true
			break
		}
	}

	// 2. Hacker sempre recebe a conta com a maior cota absoluta do cluster (scores[len-1])
	hackerIdx := len(scores) - 1
	for i := range reassigned {
		if reassigned[i].Type == config.RoleHacker {
			reassigned[i].Profile = scores[hackerIdx].ID
			usedIndices[hackerIdx] = true
			break
		}
	}

	// 3. Distribui as contas intermediárias para os demais papéis
	scoreCursor := 1
	for i := range reassigned {
		if reassigned[i].Type == config.RoleLead || reassigned[i].Type == config.RoleHacker {
			continue
		}
		for scoreCursor < len(scores) && usedIndices[scoreCursor] {
			scoreCursor++
		}
		if scoreCursor < len(scores) {
			reassigned[i].Profile = scores[scoreCursor].ID
			usedIndices[scoreCursor] = true
			scoreCursor++
		}
	}

	// 4. Todas as contas não utilizadas viram pool de reserva Hot-Standby
	for idx, s := range scores {
		if !usedIndices[idx] {
			allocation.StandbyPool = append(allocation.StandbyPool, s)
		}
	}

	allocation.Roles = reassigned
	return allocation, nil
}

// AssignRoles é o wrapper de compatibilidade direta que retorna a lista reordenada de papéis.
func (b *Balancer) AssignRoles(roles []config.RoleConfig) ([]config.RoleConfig, error) {
	alloc, err := b.AssignClusterRoles(roles)
	return alloc.Roles, err
}

// FindBestReserve retorna a melhor conta reserva com maior score para hot-swap.
func FindBestReserve(pool []AccountScore) *AccountScore {
	if len(pool) == 0 {
		return nil
	}
	best := &pool[0]
	for i := 1; i < len(pool); i++ {
		if pool[i].Score > best.Score {
			best = &pool[i]
		}
	}
	return best
}
