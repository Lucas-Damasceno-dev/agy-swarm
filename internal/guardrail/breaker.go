package guardrail

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	// Remove durações de execução (ex: 0.014s, 15ms, 120µs, 5ns)
	durationRegex = regexp.MustCompile(`\b\d+(\.\d+)?(ms|s|µs|ns)\b`)
	// Remove endereços de ponteiros de memória em stack traces (ex: 0xc0000a2040)
	pointerRegex = regexp.MustCompile(`0x[0-9a-fA-F]+`)
	// Remove timestamps ISO / RFC3339
	timestampRegex = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})?\b`)
	// Normaliza múltiplos espaços e quebras de linha repetidas
	whitespaceRegex = regexp.MustCompile(`[ \t]+`)
)

// State representa o estado do circuito.
type State string

const (
	StateClosed   State = "CLOSED"    // Normal: execuções permitidas
	StateOpen     State = "OPEN"      // Aberto: bloqueado para evitar queima estéril de cotas
	StateHalfOpen State = "HALF_OPEN" // Testando recuperação
)

// CircuitBreakerConfig define as regras e limites de proteção anti-loop.
type CircuitBreakerConfig struct {
	MaxConsecutiveFailures int
	MaxIdenticalLoops      int
	ResetTimeout           time.Duration
}

// DefaultConfig retorna a configuração padrão recomendada contra doom loops.
func DefaultConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		MaxConsecutiveFailures: 3,
		MaxIdenticalLoops:      2,
		ResetTimeout:           2 * time.Minute,
	}
}

// CircuitBreaker protege o enxame contra loops infinitos, estouro de cotas e doom-loops estéreis.
type CircuitBreaker struct {
	mu                  sync.RWMutex
	cfg                 CircuitBreakerConfig
	state               State
	consecutiveFailures int
	totalFailures       int
	totalAttempts       int
	lastFingerprint     string
	identicalLoopCount  int
	lastFailureTime     time.Time
	openReason          string
}

// NewCircuitBreaker inicializa o disjuntor com as proteções configuradas.
func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	if cfg.MaxConsecutiveFailures <= 0 {
		cfg.MaxConsecutiveFailures = 3
	}
	if cfg.MaxIdenticalLoops <= 0 {
		cfg.MaxIdenticalLoops = 2
	}
	if cfg.ResetTimeout <= 0 {
		cfg.ResetTimeout = 2 * time.Minute
	}

	return &CircuitBreaker{
		cfg:   cfg,
		state: StateClosed,
	}
}

// NormalizeTestOutput remove ruídos voláteis (durações, endereços de memória e timestamps)
// garantindo que erros funcionais idênticos produzam o mesmo hash criptográfico.
func NormalizeTestOutput(raw string) string {
	s := durationRegex.ReplaceAllString(raw, "<dur>")
	s = pointerRegex.ReplaceAllString(s, "<ptr>")
	s = timestampRegex.ReplaceAllString(s, "<time>")
	s = whitespaceRegex.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// ComputeFingerprint gera um hash único da combinação do diff normalizado e do output de teste normalizado.
func ComputeFingerprint(diff, testOutput string) string {
	normDiff := strings.TrimSpace(diff)
	normOut := NormalizeTestOutput(testOutput)

	h := sha256.New()
	h.Write([]byte(normDiff))
	h.Write([]byte("|||"))
	h.Write([]byte(normOut))
	return hex.EncodeToString(h.Sum(nil))
}

// AllowExecution verifica se a execução da etapa está autorizada ou bloqueada pelo circuito.
func (cb *CircuitBreaker) AllowExecution() (bool, string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == StateClosed {
		return true, ""
	}

	if cb.state == StateOpen {
		// Verifica se já passou o tempo de cooldown para transição para Half-Open
		if time.Since(cb.lastFailureTime) > cb.cfg.ResetTimeout {
			cb.state = StateHalfOpen
			return true, "half-open recovery"
		}
		return false, fmt.Sprintf("circuito aberto: %s", cb.openReason)
	}

	// StateHalfOpen
	return true, "half-open probe"
}

// RecordSuccess registra uma execução bem-sucedida, fechando o circuito e resetando contadores.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = StateClosed
	cb.consecutiveFailures = 0
	cb.identicalLoopCount = 0
	cb.lastFingerprint = ""
	cb.openReason = ""
}

// RecordFailure analisa a falha, computa o fingerprint e abre o circuito se detectar doom-loop ou excesso de erros.
func (cb *CircuitBreaker) RecordFailure(diff, testOutput string) (bool, string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecutiveFailures++
	cb.totalFailures++
	cb.totalAttempts++
	cb.lastFailureTime = time.Now()

	fp := ComputeFingerprint(diff, testOutput)

	// Verificação de Doom Loop (mesmo diff + mesmo erro em tentativas consecutivas)
	if cb.lastFingerprint != "" && cb.lastFingerprint == fp {
		cb.identicalLoopCount++
	} else {
		cb.identicalLoopCount = 1
	}
	cb.lastFingerprint = fp

	// Regra 1: Doom Loop detectado
	if cb.identicalLoopCount >= cb.cfg.MaxIdenticalLoops {
		cb.state = StateOpen
		cb.openReason = fmt.Sprintf("Doom-Loop detectado: %d tentativas consecutivas produziram o mesmo erro sem alteração funcional no código", cb.identicalLoopCount)
		return true, cb.openReason
	}

	// Regra 2: Limite de falhas consecutivas atingido
	if cb.consecutiveFailures >= cb.cfg.MaxConsecutiveFailures {
		cb.state = StateOpen
		cb.openReason = fmt.Sprintf("Limite máximo de falhas consecutivas (%d) atingido", cb.consecutiveFailures)
		return true, cb.openReason
	}

	return false, ""
}

// Status retorna um resumo do disjuntor para telemetria no dashboard.
func (cb *CircuitBreaker) Status() (State, int, string) {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state, cb.consecutiveFailures, cb.openReason
}
