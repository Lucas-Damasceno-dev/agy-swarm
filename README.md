# ⚡ agy-swarm

Orquestrador nativo em Go para enxame de agentes de IA operando sob o paradigma de **Pipeline de Execução Especulativa e Verificação Adversarial**.

Projetado para extrair o máximo *throughput* e inteligência técnica das **10 contas Google AI Pro** (`default`, `account2`..`account10`), orquestrando processos efêmeros (*one-shot*), isolamento estrito via **Git Worktrees**, pool dinâmico de leases com failover automático em HTTP 429, disjuntor **Circuit Breaker & Doom Loop Guard** e telemetria viva via **Tmux**.

---

## 🏛️ Filosofia: Pipeline Adversarial vs. "Mesa Redonda Humana"

Em vez de manter 5 sessões interativas abertas permanentemente trocando opiniões em chat textual, o `agy-swarm` modela o enxame como uma **Máquina de Estados Determinística (DAG)** dividida em 4 estágios matematicamente verificáveis:

```
                     [ TAREFA DO USUÁRIO ]
                               │
                               ▼
              ┌─────────────────────────────────┐
              │ ESTÁGIO 1: Especificação (Arch) │ ──► Define Interfaces, Tipos e Contratos
              └────────────────┬────────────────┘     (Sem implementar a lógica final)
                               │
                               ▼
              ┌─────────────────────────────────┐
              │ ESTÁGIO 2: Red-Team TDD (QA+Sec)│ ──► Escreve testes (*_test.go) que DEVEM
              └────────────────┬────────────────┘     falhar agora contra os stubs vazios
                               │
            ┌──────────────────┼──────────────────┐
            ▼                  ▼                  ▼
     [ Worktree A ]     [ Worktree B ]     [ Worktree C ]
     Viés: Hacker       Viés: Arquitetura  Viés: Resiliência
     (Conta Pool #1)    (Conta Pool #2)    (Conta Pool #3)
            │                  │                  │
            └──────────────────┼──────────────────┘
                               ▼
              ┌─────────────────────────────────┐
              │ ESTÁGIO 3: Filtro Frio (Go CLI) │ ──► Executa -test-cmd (ex: go test -race)
              └────────────────┬────────────────┘     Quem não compila/passa é descartado
                               │
                               ▼
              ┌─────────────────────────────────┐
              │ ESTÁGIO 4: Maestro / Merge Gate │ ──► Audita os git diffs sobreviventes,
              └─────────────────────────────────┘     seleciona o mais limpo e integra no HEAD
```

---

## ⚡ As 5 Diferenças Estruturais

1. **Agentes Efêmeros (*One-Shot*) com Isolamento PGID (`killpg`)**:
   - Fim do envio cego de comandos via `tmux send-keys` para agentes persistentes.
   - O orquestrador Go dispara processos efêmeros sob demanda (`exec.CommandContext`), isolados em seu próprio Process Group (`Setpgid: true`), com término cirúrgico via `Wait()` e eliminação total de processos zumbis ou vazamento de CPU.
   - Cada turno começa com contexto limpo, sem acúmulo de lixo na janela de tokens.

2. **Pool Dinâmico de Leases (10 Contas Google AI Pro) com Failover em 429**:
   - Desacoplamento total entre papéis lógicos e contas físicas.
   - O `lease.Broker` consulta o daemon `agy-hub` (`/tmp/agy-hub.sock`) e aloca a conta mais saudável (`WeeklyPercent` 70% + `FiveHourPercent` 30%) no momento da execução.
   - **Failover automático em 429:** Se uma conta atingir limite de taxa ou cota, o Go marca quarentena temporária e relança a mesma etapa em outra conta saudável em ~2 segundos na mesma worktree.

3. **Injeção Direta de Contexto (Zero Tokens Gastos com Mensageria no LLM)**:
   - O LLM nunca chama o `ai-memory` diretamente via bash ou tool calls.
   - O binário Go lê o histórico e contratos do `ai-memory`, monta o `prompt-%s.txt` com permissão estrita `0600` contendo todo o contexto mastigado e entrega pronto para o agente focar 100% em código e testes.
   - Ao encerrar, o Go lê o resultado/diff e publica no `ai-memory` para auditoria contínua.

4. **Circuit Breaker & Doom Loop Guard**:
   - Inspirado no motor de guardrails do LoopForge: calcula *fingerprints* criptográficos SHA-256 (`diff + testOutput`).
   - Se 2 tentativas consecutivas produzirem o mesmo erro sem alteração funcional no código, o disjuntor abre imediatamente, interrompendo *doom loops* estéreis e preservando suas cotas.

5. **Layout Tmux Repensado (UX de 3 Zonas)**:
   - **Painel Esquerdo (50% largura - Foco Humano):** Maestro interativo (`swarm-lead`) onde você comanda e aprova planos.
   - **Painel Superior Direito (50% largura, 35% altura - Telemetria Viva):** `agy-swarm dashboard --watch` rodando em loop contínuo, exibindo as 10 contas, status de cotas (5h e Semanal), leases ativos e estágio do pipeline.
   - **Painel Inferior Direito (50% largura, 65% altura - Viewport Dinâmico):** Exibe a execução e logs do worker efêmero ativo naquele momento.

---

## 🚀 Comandos

```bash
# Executar o Pipeline Adversarial de 4 Estágios (Contrato -> TDD -> Best-of-N -> Merge):
agy-swarm pipeline -test-cmd "go test ./..." "Implementar endpoint de health check"

# Pipeline com merge automático da solução vencedora aprovada no Quality Gate:
agy-swarm pipeline -auto-merge -test-cmd "go test -race ./..." "Criar middleware JWT"

# Simulação de pipeline sem disparar chamadas ou alterar branches (Dry-Run):
agy-swarm pipeline -dry-run "Refatorar módulo de autenticação"

# Iniciar sessão Tmux no Layout de 3 Zonas:
agy-swarm "Refatorar camada de banco de dados"

# Dashboard interativo com telemetria viva das 10 contas e leases ativos:
agy-swarm dashboard
agy-swarm dashboard --watch  # Atualização contínua a cada 2s

# Inspecionar diffs cumulativos dos especialistas contra o HEAD:
agy-swarm diff

# Visualizar status de conexões, cotas e caixas postais:
agy-swarm status

# Reconectar a uma sessão existente:
agy-swarm attach

# Encerrar sessão e executar teardown seguro de worktrees:
agy-swarm stop [--force]

# Limpeza de worktrees residuais:
agy-swarm clean [--force]
```

---

## 🛠️ Build & Instalação

```bash
make build    # Compila binário otimizado em bin/agy-swarm
make test     # Executa suíte de testes unitários (62+ testes)
make install  # Instala binário em ~/.local/bin/agy-swarm
make clean    # Remove artefatos temporários de compilação
```
