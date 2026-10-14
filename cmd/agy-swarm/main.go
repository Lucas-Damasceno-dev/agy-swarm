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
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/memory"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/multiplexer"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/notify"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/quota"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/tmux"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/worktree"
)

var (
	flagSession   = flag.String("session", "agy-swarm", "Nome da sessão tmux")
	flagRoles     = flag.String("roles", "", "Lista de papéis separados por vírgula (ex: arch,hacker,qa)")
	flagCount     = flag.Int("n", 0, "Número total de agentes a inicializar (1 a 10)")
	flagTestCmd   = flag.String("test-cmd", "", "Comando de teste para o Quality Gate de merge (ex: go test ./...)")
	flagDryRun    = flag.Bool("dry-run", false, "Simula criação sem iniciar o tmux")
	flagForce     = flag.Bool("force", false, "Força remoção de worktrees com alterações pendentes")
	flagDetach    = flag.Bool("detach", false, "Mantém a sessão rodando em segundo plano após fechar o terminal")
	flagAutoMerge = flag.Bool("auto-merge", false, "Executa integração sequencial automaticamente quando testes passarem")
)

func usage() {
	fmt.Fprintf(os.Stderr, `⚡ AGY-SWARM // Terminal Multi-Agent Orchestrator
Uso:
  agy-swarm [flags] ["tarefa/desafio inicial"]
  agy-swarm stop [--force]
  agy-swarm attach
  agy-swarm diff
  agy-swarm dashboard
  agy-swarm status
  agy-swarm clean [--force]

Flags:
  -session string    Nome da sessão tmux (padrão: "agy-swarm")
  -roles string      Subconjunto de papéis (ex: "arch,qa" ou "arch,hacker,sec")
  -n int             Quantidade de agentes (1 a 10)
  -test-cmd string   Comando para Quality Gate antes de cada merge
  -auto-merge        Executa integração sequencial automaticamente quando testes passarem
  -force             Força limpeza de worktrees sujos
  -detach            Mantém a sessão rodando em segundo plano após fechar o terminal
  -dry-run           Exibe planejamento de splits sem executar
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
		case "help", "-h", "--help":
			usage()
			return
		}
	}

	flag.Parse()
	prompt := strings.Join(flag.Args(), " ")

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

	// Se houver prompt, pré-alimenta as caixas dos especialistas e gera os arquivos de boot
	if cfg.Prompt != "" {
		if err := memClient.WriteWikiPage("swarm-lead", "debates/current.md", fmt.Sprintf("# Debate: %s\n\nData: %s\n", cfg.Prompt, time.Now().Format(time.RFC3339))); err != nil {
			fmt.Fprintf(os.Stderr, "Aviso: falha ao registrar debate no ai-memory: %v\n", err)
		}
		for _, r := range cfg.Roles {
			if r.Type != config.RoleLead {
				if err := memClient.SendMessage("swarm-lead", r.Project, fmt.Sprintf("Pauta de Debate do Enxame:\n\"%s\"\nPor favor, envie sua análise técnica defendendo a área de %s.", cfg.Prompt, r.Name)); err != nil {
					fmt.Fprintf(os.Stderr, "Aviso: falha ao enviar pauta para %s: %v\n", r.Project, err)
				}
			}
		}
	}

	// Prepara os prompts de inicialização em arquivos temporários protegidos (permissão 0600 em runtime 0700)
	runtimeDir, err := ensureRuntimeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao provisionar runtime seguro: %v\n", err)
		os.Exit(1)
	}

	promptFiles := make(map[config.RoleType]string)
	for _, r := range cfg.Roles {
		var pText string
		if cfg.Prompt != "" {
			if r.Type == config.RoleLead {
				pText = config.GenerateInitialMaestroPrompt(cfg.Roles, cfg.Prompt)
			} else {
				pText = config.GenerateInitialSpecialistPrompt(r, cfg.Prompt)
			}
		}
		tmpFile := filepath.Join(runtimeDir, fmt.Sprintf("prompt-%s.txt", r.Type))
		if err := writeSecureFile(tmpFile, []byte(pText)); err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao gravar prompt protegido para %s: %v\n", r.Type, err)
			os.Exit(1)
		}
		// Expurgo atômico dos prompts após o boot (Zero-Leak)
		defer os.Remove(tmpFile)
		promptFiles[r.Type] = tmpFile
	}

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

	// Inicializa os processos nos painéis com staggering
	for i, r := range cfg.Roles {
		path := wtPaths[r.Type]
		var launchCmd string
		tmpFile := promptFiles[r.Type]
		if cfg.Prompt != "" {
			launchCmd = fmt.Sprintf("cd %s && agy-profile %s -i \"$(cat %s)\"", path, r.Profile, tmpFile)
		} else {
			launchCmd = fmt.Sprintf("cd %s && agy-profile %s", path, r.Profile)
		}
		// Aguarda o painel específico estar pronto antes do envio de comandos
		PollUntil(1*time.Second, 10*time.Millisecond, func() bool {
			return driver.PaneExists(r.Type)
		})
		if err := driver.SendKeys(r.Type, launchCmd); err != nil {
			fmt.Fprintf(os.Stderr, "Aviso: falha ao enviar comando para %s: %v\n", r.Type, err)
		}

		// Staggering: intervalo de 1.5s entre inícios para evitar thundering herd de CPU, disco e OAuth
		if i < len(cfg.Roles)-1 {
			time.Sleep(1500 * time.Millisecond)
		}
	}

	// Descarta potenciais modais de boas-vindas com Escape após prontidão de todos os painéis
	go func() {
		PollUntil(3*time.Second, 50*time.Millisecond, func() bool {
			for _, r := range cfg.Roles {
				if !driver.PaneExists(r.Type) {
					return false
				}
			}
			return true
		})
		for _, r := range cfg.Roles {
			_ = driver.SendControl(r.Type, "Escape")
			_ = driver.SendControl(r.Type, "Escape")
		}
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
	roles := config.DefaultRoles()
	memClient := memory.NewClient()
	watcher := memory.NewWatcher(memClient, driver, roles)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watcher.Start(ctx)

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

	wtMgr := worktree.NewManager(cwd, config.DefaultRoles())
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

	wtMgr := worktree.NewManager(cwd, config.DefaultRoles())
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
	for _, r := range config.DefaultRoles() {
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
