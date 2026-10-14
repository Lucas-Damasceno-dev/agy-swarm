package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/agyhub"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/lease"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/memory"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/multiplexer"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/notify"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/pipeline"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/quota"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/tmux"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/worktree"
)

var (
	flagSession   = flag.String("session", "agy-swarm", "Nome da sessão tmux")
	flagRoles     = flag.String("roles", "", "Lista de papéis separados por vírgula (ex: arch,hacker,qa)")
	flagCount     = flag.Int("n", 0, "Número total de agentes a inicializar (1 a 10)")
	flagTestCmd   = flag.String("test-cmd", "", "Comando de teste para o Quality Gate de merge (ex: go test ./...)")
	flagPipeline  = flag.Bool("pipeline", false, "Executa o pipeline determinístico adversarial de 4 estágios")
	flagDryRun    = flag.Bool("dry-run", false, "Simula criação sem iniciar o tmux")
	flagForce     = flag.Bool("force", false, "Força remoção de worktrees com alterações pendentes")
	flagDetach    = flag.Bool("detach", false, "Mantém a sessão rodando em segundo plano após fechar o terminal")
	flagAutoMerge = flag.Bool("auto-merge", false, "Executa integração sequencial automaticamente quando testes passarem")
)

func usage() {
	fmt.Fprintf(os.Stderr, `⚡ AGY-SWARM // Terminal Multi-Agent Orchestrator
Uso:
  agy-swarm dispatch [flags] ["tarefa/desafio"]
  agy-swarm pipeline [flags] ["tarefa/desafio"]
  agy-swarm [flags] ["tarefa/desafio inicial"]
  agy-swarm stop [--force]
  agy-swarm attach
  agy-swarm diff
  agy-swarm dashboard [--watch]
  agy-swarm status
  agy-swarm clean [--force]

Flags:
  -pipeline          Executa o Pipeline de Verificação Adversarial (4 estágios: Contract -> Red-Team TDD -> Best-of-N -> Merge)
  -session string    Nome da sessão tmux (padrão: "agy-swarm")
  -roles string      Subconjunto de papéis (ex: "arch,qa" ou "arch,hacker,sec")
  -n int             Quantidade de agentes (1 a 10)
  -test-cmd string   Comando para Quality Gate antes de cada merge
  -auto-merge        Executa integração do vencedor automaticamente quando testes passarem
  -force             Força limpeza de worktrees sujos
  -detach            Mantém a sessão rodando em segundo plano após fechar o terminal
  -dry-run           Exibe planejamento sem executar
`)
	os.Exit(1)
}

// ensureRuntimeDir retorna o caminho de um diretório protegido exclusivo do usuário com permissão 0700.
func ensureRuntimeDir() (string, error) {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	var baseDir string
	if runtimeDir != "" {
		baseDir = filepath.Join(runtimeDir, "agy-swarm")
	} else {
		baseDir = filepath.Join(os.TempDir(), fmt.Sprintf("agy-swarm-run-%d", os.Getuid()))
	}

	if err := os.MkdirAll(baseDir, 0700); err != nil {
		baseDir, err = os.MkdirTemp("", "agy-swarm-*")
		if err != nil {
			return "", fmt.Errorf("falha ao provisionar diretório de runtime seguro: %w", err)
		}
	}

	if err := os.Chmod(baseDir, 0700); err != nil {
		return "", fmt.Errorf("falha ao aplicar permissão 0700 no runtime: %w", err)
	}

	return baseDir, nil
}

// pidFilePath resolve o caminho seguro do arquivo de PID da sessão.
func pidFilePath(session string) (string, error) {
	dir, err := ensureRuntimeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("%s.pid", session)), nil
}

