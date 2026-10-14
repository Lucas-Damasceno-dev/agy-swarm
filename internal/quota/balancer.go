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

// CalculateScore aplica a fórmula ponderada de cota semanal (70%) e 5h (30%).
func CalculateScore(weekly, fiveHour float64) float64 {
	score := (weekly * 0.7) + (fiveHour * 0.3)
	// Penalidade crítica se a cota de curto prazo (5h) estiver quase no fim (<20%)
	if fiveHour < 20.0 {
		score -= 50.0
	}
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

// AssignRoles reordena os perfis dos papéis para maximizar a durabilidade do enxame:
// - Maestro recebe a conta com MENOR cota restante (apenas orquestra/planeja)
// - Hacker/Coder recebe a conta com MAIOR cota restante (gera código intensivamente)
// - Arquiteto, Segurança e QA recebem as contas intermediárias.
func (b *Balancer) AssignRoles(roles []config.RoleConfig) ([]config.RoleConfig, error) {
	if !b.Client.IsAvailable() {
		return roles, nil // Fallback gracioso: mantém os perfis default
	}

	state, err := b.Client.GetClusterState()
	if err != nil || len(state.Profiles) == 0 {
		return roles, nil
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
		return roles, nil // Não há contas saudáveis suficientes para o mapeamento
	}

	// Ordena as contas do menor score (menor cota) para o maior score (maior cota)
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].Score < scores[j].Score
	})

	// Hierarquia de consumo de tokens:
	// 0: Maestro (menor consumo) -> scores[0] (menor cota)
	// 1: Arquiteto (médio) -> scores[1]
	// 2: Segurança (médio-alto) -> scores[2]
	// 3: QA (alto) -> scores[3]
	// 4: Hacker (altíssimo consumo de código) -> scores[len-1] (maior cota)
	reassigned := make([]config.RoleConfig, len(roles))
	copy(reassigned, roles)

	// Mapeia Maestro sempre para a conta de menor score
	for i := range reassigned {
		if reassigned[i].Type == config.RoleLead {
			reassigned[i].Profile = scores[0].ID
			break
		}
	}

	// Mapeia os especialistas ordenando pelo peso de consumo
	specialistOrder := []config.RoleType{
		config.RoleArch,
		config.RoleSec,
		config.RoleQA,
		config.RoleHacker,
	}

	// As contas restantes de scores[1:] estão em ordem crescente de cota
	availableScores := scores[1:]
	for idx, sRole := range specialistOrder {
		if idx >= len(availableScores) {
			break
		}
		for i := range reassigned {
			if reassigned[i].Type == sRole {
				reassigned[i].Profile = availableScores[idx].ID
				break
			}
		}
	}

	return reassigned, nil
}
