package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/collector"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/daemon"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/discovery"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/metrics"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/model"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/orchestrator"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/syncer"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func handleHub(args []string) {
	if len(args) < 1 {
		runTUI()
		return
	}

	command := args[0]

	switch command {
	case "tui":
		runTUI()

	case "status":
		runStatus(args[1:])

	case "exec", "run":
		runExec(args[1:])

	case "which":
		runWhich(args[1:])

	case "auto":
		runAuto(args[1:])

	case "sync":
		runSync()

	case "daemon":
		runDaemon(args[1:])

	case "install-systemd":
		runInstallSystemd()

	case "install-daemon-systemd":
		runInstallDaemonSystemd()

	case "reset-cooldown", "unquarantine":
		runResetCooldown(args[1:])

	case "leaderboard", "rank", "stats":
		runLeaderboard(args[1:])

	case "env":
		runEnv(args[1:])

	case "help", "-h", "--help":
		printHelp()

	default:
		runExec(args)
	}
}

func runTUI() {
	m := tui.NewModel()
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao iniciar TUI: %v\n", err)
		os.Exit(1)
	}
}

func runDaemon(args []string) {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	pHTTPPort := fs.Int("http-port", 8318, "Porta HTTP para métricas Prometheus e health check (0 para desativar)")
	_ = fs.Parse(args)

	d := daemon.NewServer()
	if *pHTTPPort > 0 {
		d.SetHTTPPort(*pHTTPPort)
	}
	if err := d.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao iniciar daemon: %v\n", err)
		os.Exit(1)
	}
}

func getClusterState() (*model.ClusterState, error) {
	// Try daemon fast path (<5ms)
	if state, err := daemon.FetchStateFromDaemon(); err == nil {
		return state, nil
	}
	// Fallback to direct collection
	c := collector.NewCollector()
	return c.CollectAll()
}

func runStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	jsonOutput := fs.Bool("json", false, "Output as JSON")
	barOutput := fs.Bool("bar", false, "One-line status bar for Waybar/Tmux")
	compactOutput := fs.Bool("compact", false, "Omit verbose log dumps for widgets")
	includeLogs := fs.Bool("logs", false, "Include verbose log lines in JSON")
	_ = fs.Parse(args)

	state, err := getClusterState()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao coletar telemetria: %v\n", err)
		os.Exit(1)
	}

	if *barOutput {
		printBarStatus(state)
		return
	}

	if *jsonOutput {
		if *compactOutput || !*includeLogs {
			for _, p := range state.Profiles {
				p.RecentLogLines = nil
			}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(state)
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintf(w, "ALIAS\tID\tEMAIL\tSTATUS\tQUOTA REM.\tREM.STEPS\tRPM\tSTEPS\tEST.TOK(24h)\tPROXY\tSPARK\tREASON\n")
	for _, p := range state.Profiles {
		proxy := "NO"
		if p.ProxySynced {
			proxy = "SYNC"
		}
		tokK := fmt.Sprintf("%dk", p.EstTokens24h/1000)
		remK := fmt.Sprintf("%.2fM (%d%%)", float64(p.RemainingTokens)/1000000.0, p.QuotaPercent)
		stepsRem := fmt.Sprintf("~%d", p.RemainingSteps)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%d\t%s\t%s\t%s\t%s\n",
			p.Alias, p.ID, p.Email, p.Status, remK, stepsRem, p.RPM, p.TotalSteps, tokK, proxy, p.Sparkline, p.StatusReason)
	}
	_ = w.Flush()

	fmt.Printf("\nCota Total Cluster: %.2fM / %.2fM restantes (%d%%)\n",
		float64(state.ClusterRemainingTokens)/1000000.0,
		float64(state.ClusterQuotaTokens)/1000000.0,
		state.ClusterQuotaPercent)

	fmt.Printf("Proxy Local (127.0.0.1:%d): ", state.ProxyPort)
	if state.ProxyRunning {
		fmt.Printf("ONLINE (PID %d)\n", state.ProxyPID)
	} else {
		fmt.Println("OFFLINE")
	}

	fmt.Printf("Hermes Gateway: ")
	if state.HermesRunning {
		fmt.Printf("ONLINE (PID %d · %s)\n", state.HermesPID, state.HermesStatus)
	} else {
		fmt.Println("OFFLINE")
	}

	fmt.Printf("Reset Diário de Cota: em %s (Meia-noite PDT / 04h00 BRT)\n", state.ResetCountdown)
}