// writeSecureFile grava dados de forma atômica e fail-closed com permissão 0600 e O_EXCL contra TOCTOU.
func writeSecureFile(path string, data []byte) error {
	_ = os.Remove(path) // Remove link ou arquivo pré-existente antes da criação atômica
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("falha ao criar arquivo protegido 0600 (%s): %w", path, err)
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("falha ao gravar dados em %s: %w", path, err)
	}
	return nil
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "pipeline":
			handlePipeline()
			return
		case "daemon":
			handleDaemon()
			return
		case "stop":
			handleStop()
			return
		case "attach":
			handleAttach()
			return
		case "diff":
			handleDiff()
			return
		case "dashboard":
			handleDashboard()
			return
		case "status":
			handleStatus()
			return
		case "clean":
			handleClean()
			return
		case "dispatch":
			handleDispatch()
			return
		case "help", "-h", "--help":
			usage()
			return
		}
	}

	flag.Parse()
	prompt := strings.Join(flag.Args(), " ")

	if *flagPipeline {
		handlePipeline()
		return
	}

	cfg, err := config.NewConfig(*flagSession, *flagRoles, *flagCount, prompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro de configuração: %v\n", err)
		os.Exit(1)
	}

	// Balanceia as contas dinamicamente por score de cota restante (semanal 70% + 5h 30%)
	hubClient := agyhub.NewClient(cfg.SocketPath)
	if hubClient.IsAvailable() {
		balancer := quota.NewBalancer(hubClient)
		if alloc, err := balancer.AssignClusterRoles(cfg.Roles); err == nil {
			cfg.Roles = alloc.Roles
			if len(alloc.StandbyPool) > 0 {
				fmt.Printf("🛡️ Pool de Hot-Standby: %d contas reservas prontas para rotação.\n", len(alloc.StandbyPool))
			}
		}
	}

	if *flagDryRun {
		fmt.Printf("⚡ Planejamento do Enxame (Dry-Run Ponderado por Cota):\n")
		fmt.Printf("• Sessão: %s\n", cfg.SessionName)
		fmt.Printf("• Diretório: %s\n", cfg.Workdir)
		fmt.Printf("• Prompt Inicial: %q\n", cfg.Prompt)
		fmt.Printf("• Agentes Ativos (%d):\n", len(cfg.Roles))
		for i, r := range cfg.Roles {
			fmt.Printf("  [%d] %s %-14s | Perfil: %-8s | ai-memory: %-12s | WT: %s\n",
				i, r.Icon, r.Name, r.Profile, r.Project, cfg.WorktreePath(r.Type))
		}
		return
	}

	cfg.Detach = *flagDetach

	tmuxDriver := tmux.NewDriver(cfg.SessionName, cfg.Workdir)
	tmuxDriver.Detach = *flagDetach
	var driver multiplexer.Multiplexer = tmuxDriver
	if driver.SessionExists() {
		fmt.Printf("⚠️ Sessão Tmux '%s' já está em execução.\n", cfg.SessionName)
		fmt.Printf("Para reconectar: agy-swarm attach\n")
		fmt.Printf("Para encerrar:   agy-swarm stop\n")
		os.Exit(1)
	}

	fmt.Printf("⚡ Provisionando Git Worktrees para %d especialistas...\n", len(cfg.Roles))
	wtMgr := worktree.NewManager(cfg.Workdir, cfg.Roles)
	wtPaths, err := wtMgr.ProvisionAll()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao provisionar worktrees: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("📬 Inicializando caixas postais no ai-memory...")
	memClient := memory.NewClient()
	if err := memClient.EnsureProjects(cfg.Roles); err != nil {
		fmt.Printf("Aviso: falha ao registrar projetos no ai-memory: %v\n", err)
	}

	// Se houver prompt, registra o debate no ai-memory
	if cfg.Prompt != "" {
		if err := memClient.WriteWikiPage("swarm-lead", "debates/current.md", fmt.Sprintf("# Debate: %s\n\nData: %s\n", cfg.Prompt, time.Now().Format(time.RFC3339))); err != nil {
			fmt.Fprintf(os.Stderr, "Aviso: falha ao registrar debate no ai-memory: %v\n", err)
		}
	}

	// Prepara os prompts de inicialização em arquivos temporários protegidos (permissão 0600 em runtime 0700)
	runtimeDir, err := ensureRuntimeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao provisionar runtime seguro: %v\n", err)
		os.Exit(1)
	}

	maestroRole := cfg.Roles[0]
	var pText string
	if cfg.Prompt != "" {
		pText = config.GenerateInitialMaestroPrompt(cfg.Roles, cfg.Prompt)
	} else {
		pText = config.GenerateInteractiveMaestroPrompt(cfg.Roles)
	}

	tmpFile := filepath.Join(runtimeDir, fmt.Sprintf("prompt-%s.txt", maestroRole.Type))
	if err := writeSecureFile(tmpFile, []byte(pText)); err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao gravar prompt protegido para o Maestro: %v\n", err)
		os.Exit(1)
	}
	maestroPrompt := tmpFile

	fmt.Printf("🖥️ Montando layout Tmux (Sessão: %s)...\n", cfg.SessionName)
	if err := driver.BuildLayout(cfg.Roles); err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao construir layout Tmux: %v\n", err)
		os.Exit(1)
	}

	// Atualiza status-bar com informações agregadas do cluster
	if hubClient.IsAvailable() {
		if st, err := hubClient.GetClusterState(); err == nil {
			driver.UpdateStatusBar(fmt.Sprintf("Cluster: %dM/%dM (%d%%)", st.ClusterRemaining/1000000, st.ClusterQuotaTokens/1000000, st.ClusterQuotaPercent))
		}
	}

	// Aguarda prontidão da sessão Tmux via polling determinístico
	_ = PollUntil(3*time.Second, 20*time.Millisecond, driver.SessionExists)

	// Inicializa os processos nos painéis do Layout de 3 Zonas
	// 1. Pane 0 (Maestro): Inicia agy com o perfil do Maestro no repositório principal
	maestroPath := wtPaths[maestroRole.Type]
	maestroCmd := fmt.Sprintf("cd %s && PROMPT=\"$(cat %s)\" && rm -f %s && agy-profile %s -i \"$PROMPT\"", maestroPath, maestroPrompt, maestroPrompt, maestroRole.Profile)

	PollUntil(1*time.Second, 10*time.Millisecond, func() bool {
		return driver.PaneExists(maestroRole.Type)
	})
	if err := driver.SendKeys(maestroRole.Type, maestroCmd); err != nil {
		fmt.Fprintf(os.Stderr, "Aviso: falha ao enviar comando para o Maestro: %v\n", err)
	}

	// 2. Pane Top-Right (Telemetria Viva): Dispara dashboard contínuo
	PollUntil(1*time.Second, 10*time.Millisecond, func() bool {
		return driver.PaneExists("telemetry")
	})
	_ = driver.SendKeys("telemetry", "agy-swarm dashboard --watch")

	// 3. Pane Bottom-Right (Viewport Dinâmico): Se houver prompt, executa o pipeline diretamente
	PollUntil(1*time.Second, 10*time.Millisecond, func() bool {
		return driver.PaneExists("viewport")
	})

	// Registra o pane_id do Viewport para suporte ao comando 'agy-swarm dispatch'
	if vpInfo, ok := tmuxDriver.Panes["viewport"]; ok && vpInfo.ID != "" {
		vpFile := filepath.Join(runtimeDir, fmt.Sprintf("%s-viewport.pane", cfg.SessionName))
		_ = writeSecureFile(vpFile, []byte(vpInfo.ID))
	}

	if cfg.Prompt != "" {
		testCmd := *flagTestCmd
		if testCmd == "" {
			testCmd = detectDefaultTestCmd(cfg.Workdir)
		}
		testCmdFlag := fmt.Sprintf(" -test-cmd %q", testCmd)
		autoMergeFlag := ""
		if *flagAutoMerge {
			autoMergeFlag = " -auto-merge"
		}
		viewportCmd := fmt.Sprintf("agy-swarm pipeline%s%s %q", testCmdFlag, autoMergeFlag, cfg.Prompt)
		_ = driver.SendKeys("viewport", viewportCmd)
	} else {
		_ = driver.SendKeys("viewport", "clear && echo '⚡ AGY-SWARM // VIEWPORT DINÂMICO DOS WORKERS' && echo '• Aguardando despacho do Maestro ou execute:' && echo '  agy-swarm dispatch \"sua tarefa\"' && echo ''")
	}

	// Descarta potenciais modais de boas-vindas com Escape no Maestro
	go func() {
		time.Sleep(1 * time.Second)
		_ = driver.SendControl(maestroRole.Type, "Escape")
		_ = driver.SendControl(maestroRole.Type, "Escape")
	}()

	// Dispara o daemon supervisor em segundo plano
	if err := startDaemonProcess(cfg.SessionName, cfg.Workdir, *flagDetach); err != nil {
		fmt.Fprintf(os.Stderr, "Aviso: falha ao iniciar supervisor daemon: %v\n", err)
	}

	notify.SendDesktop("agy-swarm", fmt.Sprintf("Enxame ativo com %d agentes!", len(cfg.Roles)))

	fmt.Printf("✨ Enxame ativo! Conectando ao Tmux...\n")

	// Se não estiver dentro do tmux, faz o attach
	if os.Getenv("TMUX") == "" {
		attachCmd := exec.Command("tmux", "attach-session", "-t", cfg.SessionName)
		attachCmd.Stdin = os.Stdin
		attachCmd.Stdout = os.Stdout
		attachCmd.Stderr = os.Stderr
		_ = attachCmd.Run()
	} else {
		fmt.Printf("Você já está dentro de uma sessão Tmux. Alterne para a sessão '%s' com:\n", cfg.SessionName)
		fmt.Printf("  tmux switch-client -t %s\n", cfg.SessionName)
	}
}

