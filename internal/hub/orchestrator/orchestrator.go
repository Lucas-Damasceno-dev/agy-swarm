package orchestrator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/affinity"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/discovery"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/model"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/quarantine"
)

type Strategy string

const (
	StrategyLeastLoaded Strategy = "least-loaded"
	StrategyRoundRobin  Strategy = "round-robin"
	StrategyPriority    Strategy = "priority"
)

type OrchestratorState struct {
	LastSelectedAlias string    `json:"last_selected_alias"`
	LastDispatchTime  time.Time `json:"last_dispatch_time"`
}

type Orchestrator struct {
	stateFile       string
	affinityManager *affinity.Manager
	mu              sync.Mutex
}

func NewOrchestrator() *Orchestrator {
	realHome := discovery.GetRealHome()
	cfgDir := filepath.Join(realHome, ".config/agy-hub")
	_ = os.MkdirAll(cfgDir, 0755)
	return NewOrchestratorWithConfig(filepath.Join(cfgDir, "orchestrator_state.json"), affinity.NewManager())
}

func NewOrchestratorWithConfig(stateFile string, affMgr *affinity.Manager) *Orchestrator {
	dir := filepath.Dir(stateFile)
	_ = os.MkdirAll(dir, 0755)
	return &Orchestrator{
		stateFile:       stateFile,
		affinityManager: affMgr,
	}
}

// PreviewBest chooses the optimal account without mutating state on disk
func (o *Orchestrator) PreviewBest(profiles []*model.Profile, strat Strategy) (*model.Profile, string, error) {
	return o.PreviewBestWithWorkspace(profiles, strat, "")
}

// PreviewBestWithWorkspace evaluates accounts and cache affinity purely in-memory
func (o *Orchestrator) PreviewBestWithWorkspace(profiles []*model.Profile, strat Strategy, workspace string) (*model.Profile, string, error) {
	if len(profiles) == 0 {
		return nil, "", fmt.Errorf("no profiles available")
	}

	// Filter healthy profiles
	var healthy []*model.Profile
	for _, p := range profiles {
		if p.Status == model.StatusError {
			continue
		}
		if p.InCooldown && time.Now().Before(p.CooldownUntil) {
			continue
		}
		healthy = append(healthy, p)
	}

	if len(healthy) == 0 {
		sorted := make([]*model.Profile, len(profiles))
		copy(sorted, profiles)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].CooldownUntil.Before(sorted[j].CooldownUntil)
		})
		best := sorted[0]
		return best, fmt.Sprintf("⚠️ All profiles in cooldown! Selected %s (cooldown ends: %s)", best.Alias, time.Until(best.CooldownUntil).Round(time.Second)), nil
	}

	// Check Cache Affinity hit for the workspace (only if account has healthy weekly quota and tokens > 150k)
	if workspace != "" && o.affinityManager != nil {
		if profID, alias, warm := o.affinityManager.GetWarmAffinity(workspace); warm {
			for _, p := range healthy {
				if (p.ID == profID || p.Alias == alias) && len(p.RunningPIDs) < 3 && p.RemainingTokens > 150000 && p.WeeklyPercent > 15.0 {
					return p, fmt.Sprintf("🔥 Cache Affinity Hit (%s, semanal: %.1f%%, cache <5m)", p.Alias, p.WeeklyPercent), nil
				}
			}
		}
	}

	switch strat {
	case StrategyRoundRobin:
		return o.previewRoundRobin(healthy)
	case StrategyPriority:
		return o.previewPriority(healthy)
	case StrategyLeastLoaded:
		fallthrough
	default:
		return o.previewLeastLoaded(healthy)
	}
}

// SelectBest chooses the optimal account and updates persistent state
func (o *Orchestrator) SelectBest(profiles []*model.Profile, strat Strategy) (*model.Profile, string, error) {
	return o.SelectBestWithWorkspace(profiles, strat, "")
}

// SelectBestWithWorkspace chooses the optimal account, checking cache affinity and updating persistent state
func (o *Orchestrator) SelectBestWithWorkspace(profiles []*model.Profile, strat Strategy, workspace string) (*model.Profile, string, error) {
	selected, reason, err := o.PreviewBestWithWorkspace(profiles, strat, workspace)
	if err == nil && selected != nil {
		o.saveState(selected.Alias)
	}
	return selected, reason, err
}

