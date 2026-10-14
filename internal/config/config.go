package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config contém as opções de configuração do enxame agy-swarm.
type Config struct {
	SessionName string
	Workdir     string
	Prompt      string
	Roles       []RoleConfig
	Rounds      int
	SocketPath  string
	DryRun      bool
	Detach      bool
}

// ParseRoles resolve a lista de papéis a partir de uma string de roles ou contagem.
func ParseRoles(rolesArg string, nArg int) ([]RoleConfig, error) {
	allAvailable := AllRoles()
	if rolesArg == "" && nArg <= 0 {
		return DefaultRoles(), nil
	}

	if rolesArg != "" {
		parts := strings.Split(rolesArg, ",")
		selected := []RoleConfig{allAvailable[0]} // Maestro sempre presente no pane 0
		seen := map[RoleType]bool{RoleLead: true}

		for _, p := range parts {
			clean := RoleType(strings.ToLower(strings.TrimSpace(p)))
			if clean == RoleLead {
				continue
			}
			found := false
			for _, r := range allAvailable {
				if r.Type == clean && !seen[r.Type] {
					selected = append(selected, r)
					seen[r.Type] = true
					found = true
					break
				}
			}
			if !found && clean != "" {
				return nil, fmt.Errorf("papel desconhecido ou duplicado: %s", clean)
			}
		}
		return selected, nil
	}

	if nArg > 0 {
		if nArg < 1 || nArg > len(allAvailable) {
			return nil, fmt.Errorf("número de agentes inválido (%d), deve ser entre 1 e %d", nArg, len(allAvailable))
		}
		return allAvailable[:nArg], nil
	}

	return DefaultRoles(), nil
}

// NewConfig constrói e valida a configuração inicial a partir do ambiente.
func NewConfig(session string, rolesArg string, nArg int, prompt string) (*Config, error) {
	if session == "" {
		session = "agy-swarm"
	}

	roles, err := ParseRoles(rolesArg, nArg)
	if err != nil {
		return nil, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("falha ao obter diretório corrente: %w", err)
	}

	return &Config{
		SessionName: session,
		Workdir:     cwd,
		Prompt:      prompt,
		Roles:       roles,
		Rounds:      3,
		SocketPath:  "/tmp/agy-hub.sock",
	}, nil
}

// WorktreePath retorna o caminho absoluto para o worktree de um papel.
func (c *Config) WorktreePath(role RoleType) string {
	return filepath.Join(c.Workdir, ".worktrees", fmt.Sprintf("worker-%s", role))
}

// BranchName retorna o nome do branch git efêmero de um papel.
func (c *Config) BranchName(role RoleType) string {
	return fmt.Sprintf("swarm/%s", role)
}