func startDaemonProcess(session, workdir string, detach bool) error {
	binaryPath, err := os.Executable()
	if err != nil {
		binaryPath = "agy-swarm"
	}

	args := []string{"daemon", "-session", session}
	if detach {
		args = append(args, "-detach")
	}

	cmd := exec.Command(binaryPath, args...)
	cmd.Dir = workdir
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("falha ao iniciar processo daemon: %w", err)
	}

	pidFile, err := pidFilePath(session)
	if err != nil {
		return fmt.Errorf("falha ao obter caminho de PID: %w", err)
	}

	if err := writeSecureFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid))); err != nil {
		return fmt.Errorf("falha ao gravar PID seguro: %w", err)
	}

	return nil
}

func handleDaemon() {
	flag.Parse()
	session := *flagSession
	cwd, _ := os.Getwd()

	var driver multiplexer.Multiplexer = tmux.NewDriver(session, cwd)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Loop do daemon: monitora se a sessão tmux existe e se há clientes conectados
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	noClientTicks := 0

	for {
		select {
		case <-sigChan:
			return
		case <-ticker.C:
			if !driver.SessionExists() {
				return // encerra daemon se o tmux morreu
			}
			// Se não for modo --detach explícito, verifica se o terminal desconectou (Super+C / killactive)
			if !*flagDetach {
				out, err := exec.Command("tmux", "list-clients", "-t", session).Output()
				if err == nil && len(strings.TrimSpace(string(out))) == 0 {
					noClientTicks++
					if noClientTicks >= 3 { // 3 ticks * 3s = 9 segundos sem nenhum cliente conectado
						handleStop()
						return
					}
				} else {
					noClientTicks = 0
				}
			}
		}
	}
}