func (o *Orchestrator) previewLeastLoaded(healthy []*model.Profile) (*model.Profile, string, error) {
	type scoredProfile struct {
		profile *model.Profile
		score   float64
	}

	var scored []scoredProfile
	for _, p := range healthy {
		score := 0.0

		// 1. Foco Primário Absoluto: Cota Semanal Restante (WeeklyPercent: 0 a 100%)
		weeklyUsed := 100.0 - p.WeeklyPercent
		if p.WeeklyPercent <= 0 {
			weeklyUsed = 100.0 - float64(p.QuotaPercent)
		}
		score += weeklyUsed * 20.0

		// 2. Proteção Crítica: Cota 24h quase esgotada (evita bater em 429 de imediato)
		if p.RemainingTokens < 100000 {
			score += 10000.0
		} else if p.RemainingTokens < 250000 {
			score += 2000.0
		}

		// 3. Proteção 5-Horas: Se a cota de 5h estiver crítica (< 10%)
		if p.FiveHourPercent > 0 && p.FiveHourPercent < 10.0 {
			score += 5000.0
		}

		// 4. Leve desempate para processos concorrentes (não anula a prioridade semanal)
		score += float64(len(p.RunningPIDs) * 10)
		score += float64(p.RecentRequests5m * 2)
		score += float64(p.ActiveConversations * 5)

		scored = append(scored, scoredProfile{profile: p, score: score})
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score < scored[j].score
	})

	selected := scored[0].profile
	reason := fmt.Sprintf("Semanal: %.1f%% livre, 5h: %.1f%%, Running: %d (Score: %.1f)",
		selected.WeeklyPercent, selected.FiveHourPercent, len(selected.RunningPIDs), scored[0].score)

	return selected, reason, nil
}

func (o *Orchestrator) previewRoundRobin(healthy []*model.Profile) (*model.Profile, string, error) {
	state := o.loadState()
	lastIdx := -1
	for i, p := range healthy {
		if p.Alias == state.LastSelectedAlias {
			lastIdx = i
			break
		}
	}

	nextIdx := (lastIdx + 1) % len(healthy)
	selected := healthy[nextIdx]
	return selected, fmt.Sprintf("Round-robin step %d/%d", nextIdx+1, len(healthy)), nil
}

func (o *Orchestrator) previewPriority(healthy []*model.Profile) (*model.Profile, string, error) {
	sorted := make([]*model.Profile, len(healthy))
	copy(sorted, healthy)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority > sorted[j].Priority
	})
	selected := sorted[0]
	return selected, fmt.Sprintf("Highest priority (%d)", selected.Priority), nil
}

// Dispatch executes the agy binary inside the target profile's isolated environment
func (o *Orchestrator) Dispatch(profile *model.Profile, args []string) error {
	cmd := o.buildCommand(profile, args)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// DispatchWithFailover runs with automatic failover if rate limited (429)
func (o *Orchestrator) DispatchWithFailover(profiles []*model.Profile, args []string, maxRetries int) error {
	attempted := make(map[string]bool)

	for attempt := 0; attempt < maxRetries; attempt++ {
		// Filter out already attempted profiles
		var available []*model.Profile
		for _, p := range profiles {
			if !attempted[p.ID] {
				available = append(available, p)
			}
		}

		if len(available) == 0 {
			return fmt.Errorf("todas as contas disponíveis falharam ou estão em cooldown")
		}

		target, reason, err := o.SelectBest(available, StrategyLeastLoaded)
		if err != nil {
			return err
		}

		attempted[target.ID] = true
		fmt.Printf("⚡ [Tentativa %d/%d] Executando em %s (%s)...\n", attempt+1, maxRetries, target.Alias, reason)

		var stdoutBuf, stderrBuf bytes.Buffer
		cmd := o.buildCommand(target, args)
		cmd.Stdin = os.Stdin
		cmd.Stdout = io.MultiWriter(os.Stdout, &stdoutBuf)
		cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)

		err = cmd.Run()
		if err == nil {
			return nil
		}

		combined := stdoutBuf.String() + "\n" + stderrBuf.String()
		if strings.Contains(combined, "429") || strings.Contains(combined, "RESOURCE_EXHAUSTED") || strings.Contains(combined, "quota") {
			fmt.Printf("⚠️ 429 Rate Limit detectado na conta %s! Marcando cooldown e chaveando...\n", target.Alias)
			target.InCooldown = true
			target.CooldownUntil = time.Now().Add(3 * time.Minute)
			qm := quarantine.NewManager()
			_, _ = qm.SyncQuarantine([]*model.Profile{target})
			continue
		}

		return err
	}

	return fmt.Errorf("limite de tentativas (%d) atingido sem sucesso", maxRetries)
}

