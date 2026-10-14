package guardrail

import (
	"testing"
	"time"
)

func TestCircuitBreakerSuccessFlow(t *testing.T) {
	cfg := CircuitBreakerConfig{
		MaxConsecutiveFailures: 3,
		MaxIdenticalLoops:      2,
		ResetTimeout:           1 * time.Second,
	}
	cb := NewCircuitBreaker(cfg)

	allowed, reason := cb.AllowExecution()
	if !allowed {
		t.Fatalf("deveria permitir execução no estado fechado: %s", reason)
	}

	cb.RecordSuccess()
	state, failures, _ := cb.Status()
	if state != StateClosed || failures != 0 {
		t.Errorf("estado inesperado após sucesso: %s, failures=%d", state, failures)
	}
}

func TestCircuitBreakerConsecutiveFailures(t *testing.T) {
	cfg := CircuitBreakerConfig{
		MaxConsecutiveFailures: 3,
		MaxIdenticalLoops:      5,
		ResetTimeout:           100 * time.Millisecond,
	}
	cb := NewCircuitBreaker(cfg)

	// Tentativa 1 com diffs diferentes
	opened, _ := cb.RecordFailure("diff1", "error1")
	if opened {
		t.Fatalf("não deveria abrir na 1ª falha")
	}

	// Tentativa 2
	opened, _ = cb.RecordFailure("diff2", "error2")
	if opened {
		t.Fatalf("não deveria abrir na 2ª falha")
	}

	// Tentativa 3 -> deve atingir MaxConsecutiveFailures=3 e abrir
	opened, _ = cb.RecordFailure("diff3", "error3")
	if !opened {
		t.Fatalf("deveria ter aberto o circuito na 3ª falha consecutiva")
	}

	allowed, _ := cb.AllowExecution()
	if allowed {
		t.Fatalf("não deveria permitir execução com circuito aberto")
	}

	// Espera reset timeout para transição Half-Open
	time.Sleep(150 * time.Millisecond)
	allowed, status := cb.AllowExecution()
	if !allowed || status != "half-open recovery" {
		t.Fatalf("esperava transição para half-open após timeout: allowed=%v status=%s", allowed, status)
	}

	// Sucesso fecha o circuito novamente
	cb.RecordSuccess()
	allowed, _ = cb.AllowExecution()
	if !allowed {
		t.Fatalf("deveria permitir execução após fechamento com sucesso")
	}
}

func TestCircuitBreakerDoomLoopDetection(t *testing.T) {
	cfg := CircuitBreakerConfig{
		MaxConsecutiveFailures: 10, // alto de propósito para testar gatilho do doom loop
		MaxIdenticalLoops:      2,
		ResetTimeout:           1 * time.Minute,
	}
	cb := NewCircuitBreaker(cfg)

	// 1ª tentativa falha com diff A e erro X
	opened, _ := cb.RecordFailure("var x = 1", "syntax error at line 1")
	if opened {
		t.Fatalf("não deveria abrir na 1ª tentativa")
	}

	// 2ª tentativa falha EXATAMENTE com o mesmo diff A e erro X (nenhum progresso funcional)
	opened, reason := cb.RecordFailure("var x = 1", "syntax error at line 1")
	if !opened {
		t.Fatalf("deveria ter aberto imediatamente por Doom-Loop!")
	}

	state, _, openReason := cb.Status()
	if state != StateOpen {
		t.Errorf("esperava estado OPEN, obteve %s", state)
	}
	if openReason != reason {
		t.Errorf("openReason diverge: %s != %s", openReason, reason)
	}

	allowed, msg := cb.AllowExecution()
	if allowed {
		t.Fatalf("execução permitida indevidamente após doom-loop: %s", msg)
	}
}