func handleStop() {
	session := "agy-swarm"
	cwd, _ := os.Getwd()
	var driver multiplexer.Multiplexer = tmux.NewDriver(session, cwd)

	// Mata o daemon se estiver rodando
	pidFile, err := pidFilePath(session)
	if err == nil {
		if data, err := os.ReadFile(pidFile); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
				if err := syscall.Kill(pid, syscall.SIGTERM); err == nil {
					fmt.Printf("✓ Daemon (PID %d) finalizado.\n", pid)
				}
			}
			_ = os.Remove(pidFile)
		}
	}

	if !driver.SessionExists() {
		fmt.Printf("Nenhuma sessão '%s' em execução.\n", session)
	} else {
		if err := driver.KillSession(); err != nil {
			fmt.Fprintf(os.Stderr, "Aviso ao encerrar sessão tmux: %v\n", err)
		} else {
			fmt.Printf("✓ Sessão Tmux '%s' encerrada.\n", session)
		}
	}

	force := false
	for _, a := range os.Args[2:] {
		if a == "--force" || a == "-f" {
			force = true
		}
	}

	// Limpeza de prompts protegidos e referências de pane residuais no runtime
	if dir, err := ensureRuntimeDir(); err == nil {
		matches, _ := filepath.Glob(filepath.Join(dir, "prompt-*.txt"))
		for _, m := range matches {
			_ = os.Remove(m)
		}
		vpMatches, _ := filepath.Glob(filepath.Join(dir, "*-viewport.pane"))
		for _, m := range vpMatches {
			_ = os.Remove(m)
		}
	}

	wtMgr := worktree.NewManager(cwd, config.AllRoles())
	if err := wtMgr.SafeCleanup(force); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️ %v\n", err)
	} else {
		fmt.Println("✓ Worktrees e branches efêmeros limpos com sucesso.")
	}
}

