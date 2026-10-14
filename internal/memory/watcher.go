package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/multiplexer"
)

// MessageLister define a interface para consulta de mensagens pendentes no mailbox.
type MessageLister interface {
	ListPendingMessages(project string) ([]Message, error)
}

// Watcher monitora os mailboxes dos agentes e acorda os panes correspondentes de forma thread-safe.
type Watcher struct {
	Client     MessageLister
	Notifier   multiplexer.PaneNotifier
	Roles      []config.RoleConfig
	mu         sync.RWMutex
	seenCounts map[config.RoleType]int
	stopChan   chan struct{}
	stopOnce   sync.Once
}

// NewWatcher cria uma nova instância do monitor de mensagens com desacoplamento ISP e thread-safety.
func NewWatcher(client MessageLister, notifier multiplexer.PaneNotifier, roles []config.RoleConfig) *Watcher {
	return &Watcher{
		Client:     client,
		Notifier:   notifier,
		Roles:      roles,
		seenCounts: make(map[config.RoleType]int),
		stopChan:   make(chan struct{}),
	}
}

// Start inicia o loop de monitoramento assíncrono.
func (w *Watcher) Start(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-w.stopChan:
				return
			case <-ticker.C:
				w.pollMailboxes()
			}
		}
	}()
}

// Stop encerra o watcher graciosamente e com garantia de idempotência.
func (w *Watcher) Stop() {
	w.stopOnce.Do(func() {
		close(w.stopChan)
	})
}

// GetSeenCount retorna a quantidade de mensagens registradas de forma thread-safe.
func (w *Watcher) GetSeenCount(role config.RoleType) int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.seenCounts[role]
}

// PollOnce executa uma única rodada determinística do polling para cada papel.
func (w *Watcher) PollOnce() {
	w.pollMailboxes()
}

// pollMailboxes verifica se há novas mensagens para cada papel ativo com proteção de mutex.
func (w *Watcher) pollMailboxes() {
	for _, role := range w.Roles {
		msgs, err := w.Client.ListPendingMessages(role.Project)
		if err != nil {
			continue
		}

		current := len(msgs)

		w.mu.Lock()
		prev := w.seenCounts[role.Type]
		w.seenCounts[role.Type] = current
		w.mu.Unlock()

		if current > prev {
			// Nova mensagem detectada: acorda o pane e atualiza HUD
			w.Notifier.UpdatePaneStatus(role.Type, fmt.Sprintf("💬 %d Msg(s)", current), "green")
			prompt := fmt.Sprintf("⚠️ Nova mensagem pendente no seu inbox (%s). Execute 'ai-memory message list' e processe-a.", role.Project)
			_ = w.Notifier.SendKeys(role.Type, prompt)
		} else if current == 0 && prev > 0 {
			// Caixa de entrada esvaziada: volta para Idle
			w.Notifier.UpdatePaneStatus(role.Type, "💤 Aguardando", "blue")
		}
	}
}
