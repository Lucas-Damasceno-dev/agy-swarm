package kanban

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CardStatus define o estado de cada cartão no Kanban Swarm.
type CardStatus string

const (
	StatusBacklog    CardStatus = "BACKLOG"
	StatusInProgress CardStatus = "IN_PROGRESS"
	StatusDone       CardStatus = "DONE"
	StatusBlocked    CardStatus = "BLOCKED"
)

// Card representa uma unidade atômica de trabalho executável pelo Pipeline Adversarial.
type Card struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Status    CardStatus `json:"status"`
	DependsOn []string   `json:"depends_on,omitempty"`
	Scope     []string   `json:"scope,omitempty"`
	TestCmd   string     `json:"test_cmd,omitempty"`
	Spec      string     `json:"spec"`
	Winner    string     `json:"winner,omitempty"`
	Error     string     `json:"error,omitempty"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// Board mantém a especificação do épico e o grafo de cartões em execução.
type Board struct {
	Epic      string    `json:"epic"`
	Cards     []Card    `json:"cards"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DefaultBoardPath retorna o caminho padrão para o arquivo kanban.json do repositório.
func DefaultBoardPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".swarm", "kanban.json")
}

// NewBoard inicializa um quadro Kanban com um épico especificado.
func NewBoard(epic string) *Board {
	return &Board{
		Epic:      strings.TrimSpace(epic),
		Cards:     make([]Card, 0),
		UpdatedAt: time.Now().UTC(),
	}
}

// LoadBoard carrega o arquivo de estado do Kanban a partir do disco.
func LoadBoard(path string) (*Board, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var b Board
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("formato json inválido em %s: %w", path, err)
	}

	return &b, nil
}

// Save persiste o estado do quadro no disco de forma atômica e formatada.
func (b *Board) Save(path string) error {
	b.UpdatedAt = time.Now().UTC()

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("falha ao criar diretório %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("falha ao serializar kanban json: %w", err)
	}

	return os.WriteFile(path, append(data, '\n'), 0644)
}

// AddCard insere um novo cartão no quadro, validando unicidade do ID.
func (b *Board) AddCard(c Card) error {
	c.ID = strings.TrimSpace(c.ID)
	if c.ID == "" {
		return errors.New("id do cartão não pode ser vazio")
	}

	for _, existing := range b.Cards {
		if strings.EqualFold(existing.ID, c.ID) {
			return fmt.Errorf("cartão com id %q já existe no quadro", c.ID)
		}
	}

	if c.Status == "" {
		c.Status = StatusBacklog
	}
	c.Title = strings.TrimSpace(c.Title)
	c.Spec = strings.TrimSpace(c.Spec)
	c.UpdatedAt = time.Now().UTC()

	b.Cards = append(b.Cards, c)
	return nil
}

// UpdateCardStatus atualiza o estado, vencedor e possíveis erros de um cartão.
func (b *Board) UpdateCardStatus(id string, status CardStatus, winner string, errMsg string) error {
	id = strings.TrimSpace(id)
	for i := range b.Cards {
		if strings.EqualFold(b.Cards[i].ID, id) {
			b.Cards[i].Status = status
			if winner != "" {
				b.Cards[i].Winner = strings.TrimSpace(winner)
			}
			if errMsg != "" {
				b.Cards[i].Error = strings.TrimSpace(errMsg)
			} else if status == StatusDone {
				b.Cards[i].Error = ""
			}
			b.Cards[i].UpdatedAt = time.Now().UTC()
			return nil
		}
	}
	return fmt.Errorf("cartão %q não encontrado", id)
}

// NextEligible localiza o primeiro cartão em BACKLOG cujas dependências foram concluídas (DONE).
func (b *Board) NextEligible() *Card {
	doneMap := make(map[string]bool)
	for _, c := range b.Cards {
		if c.Status == StatusDone {
			doneMap[strings.ToUpper(c.ID)] = true
		}
	}

	for _, c := range b.Cards {
		if c.Status != StatusBacklog {
			continue
		}

		depsMet := true
		for _, dep := range c.DependsOn {
			cleanDep := strings.ToUpper(strings.TrimSpace(dep))
			if cleanDep != "" && !doneMap[cleanDep] {
				depsMet = false
				break
			}
		}

		if depsMet {
			match := c
			return &match
		}
	}

	return nil
}