func handleAttach() {
	session := "agy-swarm"
	var driver multiplexer.Multiplexer = tmux.NewDriver(session, "")
	if !driver.SessionExists() {
		fmt.Fprintf(os.Stderr, "Sessão '%s' não encontrada.\n", session)
		os.Exit(1)
	}

	cmd := exec.Command("tmux", "attach-session", "-t", session)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}

func handleClean() {
	cwd, _ := os.Getwd()
	force := false
	for _, a := range os.Args[2:] {
		if a == "--force" || a == "-f" {
			force = true
		}
	}

	wtMgr := worktree.NewManager(cwd, config.AllRoles())
	if err := wtMgr.SafeCleanup(force); err != nil {
		fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ Worktrees limpos com sucesso.")
}

func handleStatus() {
	session := "agy-swarm"
	var driver multiplexer.Multiplexer = tmux.NewDriver(session, "")
	running := driver.SessionExists()

	fmt.Printf("⚡ ESTADO DO ENXAME (Sessão: %s)\n", session)
	if running {
		fmt.Println("• Tmux: ATIVO 🟢")
	} else {
		fmt.Println("• Tmux: INATIVO ⚪")
	}

	hubClient := agyhub.NewClient("")
	if hubClient.IsAvailable() {
		fmt.Println("• agy-hub Daemon: CONECTADO 🟢")
		if q, err := hubClient.GetQuarantinedAccounts(); err == nil && len(q) > 0 {
			fmt.Printf("  ⚠️ Contas em Quarentena / 429: %v\n", q)
		}
	} else {
		fmt.Println("• agy-hub Daemon: OFFLINE ⚪")
	}

	memClient := memory.NewClient()
	fmt.Println("\n📬 Caixas Postais ai-memory:")
	for _, r := range config.AllRoles() {
		msgs, err := memClient.ListPendingMessages(r.Project)
		count := 0
		if err == nil {
			count = len(msgs)
		}
		status := "idle"
		if count > 0 {
			status = fmt.Sprintf("%d pendente(s)", count)
		}
		fmt.Printf("  %s %-14s (%-12s): %s\n", r.Icon, r.Name, r.Project, status)
	}
}

func handlePipeline() {
	cwd, _ := os.Getwd()

	fs := flag.NewFlagSet("pipeline", flag.ExitOnError)
	pRoles := fs.String("roles", "", "Lista de papéis separados por vírgula")
	pCount := fs.Int("n", 0, "Número total de agentes (1 a 10)")
	pTestCmd := fs.String("test-cmd", "", "Comando de teste para o Quality Gate")
	pDryRun := fs.Bool("dry-run", false, "Simula execução sem tocar no git nem disparar LLMs")
	pAutoMerge := fs.Bool("auto-merge", false, "Executa integração automática do vencedor")

	args := os.Args[1:]
	if len(args) > 0 && args[0] == "pipeline" {
		args = args[1:]
	}
	_ = fs.Parse(args)

	prompt := strings.Join(fs.Args(), " ")
	if strings.TrimSpace(prompt) == "" {
		fmt.Fprintf(os.Stderr, "Erro: tarefa/desafio obrigatório para o pipeline adversarial.\nUso: agy-swarm pipeline [flags] \"tarefa\"\n")
		os.Exit(1)
	}

	roles, err := config.ParseRoles(*pRoles, *pCount)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro de papéis: %v\n", err)
		os.Exit(1)
	}

	hubClient := agyhub.NewClient("")
	broker := lease.NewBroker(hubClient)

	testCmd := *pTestCmd
	if testCmd == "" {
		testCmd = detectDefaultTestCmd(cwd)
	}

	cfg := pipeline.EngineConfig{
		RepoRoot:  cwd,
		Task:      prompt,
		TestCmd:   testCmd,
		DryRun:    *pDryRun,
		AutoMerge: *pAutoMerge,
		Roles:     roles,
	}

	engine := pipeline.NewPipelineEngine(cfg, broker, nil)

	fmt.Println("⚡ AGY-SWARM // INICIANDO PIPELINE DE VERIFICAÇÃO ADVERSARIAL")
	fmt.Printf("• Tarefa: %q\n", prompt)
	if cfg.TestCmd != "" {
		fmt.Printf("• Quality Gate (-test-cmd): %s\n", cfg.TestCmd)
	}
	fmt.Println(strings.Repeat("─", 68))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	results, err := engine.Execute(ctx)
	for _, res := range results {
		statusStr := "✅ CONCLUÍDO"
		if !res.Success {
			statusStr = "❌ FALHOU"
		}
		fmt.Printf("\n[%s] %s (%v)\n", res.Stage, statusStr, res.Duration.Round(time.Millisecond))
		if res.ArtifactMsg != "" {
			fmt.Printf("  • %s\n", res.ArtifactMsg)
		}
		if res.Winner != nil {
			fmt.Printf("  🏆 Vencedor: %s (%s) | Linhas: +%d/-%d\n", res.Winner.Role.Name, res.Winner.AccountID, res.Winner.LinesAdded, res.Winner.LinesDeleted)
		}
		if res.Error != nil {
			fmt.Printf("  ⚠️ Erro: %v\n", res.Error)
		}
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "\n❌ Pipeline falhou: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n✨ Pipeline de Verificação Adversarial concluído com sucesso!")
}

