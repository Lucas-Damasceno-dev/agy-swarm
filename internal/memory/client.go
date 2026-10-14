package memory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
)

// Message representa uma mensagem pendente no mailbox do ai-memory.
type Message struct {
	ID          string `json:"id"`
	FromProject string `json:"from_project"`
	ToProject   string `json:"to_project"`
	Body        string `json:"body"`
	CreatedAt   string `json:"created_at"`
}

// AuditEntry representa um registro no log .swarm/debate-<id>.jsonl.
type AuditEntry struct {
	Timestamp   string `json:"timestamp"`
	FromProject string `json:"from_project"`
	ToProject   string `json:"to_project"`
	Role        string `json:"role"`
	Body        string `json:"body"`
	Type        string `json:"type"` // "debate", "steering", "delegate", "merge"
}

// Client provê acesso às operações do ai-memory.
type Client struct {
	DataDir string
}

// NewClient inicializa o cliente de memória.
func NewClient() *Client {
	return &Client{}
}

// EnsureProjects inicializa todos os projetos de mailbox dos agentes no ai-memory.
func (c *Client) EnsureProjects(roles []config.RoleConfig) error {
	for _, r := range roles {
		body := fmt.Sprintf("# Swarm Agent: %s (%s)\n\nPerfil: %s\nFunção: %s\n", r.Name, r.Icon, r.Profile, r.Description)
		cmd := exec.Command("ai-memory", "write-page", "--project", r.Project, "--path", "index.md", "--body", body)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("falha ao registrar projeto %s no ai-memory: %w", r.Project, err)
		}
	}
	return nil
}

// ListPendingMessages resgata as mensagens pendentes na caixa de entrada de um projeto.
func (c *Client) ListPendingMessages(project string) ([]Message, error) {
	cmd := exec.Command("ai-memory", "message", "list", "--project", project, "--json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("erro ao listar mensagens para %s: %s (%w)", project, stderr.String(), err)
	}

	outStr := stdout.String()
	// Localiza início do array JSON caso haja logs prévios
	idx := strings.Index(outStr, "[")
	if idx == -1 {
		return nil, nil
	}

	var msgs []Message
	if err := json.Unmarshal([]byte(outStr[idx:]), &msgs); err != nil {
		return nil, fmt.Errorf("falha ao decodificar JSON de mensagens: %w", err)
	}
	return msgs, nil
}

// PopMessage consome e desempilha a mensagem mais antiga do projeto.
func (c *Client) PopMessage(project string) (*Message, error) {
	cmd := exec.Command("ai-memory", "message", "pop", "--project", project)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("erro ao fazer pop em %s: %s (%w)", project, stderr.String(), err)
	}

	out := stdout.String()
	if strings.Contains(out, "null") || len(strings.TrimSpace(out)) == 0 {
		return nil, nil
	}

	return &Message{
		ToProject: project,
		Body:      out,
	}, nil
}

// SendMessage envia uma mensagem direta para a caixa de outro projeto no ai-memory.
func (c *Client) SendMessage(fromProject, toProject, body string) error {
	cmd := exec.Command("ai-memory", "message", "send", "--project", fromProject, "--to-project", toProject, "--body", body)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("falha ao enviar mensagem de %s para %s: %s (%w)", fromProject, toProject, stderr.String(), err)
	}
	return nil
}

// WriteWikiPage grava uma página centralizada na wiki do ai-memory.
func (c *Client) WriteWikiPage(project, path, body string) error {
	cmd := exec.Command("ai-memory", "write-page", "--project", project, "--path", path, "--body", body)
	return cmd.Run()
}

// AppendAuditLog grava uma linha no ledger de auditoria .swarm/debate-<id>.jsonl.
func (c *Client) AppendAuditLog(workdir, debateID string, entry AuditEntry) error {
	dir := filepath.Join(workdir, ".swarm")
	_ = os.MkdirAll(dir, 0755)

	filePath := filepath.Join(dir, fmt.Sprintf("debate-%s.jsonl", debateID))
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}