// DispatchDetached runs a prompt in a detached background OS process and alerts via notify-send
func (o *Orchestrator) DispatchDetached(profile *model.Profile, prompt string, modelName ...string) error {
	realHome := discovery.GetRealHome()
	agyBin := o.getAgyBin()

	var agyArgs []string
	if len(modelName) > 0 && modelName[0] != "" && strings.ToLower(modelName[0]) != "auto" {
		agyArgs = append(agyArgs, "-m", modelName[0])
	}
	agyArgs = append(agyArgs, "-p", prompt)

	_ = os.MkdirAll(filepath.Join(profile.HomeDir, ".gemini/antigravity-cli"), 0755)

	pwd, _ := os.Getwd()
	if pwd != "" && o.affinityManager != nil {
		o.affinityManager.Record(pwd, profile.ID, profile.Alias)
	}

	runnerScript := `
agy_bin="$1"
shift
out=$(env \
  REAL_HOME="$REAL_HOME" \
  HOME="$TARGET_HOME" \
  XDG_CONFIG_HOME="$TARGET_HOME/.config" \
  XDG_DATA_HOME="$TARGET_HOME/.local/share" \
  XDG_CACHE_HOME="$TARGET_HOME/.cache" \
  XDG_STATE_HOME="$TARGET_HOME/.local/state" \
  DBUS_SESSION_BUS_ADDRESS="disabled" \
  GIT_CONFIG_GLOBAL="$REAL_HOME/.gitconfig" \
  "$agy_bin" "$@" 2>&1)
res=$?

title="⚡ AGY-HUB // $PROFILE_ALIAS Concluído"
if [ $res -eq 0 ]; then
  msg="Tarefa finalizada com sucesso."
else
  msg="Falha na execução (código $res):\n$out"
fi

notify-send -a "agy-hub" "$title" "$msg"
`
	cmdArgs := append([]string{"-c", runnerScript, "agy-detached-runner", agyBin}, agyArgs...)
	cmd := exec.Command("/bin/sh", cmdArgs...)

	// Fully detach process from terminal and parent process lifetime
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if pwd != "" {
		cmd.Dir = pwd
	}

	env := os.Environ()
	env = append(env,
		fmt.Sprintf("REAL_HOME=%s", realHome),
		fmt.Sprintf("TARGET_HOME=%s", profile.HomeDir),
		fmt.Sprintf("PROFILE_ALIAS=%s", profile.Alias),
	)
	cmd.Env = env

	return cmd.Start()
}

// DispatchSwarm splits a prompt or tasks across N concurrent profiles
func (o *Orchestrator) DispatchSwarm(profiles []*model.Profile, n int, prompt string, modelName ...string) error {
	if len(profiles) == 0 {
		return fmt.Errorf("nenhum perfil disponível para swarm")
	}

	var healthy []*model.Profile
	var fallback []*model.Profile
	now := time.Now()

	for _, p := range profiles {
		if p.Status == model.StatusError || p.Email == "" {
			continue
		}
		if p.InCooldown && now.Before(p.CooldownUntil) {
			fallback = append(fallback, p)
			continue
		}
		healthy = append(healthy, p)
	}

	// Sort healthy by lowest load first
	sort.Slice(healthy, func(i, j int) bool {
		return healthy[i].RecentRequests5m < healthy[j].RecentRequests5m
	})
	// Sort fallback by earliest cooldown end
	sort.Slice(fallback, func(i, j int) bool {
		return fallback[i].CooldownUntil.Before(fallback[j].CooldownUntil)
	})

	candidates := append(healthy, fallback...)
	if len(candidates) == 0 {
		return fmt.Errorf("nenhum perfil ativo ou utilizável para swarm")
	}

	if n > len(candidates) {
		n = len(candidates)
	}
	if n <= 0 {
		n = 1
	}

	var wg sync.WaitGroup
	fmt.Printf("🚀 Disparando Swarm com %d contas dedicadas em paralelo...\n", n)

	for i := 0; i < n; i++ {
		p := candidates[i]
		wg.Add(1)
		go func(workerID int, prof *model.Profile) {
			defer wg.Done()
			workerPrompt := fmt.Sprintf("[Worker %d/%d - Conta: %s] %s", workerID+1, n, prof.Alias, prompt)
			args := []string{}
			if len(modelName) > 0 && modelName[0] != "" && strings.ToLower(modelName[0]) != "auto" {
				args = append(args, "-m", modelName[0])
			}
			args = append(args, "-p", workerPrompt)
			cmd := o.buildCommand(prof, args)
			out, err := cmd.CombinedOutput()
			if err != nil {
				fmt.Printf("❌ [Worker %d: %s] Erro: %v\n", workerID+1, prof.Alias, err)
			} else {
				fmt.Printf("✓ [Worker %d: %s] Sucesso (%d bytes):\n%s\n", workerID+1, prof.Alias, len(out), string(out))
			}
		}(i, p)
	}

	wg.Wait()
	fmt.Println("🏁 Swarm finalizado em todas as contas.")
	return nil
}

