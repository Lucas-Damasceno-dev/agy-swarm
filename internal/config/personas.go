package config

import (
	"fmt"
	"strings"
)

// RoleType define o identificador do papel do agente.
type RoleType string

const (
	RoleLead      RoleType = "lead"
	RoleArch      RoleType = "arch"
	RoleSec       RoleType = "sec"
	RoleQA        RoleType = "qa"
	RoleHacker    RoleType = "hacker"
	RoleReviewer  RoleType = "reviewer"
	RoleDoc       RoleType = "doc"
	RoleBench     RoleType = "bench"
	RoleRefactor  RoleType = "refactor"
	RoleAdversary RoleType = "adversary"
)

// RoleConfig define os metadados de execução de um agente do enxame.
type RoleConfig struct {
	Type        RoleType
	Name        string
	Profile     string
	Project     string
	Icon        string
	Description string
	Bias        string
	Permission  string
}

// DefaultRoles retorna os 5 agentes padrão na ordem exata.
func DefaultRoles() []RoleConfig {
	return []RoleConfig{
		{
			Type:        RoleLead,
			Name:        "Maestro",
			Profile:     "default",
			Project:     "swarm-lead",
			Icon:        "👑",
			Description: "Líder técnico, moderador do debate, ponto de contato com o usuário e autoridade final de merge.",
			Bias:        "Sintetizador e pragmático. Foca na visão de produto, consenso prático e viabilidade.",
			Permission:  "prompt",
		},
		{
			Type:        RoleArch,
			Name:        "Arquiteto",
			Profile:     "account2",
			Project:     "swarm-arch",
			Icon:        "📐",
			Description: "Especialista em arquitetura, baixo acoplamento, modularidade e padrões de projeto.",
			Bias:        "Puxa sempre para interfaces limpas, separação rígida de camadas, extensibilidade e clareza estrutural.",
			Permission:  "always-proceed",
		},
		{
			Type:        RoleSec,
			Name:        "Seguranca-SRE",
			Profile:     "account3",
			Project:     "swarm-sec",
			Icon:        "🛡️",
			Description: "Especialista em segurança, auditoria, superfícies de ataque, resiliência e tratamento de falhas.",
			Bias:        "Puxa para validações rigorosas de inputs, integridade, fail-closed, mitigação de vulnerabilidades e observabilidade.",
			Permission:  "always-proceed",
		},
		{
			Type:        RoleQA,
			Name:        "QA-Testes",
			Profile:     "account4",
			Project:     "swarm-qa",
			Icon:        "🧪",
			Description: "Especialista em qualidade, cobertura de código, cenários adversos, harnesses e automação de testes.",
			Bias:        "Puxa para testabilidade determinística, casos de borda, zero regressões e mocks isolados.",
			Permission:  "always-proceed",
		},
		{
			Type:        RoleHacker,
			Name:        "Pragmatico",
			Profile:     "account5",
			Project:     "swarm-hacker",
			Icon:        "⚡",
			Description: "Hacker pragmático, desenvolvedor ágil focado em entrega enxuta, código direto e simplicidade.",
			Bias:        "Puxa para eliminação de over-engineering, código direto, prototipação rápida e menor número possível de linhas.",
			Permission:  "always-proceed",
		},
	}
}

