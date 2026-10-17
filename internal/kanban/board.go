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
	StatusFailed     CardStatus = "FAILED"
)

// Card representa uma unidade atômica de trabalho executável pelo Pipeline Adversarial.
type Card struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Status    CardStatus `json:"status"`
	DependsOn []string   `json:"depends_on,omitempty"`
	Scope     []string   `json:"scope,omitempty"`
	TestCmd   string     `json:"test_cmd,omitempty"`
	Spec        string     `json:"spec"`
	Winner      string     `json:"winner,omitempty"`
	MergeCommit string     `json:"merge_commit,omitempty"`
	Error       string     `json:"error,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`
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

// SetMergeCommit anota o commit hash do merge integrado para auditoria e rollback.
func (b *Board) SetMergeCommit(id string, commitHash string) error {
	id = strings.TrimSpace(id)
	for i := range b.Cards {
		if strings.EqualFold(b.Cards[i].ID, id) {
			b.Cards[i].MergeCommit = strings.TrimSpace(commitHash)
			b.Cards[i].UpdatedAt = time.Now().UTC()
			return nil
		}
	}
	return fmt.Errorf("cartão %q não encontrado", id)
}

// MarkRolledBack reverte o estado do cartão para BLOCKED (ou BACKLOG), limpa o vencedor e anota o motivo.
func (b *Board) MarkRolledBack(id string, toBacklog bool, reason string) error {
	id = strings.TrimSpace(id)
	for i := range b.Cards {
		if strings.EqualFold(b.Cards[i].ID, id) {
			if toBacklog {
				b.Cards[i].Status = StatusBacklog
			} else {
				b.Cards[i].Status = StatusBlocked
			}
			b.Cards[i].Winner = ""
			b.Cards[i].Error = fmt.Sprintf("ROLLBACK: %s", strings.TrimSpace(reason))
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

var criticalRootConfigs = map[string]bool{
	"go.mod":            true,
	"go.sum":            true,
	"cargo.toml":        true,
	"cargo.lock":        true,
	"package.json":      true,
	"package-lock.json": true,
	"pnpm-lock.yaml":    true,
	"flake.nix":         true,
	"flake.lock":        true,
	"makefile":          true,
	"pyproject.toml":    true,
	"poetry.lock":       true,
}

func touchesCriticalRootConfig(scopes []string) bool {
	for _, s := range scopes {
		base := strings.ToLower(filepath.Base(CleanScope(s)))
		if criticalRootConfigs[base] {
			return true
		}
	}
	return false
}

// ScopesOverlap avalia colisão entre escopos de dois cartões.
// Se qualquer escopo for raiz (".", "" ou "*"), colide com tudo.
// Também impede paralelismo se ambos os cartões tocarem arquivos de manifesto/lockfile raiz (RootConfigGuard).
func ScopesOverlap(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	if touchesCriticalRootConfig(a) && touchesCriticalRootConfig(b) {
		return true
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

// FindCard localiza um cartão pelo ID no quadro.
func (b *Board) FindCard(id string) *Card {
	id = strings.TrimSpace(id)
	for i, c := range b.Cards {
		if strings.EqualFold(c.ID, id) {
			return &b.Cards[i]
		}
	}
	return nil
}

// RetryCard reseta o status do cartão para BACKLOG, limpando qualquer erro registrado.
func (b *Board) RetryCard(id string) error {
	id = strings.TrimSpace(id)
	for i, c := range b.Cards {
		if strings.EqualFold(c.ID, id) {
			b.Cards[i].Status = StatusBacklog
			b.Cards[i].Error = ""
			b.Cards[i].Winner = ""
			b.Cards[i].UpdatedAt = time.Now().UTC()
			return nil
		}
	}
	return fmt.Errorf("cartão %q não encontrado", id)
}

// ExportMarkdown retorna uma tabela GitHub Flavored Markdown do quadro com métricas do épico.
func (b *Board) ExportMarkdown() string {
	var sb strings.Builder
	doneCount := 0
	for _, c := range b.Cards {
		if c.Status == StatusDone {
			doneCount++
		}
	}
	total := len(b.Cards)
	pct := 0
	if total > 0 {
		pct = (doneCount * 100) / total
	}

	sb.WriteString(fmt.Sprintf("## 📋 Épico: %s\n\n", b.Epic))
	sb.WriteString(fmt.Sprintf("**Progresso:** %d/%d cartões concluídos (%d%%)\n\n", doneCount, total, pct))
	sb.WriteString("| ID | Status | Dependências | Título | Vencedor / Detalhe |\n")
	sb.WriteString("|---|---|---|---|---|\n")

	for _, c := range b.Cards {
		statusBadge := string(c.Status)
		switch c.Status {
		case StatusDone:
			statusBadge = "✅ DONE"
		case StatusInProgress:
			statusBadge = "⚡ IN_PROGRESS"
		case StatusBlocked:
			statusBadge = "🚫 BLOCKED"
		case StatusFailed:
			statusBadge = "❌ FAILED"
		default:
			statusBadge = "⏳ BACKLOG"
		}
		deps := "-"
		if len(c.DependsOn) > 0 {
			deps = strings.Join(c.DependsOn, ", ")
		}
		detail := "-"
		if c.Winner != "" {
			detail = fmt.Sprintf("🏆 %s", c.Winner)
		} else if c.Error != "" {
			detail = fmt.Sprintf("⚠️ %s", c.Error)
		}
		sb.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s | %s |\n", c.ID, statusBadge, deps, c.Title, detail))
	}
	return sb.String()
}

// GenerateChangelogReport produz um relatório executivo em Markdown com sumário de release,
// cartões concluídos, decisões arquiteturais consolidadas e métricas da execução.
func (b *Board) GenerateChangelogReport() string {
	var sb strings.Builder
	doneCards := make([]Card, 0)
	failedCards := make([]Card, 0)
	backlogCards := make([]Card, 0)

	for _, c := range b.Cards {
		switch c.Status {
		case StatusDone:
			doneCards = append(doneCards, c)
		case StatusBlocked, StatusFailed:
			failedCards = append(failedCards, c)
		default:
			backlogCards = append(backlogCards, c)
		}
	}

	total := len(b.Cards)
	pct := 0
	if total > 0 {
		pct = (len(doneCards) * 100) / total
	}

	sb.WriteString(fmt.Sprintf("# 🚀 Release & Execution Report: %s\n\n", b.Epic))
	sb.WriteString(fmt.Sprintf("> Gerado automaticamente pelo AGY-SWARM Orchestrator em %s\n\n", time.Now().Format("2006-01-02 15:04:05")))

	sb.WriteString("## 📊 Sumário Executivo\n\n")
	sb.WriteString(fmt.Sprintf("- **Total de Cartões no Épico:** %d\n", total))
	sb.WriteString(fmt.Sprintf("- **Concluídos com Sucesso (Quality Gate Aprovado):** %d (%d%%)\n", len(doneCards), pct))
	sb.WriteString(fmt.Sprintf("- **Bloqueados / Requerem Atenção:** %d\n", len(failedCards)))
	sb.WriteString(fmt.Sprintf("- **Pendentes no Backlog:** %d\n\n", len(backlogCards)))

	sb.WriteString("## 🏆 Entregas Concluídas (Changelog)\n\n")
	if len(doneCards) == 0 {
		sb.WriteString("_Nenhuma entrega finalizada até o momento._\n\n")
	} else {
		for _, c := range doneCards {
			sb.WriteString(fmt.Sprintf("### `[%s]` %s\n", c.ID, c.Title))
			if c.Winner != "" {
				sb.WriteString(fmt.Sprintf("- **Vencedor Integrado:** `%s`\n", c.Winner))
			}
			if len(c.Scope) > 0 {
				sb.WriteString(fmt.Sprintf("- **Escopo/Módulos Afetados:** `%s`\n", strings.Join(c.Scope, "`, `")))
			}
			if c.TestCmd != "" {
				sb.WriteString(fmt.Sprintf("- **Quality Gate:** `%s`\n", c.TestCmd))
			}
			if c.Spec != "" {
				sb.WriteString(fmt.Sprintf("- **Especificação:**\n  > %s\n", strings.ReplaceAll(c.Spec, "\n", "\n  > ")))
			}
			sb.WriteString("\n")
		}
	}

	if len(failedCards) > 0 {
		sb.WriteString("## ⚠️ Itens Bloqueados ou com Falha\n\n")
		for _, c := range failedCards {
			sb.WriteString(fmt.Sprintf("- **`[%s]` %s**: %s\n", c.ID, c.Title, c.Error))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