// CleanScope normaliza o caminho do módulo removendo redundâncias de diretório.
func CleanScope(s string) string {
	s = strings.TrimSpace(filepath.ToSlash(filepath.Clean(s)))
	s = strings.TrimPrefix(s, "./")
	return strings.Trim(s, "/")
}

// ScopesOverlap avalia colisão entre escopos de dois cartões.
// Se qualquer escopo for raiz (".", "" ou "*"), colide com tudo.
func ScopesOverlap(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	for _, rawA := range a {
		ca := CleanScope(rawA)
		for _, rawB := range b {
			cb := CleanScope(rawB)
			if ca == "" || ca == "." || ca == "*" || cb == "" || cb == "." || cb == "*" {
				return true
			}
			if ca == cb || strings.HasPrefix(ca, cb+"/") || strings.HasPrefix(cb, ca+"/") {
				return true
			}
		}
	}
	return false
}

// NextEligibleWave retorna todos os cartões em BACKLOG cujas dependências foram atendidas e sem colisão de escopo.
func (b *Board) NextEligibleWave() []Card {
	doneMap := make(map[string]bool)
	for _, c := range b.Cards {
		if c.Status == StatusDone {
			doneMap[strings.ToUpper(c.ID)] = true
		}
	}

	var wave []Card
	for _, c := range b.Cards {
		if c.Status != StatusBacklog {
			continue
		}

		depsMet := true
		for _, dep := range c.DependsOn {
			cleanDep := strings.ToUpper(strings.TrimSpace(dep))
			if cleanDep != "" && !doneMap[cleanDep] {
				depsMet = false
				break
			}
		}

		if !depsMet {
			continue
		}

		conflict := false
		for _, selected := range wave {
			if ScopesOverlap(selected.Scope, c.Scope) {
				conflict = true
				break
			}
		}
		if !conflict {
			wave = append(wave, c)
		}
	}

	return wave
}

// FormatTable retorna uma representação textual visual e compacta do quadro.
func (b *Board) FormatTable() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📋 KANBAN SWARM // ÉPICO: %s\n", b.Epic))
	sb.WriteString(strings.Repeat("─", 78) + "\n")
	sb.WriteString(fmt.Sprintf("%-10s %-12s %-14s %s\n", "ID", "STATUS", "DEPENDS", "TÍTULO"))
	sb.WriteString(strings.Repeat("─", 78) + "\n")

	if len(b.Cards) == 0 {
		sb.WriteString("  (Nenhum cartão cadastrado no quadro. Use 'agy-swarm kanban add' para criar).\n")
		return sb.String()
	}

	for _, c := range b.Cards {
		statusIcon := "⏳"
		switch c.Status {
		case StatusDone:
			statusIcon = "✅"
		case StatusInProgress:
			statusIcon = "⚡"
		case StatusBlocked:
			statusIcon = "🚫"
		}

		deps := "-"
		if len(c.DependsOn) > 0 {
			deps = strings.Join(c.DependsOn, ",")
		}

		statusStr := fmt.Sprintf("%s %s", statusIcon, c.Status)
		title := c.Title
		if len(title) > 36 {
			title = title[:33] + "..."
		}

		sb.WriteString(fmt.Sprintf("%-10s %-14s %-14s %s\n", c.ID, statusStr, deps, title))
		if c.Error != "" {
			sb.WriteString(fmt.Sprintf("   ↳ ⚠️ %s\n", c.Error))
		}
		if c.Winner != "" && c.Status == StatusDone {
			sb.WriteString(fmt.Sprintf("   ↳ 🏆 Integrado via: %s\n", c.Winner))
		}
	}
	sb.WriteString(strings.Repeat("─", 78) + "\n")
	return sb.String()
}