// AllRoles retorna todas as 10 personas possíveis para suportar clusters completos de 10 contas.
func AllRoles() []RoleConfig {
	roles := DefaultRoles()
	extended := []RoleConfig{
		{
			Type:        RoleReviewer,
			Name:        "Code-Reviewer",
			Profile:     "account6",
			Project:     "swarm-reviewer",
			Icon:        "🔍",
			Description: "Especialista em code review, conformidade estrita de tipagem, linting e legibilidade.",
			Bias:        "Puxa para precisão de diffs, legibilidade de código, convenções idiomáticas e higiene de commits.",
			Permission:  "always-proceed",
		},
		{
			Type:        RoleDoc,
			Name:        "Tech-Writer",
			Profile:     "account7",
			Project:     "swarm-doc",
			Icon:        "📝",
			Description: "Especialista em documentação viva, especificações de arquitetura, READMEs e diagramas Mermaid.",
			Bias:        "Puxa para documentação exaustiva, precisão de contratos, ADRs e sincronização de wikis.",
			Permission:  "always-proceed",
		},
		{
			Type:        RoleBench,
			Name:        "SRE-Performance",
			Profile:     "account8",
			Project:     "swarm-bench",
			Icon:        "⏱️",
			Description: "Especialista em performance, profiling, benchmarks de CPU/memória e escalabilidade.",
			Bias:        "Puxa para complexidade algorítmica ótima, zero alocações desnecessárias e concorrência eficiente.",
			Permission:  "always-proceed",
		},
		{
			Type:        RoleRefactor,
			Name:        "Refactor-Sweeper",
			Profile:     "account9",
			Project:     "swarm-refactor",
			Icon:        "🧹",
			Description: "Especialista em limpeza de código morto, consolidação de duplicatas e higienização de dependências.",
			Bias:        "Puxa para remoção agressiva de débitos técnicos e simplificação de estruturas antigas.",
			Permission:  "always-proceed",
		},
		{
			Type:        RoleAdversary,
			Name:        "Red-Team",
			Profile:     "account10",
			Project:     "swarm-adversary",
			Icon:        "🎯",
			Description: "Especialista em segurança ofensiva, testes adversariais e busca por brechas e edge-cases.",
			Bias:        "Puxa para validação de hipóteses de quebra, exploits, injeções e testes de estresse.",
			Permission:  "always-proceed",
		},
	}
	return append(roles, extended...)
}

// GeneratePersonaRule gera o conteúdo de markdown injetado em .gemini/rules/swarm-persona.md.
func GeneratePersonaRule(role RoleConfig, allRoles []RoleConfig) string {
	var peers []string
	for _, r := range allRoles {
		if r.Type != role.Type {
			peers = append(peers, fmt.Sprintf("- **%s (%s)**: Projeto ai-memory `%s` — %s", r.Name, r.Icon, r.Project, r.Description))
		}
	}

	return fmt.Sprintf(`# ⚡ PROTOCOLO DE ENXAME // PERSONA: %s %s

Você está operando como o agente **%s** dentro de uma sessão do **agy-swarm**.
Seu perfil ativo é: **%s** | Seu projeto no ai-memory é: **%s**.

## 🎯 SEU PAPEL & DIRETRIZ ONTOLÓGICA
- **Descrição:** %s
- **Seu Viés Central:** %s
- **Comportamento em Debate:** Você DEVE defender fortemente o seu viés técnico durante a Mesa Redonda. Não ceda passivamente a soluções que violem seus princípios.

## 📬 PROTOCOLO DE MENSAGERIA INTER-AGENTES (ai-memory)
Você tem acesso ao MCP e CLI 'ai-memory'. Seus pares no enxame são:
%s

### Como se comunicar:
1. **Verificar mensagens pendentes:** Use 'ai-memory message list --project %s' ou tool 'memory_message_list'.
2. **Consumir mensagem:** Use 'ai-memory message pop --project %s' ou tool 'memory_message_pop'.
3. **Enviar réplica/resposta:** Use 'ai-memory message send --project %s --to-project <projeto_alvo> --body "<conteudo>"' ou tool 'memory_message_send'.

## 🛠️ AMBIENTE DE EXECUÇÃO (Git Worktree & Escopo Ortogonal)
Se você for um especialista em fase de implementação pós-debate, você opera isolado em seu Git Worktree:
- Diretório de trabalho: '.worktrees/worker-%s'
- Branch ativo: 'swarm/%s'

### Regras de Ouro para Evitar Conflitos de Merge:
1. **Ortogonalidade de Escopo:**
   - 📐 Arquiteto: interfaces, contratos, assinaturas e abstrações (DIP/ISP).
   - ⚡ Pragmático: lógica de execução, comandos de terminal e núcleo do app.
   - 🛡️ Segurança: sanitização de input, runtime 0600/0700 e permissões.
   - 🧪 QA-Testes: focado estritamente em arquivos '*_test.go' e mocks isolados (evita colisões com código de produção).
2. **Rebase de Relay:** Se o branch base sofrer atualização ou merge prévio, sincronize com 'git rebase main' ou solicite o rebase via Maestro.
3. Comite suas alterações no seu branch local. O Maestro fará o review e integração com Quality Gate.
`, role.Name, role.Icon, role.Name, role.Profile, role.Project, role.Description, role.Bias, strings.Join(peers, "\n"), role.Project, role.Project, role.Project, role.Type, role.Type)
}