func (o *Orchestrator) getAgyBin() string {
	realHome := discovery.GetRealHome()
	agyBin := filepath.Join(realHome, ".local/bin/agy-real")
	if _, err := os.Stat(agyBin); os.IsNotExist(err) {
		agyBin = filepath.Join(realHome, ".local/bin/agy")
		if _, err := os.Stat(agyBin); os.IsNotExist(err) {
			if agyPath, err := exec.LookPath("agy"); err == nil {
				agyBin = agyPath
			}
		}
	}
	return agyBin
}

func (o *Orchestrator) buildCommand(profile *model.Profile, args []string) *exec.Cmd {
	realHome := discovery.GetRealHome()
	agyBin := o.getAgyBin()
	cmd := exec.Command(agyBin, args...)

	customEnv := map[string]string{
		"REAL_HOME":                realHome,
		"HOME":                     profile.HomeDir,
		"XDG_CONFIG_HOME":          filepath.Join(profile.HomeDir, ".config"),
		"XDG_DATA_HOME":            filepath.Join(profile.HomeDir, ".local/share"),
		"XDG_CACHE_HOME":           filepath.Join(profile.HomeDir, ".cache"),
		"XDG_STATE_HOME":           filepath.Join(profile.HomeDir, ".local/state"),
		"DBUS_SESSION_BUS_ADDRESS": "disabled",
		"GIT_CONFIG_GLOBAL":        filepath.Join(realHome, ".gitconfig"),
	}

	env := os.Environ()
	filteredEnv := []string{}
	for _, e := range env {
		parts := splitEnv(e)
		if _, exists := customEnv[parts[0]]; exists {
			continue
		}
		filteredEnv = append(filteredEnv, e)
	}

	for k, v := range customEnv {
		filteredEnv = append(filteredEnv, fmt.Sprintf("%s=%s", k, v))
	}

	cmd.Env = filteredEnv
	_ = os.MkdirAll(filepath.Join(profile.HomeDir, ".gemini/antigravity-cli"), 0755)

	if pwd, err := os.Getwd(); err == nil && o.affinityManager != nil {
		o.affinityManager.Record(pwd, profile.ID, profile.Alias)
	}

	return cmd
}

// ExecShell replaces current process with agy-profile invocation for interactive session
func (o *Orchestrator) ExecShell(profile *model.Profile, args []string) error {
	realHome := discovery.GetRealHome()
	agyProfileBin := filepath.Join(realHome, ".local/bin/agy-profile")
	if _, err := os.Stat(agyProfileBin); os.IsNotExist(err) {
		agyProfileBin, _ = exec.LookPath("agy-profile")
	}

	if agyProfileBin == "" {
		return o.Dispatch(profile, args)
	}

	fullArgs := append([]string{"agy-profile", profile.ID}, args...)
	return syscall.Exec(agyProfileBin, fullArgs, os.Environ())
}

func (o *Orchestrator) loadState() OrchestratorState {
	o.mu.Lock()
	defer o.mu.Unlock()

	var st OrchestratorState
	data, err := os.ReadFile(o.stateFile)
	if err == nil {
		_ = json.Unmarshal(data, &st)
	}
	return st
}

func (o *Orchestrator) saveState(alias string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	st := OrchestratorState{
		LastSelectedAlias: alias,
		LastDispatchTime:  time.Now(),
	}
	data, err := json.Marshal(st)
	if err != nil {
		return
	}
	tmpFile := o.stateFile + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err == nil {
		_ = os.Rename(tmpFile, o.stateFile)
	}
}

func splitEnv(s string) [2]string {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return [2]string{s[:i], s[i+1:]}
		}
	}
	return [2]string{s, ""}
}