func printBarStatus(state *model.ClusterState) {
	var sb strings.Builder
	sb.WriteString("AGY:[")
	for i, p := range state.Profiles {
		icon := "○" // Idle
		if p.Status == model.StatusActive {
			icon = "●" // Active
		} else if p.Status == model.StatusCooldown {
			icon = "❄" // Cooldown
		} else if p.Status == model.StatusError {
			icon = "✖" // Error
		}
		sb.WriteString(fmt.Sprintf("%d:%s", i+1, icon))
		if i < len(state.Profiles)-1 {
			sb.WriteString(" ")
		}
	}
	sb.WriteString("]")

	if state.ProxyRunning {
		sb.WriteString(" ⚡Proxy:OK")
	} else {
		sb.WriteString(" ⚠️Proxy:OFF")
	}

	fmt.Println(sb.String())
}

func runWhich(args []string) {
	state, err := getClusterState()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
		os.Exit(1)
	}

	orch := orchestrator.NewOrchestrator()
	strat := orchestrator.StrategyRoundRobin
	if len(args) > 0 {
		strat = orchestrator.Strategy(args[0])
	}

	pwd, _ := os.Getwd()
	best, reason, err := orch.SelectBestWithWorkspace(state.Profiles, strat, pwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao selecionar conta: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("🎯 Conta Selecionada: %s (%s)\n", best.Alias, best.ID)
	fmt.Printf("   Email: %s\n", best.Email)
	fmt.Printf("   Razão: %s\n", reason)
}

