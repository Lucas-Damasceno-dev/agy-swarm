package tmux

import "github.com/Lucas-Damasceno-dev/agy-swarm/internal/multiplexer"

// Multiplexer re-exporta a interface de domínio multiplexer.Multiplexer para compatibilidade.
type Multiplexer = multiplexer.Multiplexer

// PaneNotifier re-exporta o contrato segregado de notificação.
type PaneNotifier = multiplexer.PaneNotifier
