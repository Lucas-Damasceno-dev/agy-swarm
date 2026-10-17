package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/lease"
)

// ProcessRunner interface desacoplada para execução de comandos do sistema com suporte a mock.
type ProcessRunner interface {
	RunEphemeral(ctx context.Context, workdir string, accountID string, promptFile string, timeout time.Duration) (string, int, error)
	RunCommand(ctx context.Context, workdir string, commandLine string, timeout time.Duration) (string, int, error)
}

// OSProcessRunner implementa ProcessRunner com controle estrito de processo via killpg.
type OSProcessRunner struct {
	LogWriter io.Writer
	Env       []string
}

// NewOSProcessRunner cria uma nova instância de OSProcessRunner.
func NewOSProcessRunner() *OSProcessRunner {
	return &OSProcessRunner{}
}

// NewOSProcessRunnerWithLogger cria uma nova instância com streaming em tempo real para um io.Writer.
func NewOSProcessRunnerWithLogger(w io.Writer) *OSProcessRunner {
	return &OSProcessRunner{LogWriter: w}
}

// RunEphemeral despacha o processo efêmero do agy-profile com isolamento em PGID.
func (r *OSProcessRunner) RunEphemeral(ctx context.Context, workdir string, accountID string, promptFile string, timeout time.Duration) (string, int, error) {
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	promptBytes, err := os.ReadFile(promptFile)
	if err != nil {
		return "", -1, fmt.Errorf("falha ao ler ficheiro de prompt (%s): %w", promptFile, err)
	}

	cmd := exec.CommandContext(execCtx, "agy-profile", accountID, "--dangerously-skip-permissions", "-p", string(promptBytes))
	cmd.Dir = workdir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if len(r.Env) > 0 {
		cmd.Env = append(os.Environ(), r.Env...)
	}

	var outputBuf bytes.Buffer
	var stdoutWriter io.Writer = &outputBuf
	var stderrWriter io.Writer = &outputBuf

	if r.LogWriter != nil {
		stdoutWriter = io.MultiWriter(&outputBuf, r.LogWriter)
		stderrWriter = io.MultiWriter(&outputBuf, r.LogWriter)
	}

	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter

	if err := cmd.Start(); err != nil {
		return "", -1, fmt.Errorf("falha ao iniciar processo efêmero agy-profile: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-execCtx.Done():
		// Timeout ou cancelamento: mata o process group inteiro (killpg)
		if cmd.Process != nil && cmd.Process.Pid > 0 {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return outputBuf.String(), -1, fmt.Errorf("processo efêmero excedeu o timeout de %v", timeout)
	case err := <-done:
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = 1
			}
		}
		return outputBuf.String(), exitCode, err
	}
}

// RunCommand executa um comando genérico de terminal no diretório alvo (ex: test-cmd).
func (r *OSProcessRunner) RunCommand(ctx context.Context, workdir string, commandLine string, timeout time.Duration) (string, int, error) {
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "sh", "-c", commandLine)
	cmd.Dir = workdir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if len(r.Env) > 0 {
		cmd.Env = append(os.Environ(), r.Env...)
	}

	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	if err := cmd.Start(); err != nil {
		return "", -1, fmt.Errorf("falha ao iniciar comando (%s): %w", commandLine, err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-execCtx.Done():
		if cmd.Process != nil && cmd.Process.Pid > 0 {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return outBuf.String(), -1, fmt.Errorf("comando excedeu timeout de %v", timeout)
	case err := <-done:
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = 1
			}
		}
		return outBuf.String(), exitCode, err
	}
}

// EphemeralDispatcher coordena a execução de um papel com lease e failover automático em 429.
type EphemeralDispatcher struct {
	Runner ProcessRunner
	Broker *lease.Broker
}

// NewEphemeralDispatcher cria um despachante de tarefas efêmeras.
func NewEphemeralDispatcher(runner ProcessRunner, broker *lease.Broker) *EphemeralDispatcher {
	if runner == nil {
		runner = NewOSProcessRunner()
	}
	return &EphemeralDispatcher{
		Runner: runner,
		Broker: broker,
	}
}

// IsRateLimitOrQuotaExhausted verifica se o output acusa HTTP 429 ou cota esgotada.
func IsRateLimitOrQuotaExhausted(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "429") ||
		strings.Contains(lower, "resource_exhausted") ||
		strings.Contains(lower, "quota exceeded") ||
		strings.Contains(lower, "rate limit")
}

// DispatchWithFailover executa o papel efêmero garantindo fallback transparente em caso de 429.
func (d *EphemeralDispatcher) DispatchWithFailover(ctx context.Context, workdir string, role config.RoleConfig, stage string, promptFile string, timeout time.Duration) (string, int, *lease.Lease, error) {
	activeLease, err := d.Broker.Acquire(ctx, role.Type, stage)
	if err != nil {
		return "", -1, nil, fmt.Errorf("falha ao obter lease inicial para %s: %w", role.Type, err)
	}

	maxAttempts := 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		output, exitCode, execErr := d.Runner.RunEphemeral(ctx, workdir, activeLease.AccountID, promptFile, timeout)

		// Verifica se tomou HTTP 429 / cota esgotada
		if IsRateLimitOrQuotaExhausted(output) {
			// Aciona failover imediato para outra conta do pool de 10
			newLease, failoverErr := d.Broker.Failover(ctx, activeLease)
			if failoverErr != nil {
				d.Broker.Release(activeLease)
				return output, exitCode, nil, fmt.Errorf("429 detectado e falha no failover de conta: %w", failoverErr)
			}
			activeLease = newLease
			time.Sleep(2 * time.Second) // Aguarda estabilização de worktree
			continue
		}

		return output, exitCode, activeLease, execErr
	}

	d.Broker.Release(activeLease)
	return "", -1, nil, fmt.Errorf("esgotadas tentativas de execução com failover para %s", role.Type)
}
