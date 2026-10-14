package multiplexer

import "github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"

// PaneNotifier define a interface segregada (ISP) para envio de comandos e atualização de status em panes.
// Usada primariamente por observadores (Watcher) e agentes sem necessidade de acoplamento ao ciclo de vida do terminal.
type PaneNotifier interface {
	UpdatePaneStatus(role config.RoleType, status string, color string)
	UpdatePaneWithQuota(role config.RoleType, status string, color string, weeklyPct, fiveHourPct float64)
	SendLiteral(role config.RoleType, text string) error
	SendControl(role config.RoleType, key string) error
	SendKeys(role config.RoleType, text string) error
}

// Multiplexer define o contrato desacoplado para orquestração de terminais, sessões, splits e layouts.
type Multiplexer interface {
	SessionExists() bool
	PaneExists(role config.RoleType) bool
	KillSession() error
	BuildLayout(roles []config.RoleConfig) error
	SetupHUD()
	UpdateStatusBar(clusterInfo string)
	IsPaneAlive(role config.RoleType) (bool, error)
	PaneNotifier
}