func handleDispatch() {
	cwd, _ := os.Getwd()
	fs := flag.NewFlagSet("dispatch", flag.ExitOnError)
	pTestCmd := fs.String("test-cmd", "", "Comando de teste para o Quality Gate")
	pAutoMerge := fs.Bool("auto-merge", false, "Executa integração automática do vencedor")

	args := os.Args[1:]
	if len(args) > 0 && args[0] == "dispatch" {
		args = args[1:]
	}
	_ = fs.Parse(args)

	task := strings.Join(fs.Args(), " ")
	if strings.TrimSpace(task) == "" {
		fmt.Fprintf(os.Stderr, "Erro: tarefa/desafio obrigatório.\nUso: agy-swarm dispatch [flags] \"tarefa\"\n")
		os.Exit(1)
	}

	session := "agy-swarm"
	runtimeDir, err := ensureRuntimeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao obter runtime: %v\n", err)
		os.Exit(1)
	}

	testCmd := *pTestCmd
	if testCmd == "" {
		testCmd = detectDefaultTestCmd(cwd)
	}
	testCmdFlag := fmt.Sprintf(" -test-cmd %q", testCmd)
	autoMergeFlag := ""
	if *pAutoMerge {
		autoMergeFlag = " -auto-merge"
	}
	cmdToSend := fmt.Sprintf("agy-swarm pipeline%s%s %q", testCmdFlag, autoMergeFlag, task)

	// Tenta despachar para o painel Viewport do Tmux
	vpFile := filepath.Join(runtimeDir, fmt.Sprintf("%s-viewport.pane", session))
	data, err := os.ReadFile(vpFile)
	if err == nil && len(strings.TrimSpace(string(data))) > 0 {
		paneID := strings.TrimSpace(string(data))
		if exec.Command("tmux", "display-message", "-p", "-t", paneID, "#{pane_id}").Run() == nil {
			_ = exec.Command("tmux", "send-keys", "-t", paneID, cmdToSend, "Enter").Run()
			fmt.Printf("⚡ Tarefa despachada para o Viewport Dinâmico (%s):\n  %q\n", paneID, task)
			return
		}
	}

	// Fallback se não encontrar o viewport no tmux
	fmt.Println("ℹ️ Painel Viewport não encontrado no Tmux. Executando pipeline localmente...")
	os.Args = append([]string{"agy-swarm", "pipeline", "-test-cmd", testCmd}, task)
	handlePipeline()
}

// detectDefaultTestCmd identifica se o repositório é Rust (Cargo.toml) ou Go (go.mod) para selecionar o Quality Gate padrão.
func detectDefaultTestCmd(repoRoot string) string {
	if _, err := os.Stat(filepath.Join(repoRoot, "Cargo.toml")); err == nil {
		return "cargo test --workspace"
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err == nil {
		return "go test ./..."
	}
	return "go test ./..."
}

