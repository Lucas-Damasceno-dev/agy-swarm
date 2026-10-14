# ⚡ agy-swarm

Orquestrador terminal em Go para coordenação em enxame de até 10 contas simultâneas do **Antigravity CLI (agy)** em contas Google AI Pro distintas (`default`, `account2`..`account10`), utilizando **Tmux**, mensageria nativa do **ai-memory**, telemetria de cotas do **agy-hub**, barreira de sincronização de debate e isolamento por **Git Worktrees**.

---

## 🏛️ Topologia & Papéis (Até 10 Contas)

| Pane | Papel | Perfil AGY | Projeto ai-memory | Foco / Viés Ontológico |
| :--- | :--- | :--- | :--- | :--- |
| **0** | **Maestro** | `default` | `swarm-lead` | Interface humana, barreira de sincronização, Quality Gate e Merge Master. |
| **1** | **Arquiteto** | `account2` | `swarm-arch` | Padrões de projeto, modularidade, desacoplamento e interfaces limpas. |
| **2** | **Segurança/SRE**| `account3` | `swarm-sec` | Defesa em profundidade, validações rígidas, edge cases e resiliência. |
| **3** | **QA / Testes** | `account4` | `swarm-qa` | Testabilidade determinística, cobertura de código e regressão zero. |
| **4** | **Pragmático** | `account5` | `swarm-hacker`| MVP enxuto, velocidade de entrega, simplicidade e código direto. |
| *Pool* | **Reviewer** | `account6` | `swarm-reviewer`| Code review semântico, tipagem estrita e convenções idiomáticas. |
| *Pool* | **Tech Writer** | `account7` | `swarm-doc` | Documentação técnica, especificações de arquitetura e ADRs. |
| *Pool* | **Performance** | `account8` | `swarm-bench` | Profiling de CPU/RAM, benchmarks e escalabilidade assintótica. |
| *Pool* | **Refactor** | `account9` | `swarm-refactor`| Limpeza de código morto, unificação de duplicatas e higienização. |
| *Pool* | **Red Team** | `account10`| `swarm-adversary`| Testes de estresse adversarial, quebra de contratos e fuzzing. |

---

## 🚀 Comandos

```bash
# Iniciar enxame padrão com debate imediato:
agy-swarm "Refatorar camada de banco de dados para suportar múltiplos drivers"

# Iniciar interativo com os 5 agentes aguardando no Maestro:
agy-swarm

# Iniciar com subconjunto de especialistas:
agy-swarm --roles arch,qa "Especificar contratos de teste da API"

# Iniciar em segundo plano permanente (não mata ao fechar o terminal):
agy-swarm -detach "Refatorar módulos"

# Dashboard interativo com telemetria de cotas do cluster e estado dos agentes:
agy-swarm dashboard

# Inspecionar diffs cumulativos de todos os especialistas contra o HEAD:
agy-swarm diff

# Iniciar com integração automática de merge caso testes passem:
agy-swarm -auto-merge "Otimizar consultas lentas"

# Visualizar status de conexões, tmux, cotas e caixas postais:
agy-swarm status

# Reconectar a uma sessão swarm existente:
agy-swarm attach

# Encerrar sessão e executar teardown seguro de worktrees:
agy-swarm stop

# Limpeza forçada de worktrees residuais:
agy-swarm clean --force
```

---

## 🛠️ Build & Instalação

```bash
make build    # Compila binário em bin/agy-swarm
make test     # Executa suíte de testes unitários
make install  # Instala binário em ~/.local/bin/agy-swarm
make clean    # Remove artefatos de compilação
```