func runExec(args []string) {
	if len(args) == 0 {
		fmt.Println("Uso: agy-hub exec [flags] <prompt>")
		os.Exit(1)
	}

	var accountOverride string
	var strategy string
	var modelFlag string
	var detach bool
	var swarmCount int
	noFailover := false

	var cleanArgs []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--account" || arg == "-a" {
			if i+1 < len(args) {
				accountOverride = args[i+1]
				i++
				continue
			}
		}
		if arg == "--strategy" || arg == "-s" {
			if i+1 < len(args) {
				strategy = args[i+1]
				i++
				continue
			}
		}
		if arg == "--model" || arg == "-m" {
			if i+1 < len(args) {
				modelFlag = args[i+1]
				i++
				continue
			}
		}
		if arg == "--detach" || arg == "-b" {
			detach = true
			continue
		}
		if arg == "--no-failover" {
			noFailover = true
			continue
		}
		if arg == "--swarm" {
			if i+1 < len(args) {
				if n, err := strconv.Atoi(args[i+1]); err == nil {
					swarmCount = n
					i++
					continue
				}
			}
			swarmCount = 5
			continue
		}
		cleanArgs = append(cleanArgs, arg)
	}

	state, err := getClusterState()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao inspecionar perfis: %v\n", err)
		os.Exit(1)
	}

	orch := orchestrator.NewOrchestrator()
	prompt := strings.Join(cleanArgs, " ")
	pwd, _ := os.Getwd()

	// Swarm Execution Mode
	if swarmCount > 0 {
		var healthy []*model.Profile
		var fallback []*model.Profile
		now := time.Now()
		for _, p := range state.Profiles {
			if p.Status == model.StatusError || p.Email == "" {
				continue
			}
			if p.InCooldown && now.Before(p.CooldownUntil) {
				fallback = append(fallback, p)
				continue
			}
			healthy = append(healthy, p)
		}
		sort.Slice(healthy, func(i, j int) bool {
			return healthy[i].RecentRequests5m < healthy[j].RecentRequests5m
		})
		sort.Slice(fallback, func(i, j int) bool {
			return fallback[i].CooldownUntil.Before(fallback[j].CooldownUntil)
		})
		candidates := append(healthy, fallback...)
		if len(candidates) == 0 {
			fmt.Fprintf(os.Stderr, "Erro: nenhum perfil utilizável para Swarm.\n")
			os.Exit(1)
		}
		if swarmCount > len(candidates) {
			swarmCount = len(candidates)
		}

		if detach {
			fmt.Printf("🚀 Swarm com %d contas despachado em segundo plano (notificações desktop ao concluir).\n", swarmCount)
			for i := 0; i < swarmCount; i++ {
				workerPrompt := fmt.Sprintf("[Worker %d/%d - Conta: %s] %s", i+1, swarmCount, candidates[i].Alias, prompt)
				_ = orch.DispatchDetached(candidates[i], workerPrompt, modelFlag)
			}
			return
		}
		if err := orch.DispatchSwarm(candidates, swarmCount, prompt, modelFlag); err != nil {
			fmt.Fprintf(os.Stderr, "Erro no Swarm: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Detached Background Mode
	if detach {
		best, _, err := orch.SelectBestWithWorkspace(state.Profiles, orchestrator.StrategyLeastLoaded, pwd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("🚀 Tarefa despachada em segundo plano na conta %s (notificação desktop ao concluir).\n", best.Alias)
		_ = orch.DispatchDetached(best, prompt, modelFlag)
		return
	}

	// Single Account Override Mode
	if accountOverride != "" {
		var target *model.Profile
		for _, p := range state.Profiles {
			if strings.EqualFold(p.ID, accountOverride) || strings.EqualFold(p.Alias, accountOverride) {
				target = p
				break
			}
		}
		if target == nil {
			fmt.Fprintf(os.Stderr, "Perfil '%s' não encontrado.\n", accountOverride)
			os.Exit(1)
		}

		var agyArgs []string
		if modelFlag != "" && strings.ToLower(modelFlag) != "auto" {
			agyArgs = append(agyArgs, "-m", modelFlag)
		}
		if len(cleanArgs) == 1 && !strings.HasPrefix(cleanArgs[0], "-") {
			agyArgs = append(agyArgs, "-p", cleanArgs[0])
		} else {
			agyArgs = append(agyArgs, cleanArgs...)
		}

		if err := orch.Dispatch(target, agyArgs); err != nil {
			fmt.Fprintf(os.Stderr, "Erro na execução: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Dynamic Failover Mode
	strat := orchestrator.StrategyLeastLoaded
	if strategy != "" {
		strat = orchestrator.Strategy(strategy)
	}

	var agyArgs []string
	if modelFlag != "" && strings.ToLower(modelFlag) != "auto" {
		agyArgs = append(agyArgs, "-m", modelFlag)
	}
	if len(cleanArgs) == 1 && !strings.HasPrefix(cleanArgs[0], "-") {
		agyArgs = append(agyArgs, "-p", cleanArgs[0])
	} else {
		agyArgs = append(agyArgs, cleanArgs...)
	}

	if noFailover {
		best, reason, err := orch.SelectBestWithWorkspace(state.Profiles, strat, pwd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("⚡ Executando em %s (%s)...\n", best.Alias, reason)
		if err := orch.Dispatch(best, agyArgs); err != nil {
			fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Run with in-flight failover (retries up to 3 accounts if 429 occurs)
	if err := orch.DispatchWithFailover(state.Profiles, agyArgs, 3); err != nil {
		fmt.Fprintf(os.Stderr, "Execução terminada com erro: %v\n", err)
		os.Exit(1)
	}
}

func runEnv(args []string) {
	state, err := getClusterState()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
		os.Exit(1)
	}

	orch := orchestrator.NewOrchestrator()
	pwd, _ := os.Getwd()
	target, _, err := orch.SelectBestWithWorkspace(state.Profiles, orchestrator.StrategyLeastLoaded, pwd)
	if len(args) > 0 {
		for _, p := range state.Profiles {
			if strings.EqualFold(p.ID, args[0]) || strings.EqualFold(p.Alias, args[0]) {
				target = p
				break
			}
		}
	}
	if target == nil {
		fmt.Fprintf(os.Stderr, "Nenhuma conta disponível.\n")
		os.Exit(1)
	}

	realHome := discovery.GetRealHome()
	fmt.Printf("export REAL_HOME=%q\n", realHome)
	fmt.Printf("export HOME=%q\n", target.HomeDir)
	fmt.Printf("export XDG_CONFIG_HOME=%q\n", filepath.Join(target.HomeDir, ".config"))
	fmt.Printf("export XDG_DATA_HOME=%q\n", filepath.Join(target.HomeDir, ".local/share"))
	fmt.Printf("export XDG_CACHE_HOME=%q\n", filepath.Join(target.HomeDir, ".cache"))
	fmt.Printf("export XDG_STATE_HOME=%q\n", filepath.Join(target.HomeDir, ".local/state"))
	fmt.Printf("export DBUS_SESSION_BUS_ADDRESS=\"disabled\"\n")
	fmt.Printf("export GIT_CONFIG_GLOBAL=%q\n", filepath.Join(realHome, ".gitconfig"))
	fmt.Printf("# Conta: %s (%s) · %s\n", target.Alias, target.ID, target.Email)
}

func runSync() {
	state, err := getClusterState()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao coletar contas: %v\n", err)
		os.Exit(1)
	}

	s := syncer.NewSyncer()
	synced, err := s.SyncAllAccounts(state.Profiles)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao sincronizar: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ %d contas sincronizadas em ~/.cli-proxy-api/:\n", len(synced))
	for _, item := range synced {
		fmt.Printf("  • %s\n", item)
	}

	fmt.Print("Verificando serviço cli-proxy-api... ")
	if err := s.EnsureProxyRunning(); err != nil {
		fmt.Printf("⚠️ Erro: %v\n", err)
	} else {
		fmt.Println("OK (Serviço ativo, sincronização zero-downtime)")
	}
}

func getAgyHubExecutable(realHome string) string {
	hubBin := filepath.Join(realHome, ".local/bin/agy-hub")
	if _, err := os.Stat(hubBin); err == nil {
		return hubBin
	}
	if exe, err := os.Executable(); err == nil && exe != "" {
		return exe
	}
	return hubBin
}

func runInstallSystemd() {
	realHome := discovery.GetRealHome()
	systemdDir := filepath.Join(realHome, ".config/systemd/user")
	_ = os.MkdirAll(systemdDir, 0755)

	agyHubBin := getAgyHubExecutable(realHome)

	serviceContent := fmt.Sprintf(`[Unit]
Description=AGY-Hub Automatic Multi-Account Token Synchronizer
After=network.target

[Service]
Type=oneshot
ExecStart=%s sync
StandardOutput=journal
StandardError=journal
`, agyHubBin)

	timerContent := `[Unit]
Description=Run AGY-Hub Token Sync every 30 minutes

[Timer]
OnBootSec=2min
OnUnitActiveSec=30min
Persistent=true

[Install]
WantedBy=timers.target
`

	servicePath := filepath.Join(systemdDir, "agy-hub-sync.service")
	timerPath := filepath.Join(systemdDir, "agy-hub-sync.timer")

	_ = os.WriteFile(servicePath, []byte(serviceContent), 0644)
	_ = os.WriteFile(timerPath, []byte(timerContent), 0644)

	fmt.Println("✓ Arquivos de serviço criados:")
	fmt.Printf("  • %s\n", servicePath)
	fmt.Printf("  • %s\n", timerPath)
	fmt.Println("\nPara ativar o timer automático execute:")
	fmt.Println("  systemctl --user daemon-reload && systemctl --user enable --now agy-hub-sync.timer")
}

func runInstallDaemonSystemd() {
	realHome := discovery.GetRealHome()
	systemdDir := filepath.Join(realHome, ".config/systemd/user")
	_ = os.MkdirAll(systemdDir, 0755)

	agyHubBin := getAgyHubExecutable(realHome)

	serviceContent := fmt.Sprintf(`[Unit]
Description=AGY-Hub Resident Background Daemon & Auto-Quarantine Engine
After=network.target

[Service]
Type=simple
ExecStart=%s daemon
Restart=on-failure
RestartSec=5s
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=default.target
`, agyHubBin)

	servicePath := filepath.Join(systemdDir, "agy-hub-daemon.service")
	_ = os.WriteFile(servicePath, []byte(serviceContent), 0644)

	fmt.Printf("✓ Serviço agy-hub-daemon criado em: %s\n", servicePath)
	fmt.Println("\nPara iniciar o daemon em background execute:")
	fmt.Println("  systemctl --user daemon-reload && systemctl --user enable --now agy-hub-daemon.service")
}

func runResetCooldown(args []string) {
	target := ""
	if len(args) > 0 {
		target = args[0]
	}

	realHome := discovery.GetRealHome()
	profiles, _ := discovery.DiscoverProfiles()
	targetEmails := make(map[string]bool)

	for _, p := range profiles {
		if target == "" || target == "all" || target == "*" ||
			strings.EqualFold(p.ID, target) ||
			strings.EqualFold(p.Alias, target) ||
			(p.Email != "" && strings.EqualFold(p.Email, target)) {
			if p.Email != "" {
				targetEmails[strings.ToLower(p.Email)] = true
			}
		}
	}

	cliProxyDir := filepath.Join(realHome, ".cli-proxy-api")
	files, _ := filepath.Glob(filepath.Join(cliProxyDir, "antigravity-*.json"))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(data, &payload); err == nil {
			email, _ := payload["email"].(string)
			emailLower := strings.ToLower(email)
			if target != "" && target != "all" && target != "*" && !targetEmails[emailLower] && !strings.Contains(f, target) {
				continue
			}
			payload["disabled"] = false
			if encoded, err := json.MarshalIndent(payload, "", "  "); err == nil {
				_ = os.WriteFile(f, encoded, 0644)
			}
		}
	}
	// Notify daemon via socket if running
	conn, err := net.DialTimeout("unix", daemon.GetDefaultSocketPath(), 200*time.Millisecond)
	if err != nil {
		conn, err = net.DialTimeout("unix", "/tmp/agy-hub.sock", 200*time.Millisecond)
	}
	if err == nil {
		if target != "" {
			fmt.Fprintf(conn, "reset-cooldown %s\n", target)
		} else {
			fmt.Fprintf(conn, "reset-cooldown\n")
		}
		conn.Close()
	}
	s := syncer.NewSyncer()
	_ = s.RestartProxyService()
	if target != "" {
		fmt.Printf("✓ Cooldown liberado para a conta '%s' e reativada no CLI-Proxy/Hermes.\n", target)
	} else {
		fmt.Println("✓ Todas as contas liberadas do cooldown e reativadas no CLI-Proxy/Hermes.")
	}
}

func runAuto(args []string) {
	isPrintMode := false
	for _, arg := range args {
		if arg == "-p" || arg == "--print" || arg == "--prompt" {
			isPrintMode = true
			break
		}
	}

	if isPrintMode {
		runExec(args)
		return
	}

	state, err := getClusterState()
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠️ [agy-hub] Falha na telemetria: %v (usando padrão)\n", err)
		execDefault(args)
		return
	}

	orch := orchestrator.NewOrchestrator()
	pwd, _ := os.Getwd()

	// Cross-Profile Session Resume: se invocado com -c ou --continue,
	// descobre qual conta possui a sessão modificada mais recentemente e retoma nela.
	isContinue := false
	for _, arg := range args {
		if arg == "-c" || arg == "--continue" {
			isContinue = true
			break
		}
	}

	if isContinue && state != nil && len(state.Profiles) > 0 {
		var mostRecentProfile *model.Profile
		var latestTime time.Time
		for _, p := range state.Profiles {
			if p.LastActive.After(latestTime) {
				latestTime = p.LastActive
				mostRecentProfile = p
			}
		}
		if mostRecentProfile != nil {
			fmt.Fprintf(os.Stderr, "⚡ [AGY Router] Retomando sessão mais recente em: %s (%s) · Última atividade: %s\n",
				mostRecentProfile.Alias, mostRecentProfile.ID, latestTime.Format("15:04:05"))
			if err := orch.ExecShell(mostRecentProfile, args); err != nil {
				fmt.Fprintf(os.Stderr, "Erro ao retomar sessão: %v\n", err)
				os.Exit(1)
			}
			return
		}
	}

	best, reason, err := orch.SelectBestWithWorkspace(state.Profiles, orchestrator.StrategyRoundRobin, pwd)
	if err != nil || best == nil {
		fmt.Fprintf(os.Stderr, "⚠️ [agy-hub] Falha ao selecionar conta: %v (usando padrão)\n", err)
		execDefault(args)
		return
	}

	fmt.Fprintf(os.Stderr, "⚡ [AGY Router] Conta ativa: %s (%s) · %s\n", best.Alias, best.ID, reason)
	if err := orch.ExecShell(best, args); err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao iniciar sessão: %v\n", err)
		os.Exit(1)
	}
}

func execDefault(args []string) {
	realHome := discovery.GetRealHome()
	agyProfileBin := filepath.Join(realHome, ".local/bin/agy-profile")
	fullArgs := append([]string{"agy-profile", "default"}, args...)
	_ = syscall.Exec(agyProfileBin, fullArgs, os.Environ())
}

func runLeaderboard(args []string) {
	store, err := metrics.NewStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao abrir banco de métricas: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	entries, err := store.GetLeaderboard()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao consultar ranking: %v\n", err)
		os.Exit(1)
	}

	if len(entries) == 0 {
		fmt.Println("ℹ️ Nenhuma execução registrada no histórico até o momento.")
		return
	}

	fmt.Println("🏆 AGY-HUB // LEADERBOARD DE CONTAS E AGENTES")
	fmt.Println(strings.Repeat("─", 78))
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "POS\tCONTA\tRUNS\tVITÓRIAS\tWIN RATE\tDURAÇÃO MÉD\tDIFF TOTAL")
	fmt.Fprintln(w, "---\t-----\t----\t--------\t--------\t-----------\t----------")
	for i, e := range entries {
		medals := []string{"🥇", "🥈", "🥉"}
		pos := fmt.Sprintf("#%d", i+1)
		if i < len(medals) {
			pos = medals[i]
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%.1f%%\t%dms\t+%dB\n",
			pos, e.ProfileID, e.TotalRuns, e.Wins, e.WinRate, e.AvgDurationMs, e.TotalDiffBytes)
	}
	_ = w.Flush()
	fmt.Println(strings.Repeat("─", 78))
}

func printHelp() {
	fmt.Println(`⚡ AGY-HUB // Antigravity & Hermes Multi-Account Orchestrator

COMANDOS:
  agy-hub                    Inicia o painel TUI interativo (default)
  agy-hub tui                Inicia o painel TUI interativo
  agy-hub daemon             Inicia daemon residente via Unix Socket (/tmp/agy-hub.sock)
  agy-hub status [--bar]     Exibe tabela com status das contas e proxy (--bar para Waybar/Tmux)
  agy-hub leaderboard        Exibe ranking de vitórias e acurácia por conta
  agy-hub which              Mostra qual conta seria selecionada pelo orquestrador agora (com afinidade KV)
  agy-hub exec <prompt>      Orquestra e despacha tarefa com failover em voo
  agy-hub sync               Sincroniza tokens das contas para o CLI-Proxy/Hermes
  agy-hub install-systemd    Gera timer systemd para sincronização periódica
  agy-hub install-daemon-systemd Gera serviço systemd para daemon em background

FLAGS PARA EXEC:
  -a, --account <id|alias>   Força execução em conta específica (ex: account3, agy-2)
  -s, --strategy <strat>     Estratégia: least-loaded (default), round-robin, priority
  -b, --detach               Executa em segundo plano com notificação desktop (notify-send)
  --swarm <n>                Executa prompt em N contas simultâneas em paralelo (1 conta por worker)
  --no-failover              Desativa retry automático caso ocorra 429

TECLAS NA TUI:
  [Enter]                    Abre sessão interativa no perfil selecionado
  [d]                        Dispatch de prompt com orquestrador automático
  [l]                        Live Log Viewer (streaming do cli.log com destaque de 429)
  [L]                        Dispara login OAuth interativo para a conta selecionada
  [s]                        Sincroniza as contas no CLI-Proxy
  [p]                        Reinicia o serviço cli-proxy-api
  [r]                        Atualiza métricas imediatamente
  [q]                        Sair`)
}