// GenerateInitialSpecialistPrompt gera o prompt de boot de um especialista.
func GenerateInitialSpecialistPrompt(role RoleConfig, task string) string {
	return fmt.Sprintf(`⚡ VOCÊ É O ESPECIALISTA %s (%s) NO AGY-SWARM.
Perfil: %s | Projeto ai-memory: %s
Seu foco e viés ontológico obrigatório: %s (%s).

O Maestro lançou a seguinte tarefa para o enxame:
"%s"

SUA MISSÃO IMEDIATA:
1. Resgate a mensagem no seu inbox com a tool 'memory_message_pop' ou executando no terminal:
   ai-memory message pop --project %s
2. Elabore sua análise técnica fundamentada, defendendo fortemente o seu viés (%s). Proponha como a solução deve ser construída respeitando seus princípios.
3. Envie sua réplica técnica diretamente para o Maestro ('swarm-lead') usando a tool 'memory_message_send' (to_workspace="default", to_project="swarm-lead", body="...") ou executando no terminal:
   ai-memory message send --project %s --to-project swarm-lead --body "<sua análise e proposta>"
4. Aguarde no terminal as ordens de implementação do Maestro após a consolidação do debate.
Inicie sua análise agora!`,
		role.Name, role.Icon, role.Profile, role.Project, role.Name, role.Bias,
		task,
		role.Project,
		role.Bias,
		role.Project,
	)
}

// GenerateInitialMaestroPrompt gera o prompt de boot do Maestro.
func GenerateInitialMaestroPrompt(roles []RoleConfig, task string) string {
	var specialistList []string
	for _, r := range roles {
		if r.Type != RoleLead {
			specialistList = append(specialistList, fmt.Sprintf("- %s %s (ai-memory: '%s')", r.Icon, r.Name, r.Project))
		}
	}

	return fmt.Sprintf(`⚡ VOCÊ É O MAESTRO DO ENXAME AGY-SWARM.
Perfil: default | Projeto ai-memory: swarm-lead
Você é o líder da sessão, moderador do debate técnico e o único agente que interage diretamente com o usuário.

TAREFA DO USUÁRIO:
"%s"

ESTADO ATUAL DO ENXAME:
O desafio já foi despachado para as caixas postais dos seus especialistas:
%s
Todos os especialistas já foram acionados e estão elaborando suas propostas técnicas em seus respectivos painéis agora mesmo.

SUA MISSÃO:
1. Aguarde a conclusão da análise inicial dos especialistas. O orquestrador em Go monitora as caixas postais em background a custo zero de tokens e notificará você automaticamente no terminal quando a barreira de sincronização atingir todas as propostas.
2. Assim que receber a notificação de que as propostas estão prontas, resgate todas as mensagens pendentes no seu inbox com 'memory_message_pop' (ou 'ai-memory message pop --project swarm-lead').
3. Sintetize uma visão comparativa das propostas recebidas (Arquitetura, Segurança, Qualidade, Pragmático), aponte os trade-offs e apresente o plano consolidado aqui para o usuário validar.
Aguarde a notificação do orquestrador para iniciar!`,
		task,
		strings.Join(specialistList, "\n"),
	)
}
