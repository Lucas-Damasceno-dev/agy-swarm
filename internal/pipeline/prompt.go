package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/security"
)

// PromptBuilder constrói prompts otimizados sem ruído de mensageria para os processos efêmeros.
type PromptBuilder struct {
	Task string
}

// NewPromptBuilder cria um novo construtor de prompts injetados.
func NewPromptBuilder(task string) *PromptBuilder {
	return &PromptBuilder{Task: task}
}

// BuildContractPrompt gera a missão do Arquiteto (Stage 1).
func (pb *PromptBuilder) BuildContractPrompt(role config.RoleConfig, projectASTSummary string) string {
	var astBlock string
	if projectASTSummary != "" {
		astBlock = fmt.Sprintf("\n## 🗺️ ESTRUTURA E CONTRATOS DO PROJETO (AST):\n%s\n", projectASTSummary)
	}

	return fmt.Sprintf(`⚡ AGY-SWARM // ESTÁGIO 1: ESPECIFICAÇÃO & CONTRATOS
Papel: %s (%s) | Viés: %s

## 🎯 TAREFA PRINCIPAL:
"%s"
%s
## 📋 SUA MISSÃO CIRÚRGICA:
1. Declare e especifique as INTERFACES, TIPOS, STRUCTS e ASSINATURAS necessárias para suportar a tarefa.
2. NÃO implemente a lógica de negócio final ou algoritmos complexos neste estágio; forneça stubs/esqueletos para compilação limpa.
3. Garanta que o projeto continue compilando sem erros de sintaxe ou imports quebrados.
4. Ao concluir, faça 'git add' e 'git commit' com a mensagem "feat(spec): definir contratos e interfaces".

Atenção: Você é um executor efêmero. Não gaste turnos com mensagens ou bate-papo. Execute cirurgicamente no código e encerre!`,
		role.Name, role.Icon, role.Bias, pb.Task, astBlock)
}

// BuildRedTeamPrompt gera a missão do QA/Sec (Stage 2).
func (pb *PromptBuilder) BuildRedTeamPrompt(role config.RoleConfig, contractDiff string) string {
	return fmt.Sprintf(`⚡ AGY-SWARM // ESTÁGIO 2: RED-TEAM TDD (TESTES ADVERSARIAIS)
Papel: %s (%s) | Viés: %s

## 🎯 TAREFA PRINCIPAL:
"%s"

## 📐 CONTRATOS E INTERFACES DECLARADOS NO ESTÁGIO 1:
`+"```diff"+`
%s
`+"```"+`

## 📋 SUA MISSÃO CIRÚRGICA:
1. Escreva a suíte de testes unitários e de integração (*_test.go em Go, ou em arquivos dedicados tests/*.rs / src/*_test.rs em Rust) que cubra completamente o comportamento esperado.
2. Em projetos Rust: NUNCA declare testes inline com #[cfg(test)] dentro de arquivos de produção (src/*.rs). Use SEMPRE arquivos dedicados (tests/<nome>.rs ou src/<modulo>_test.rs) para garantir a imutabilidade do suite e evitar conflitos com workers.
3. Seus testes DEVEM ser rigorosos: cubra casos de sucesso, edge-cases, limites numéricos, sanitização e segurança.
4. IMPORTANTE: Seus testes devem ser fiéis aos contratos acima e DEVEM falhar agora contra os stubs vazios (validando que o teste realmente testa algo novo).
5. Ao concluir, faça 'git add' e 'git commit' com a mensagem "test(red-team): suite de testes adversariais para validacao".

Atenção: Foco estrito em arquivos de teste. Execute cirurgicamente e encerre!`,
		role.Name, role.Icon, role.Bias, pb.Task, contractDiff)
}

// BuildWorkerPrompt gera a missão de implementação para os workers concorrentes (Stage 3).
func (pb *PromptBuilder) BuildWorkerPrompt(role config.RoleConfig, contractDiff string, testDiff string, failingTestOutput string) string {
	return fmt.Sprintf(`⚡ AGY-SWARM // ESTÁGIO 3: IMPLEMENTAÇÃO COMPETITIVA (BEST-OF-N)
Papel: %s (%s) | Viés: %s

## 🎯 TAREFA PRINCIPAL:
"%s"

## 📐 CONTRATOS (ESTÁGIO 1):
`+"```diff"+`
%s
`+"```"+`

## 🧪 TESTES DE VERIFICAÇÃO ADVERSARIAL (ESTÁGIO 2):
`+"```diff"+`
%s
`+"```"+`

## ❌ FALHA ATUAL DOS TESTES:
`+"```"+`
%s
`+"```"+`

## 📋 SUA MISSÃO CIRÚRGICA:
1. Implemente a lógica de produção necessária na sua worktree para fazer os testes do Estágio 2 passarem 100%%.
2. Seu viés técnico é: %s. Aplique-o na sua solução.
3. NÃO altere os arquivos de teste do Estágio 2 para fazê-los passar artificialmente; a implementação é que deve satisfazer o teste.
4. Garanta que a suíte completa de testes passe antes de encerrar.
5. Ao concluir com testes verdes, faça 'git commit' no seu branch.

Atenção: Você está competindo contra outras soluções. A mais limpa, correta e performática será integrada. Execute e encerre!`,
		role.Name, role.Icon, role.Bias, pb.Task, contractDiff, testDiff, failingTestOutput, role.Bias)
}

// BuildSelfHealingPrompt gera a missão de auto-cura pós-falha no Filtro Frio.
func (pb *PromptBuilder) BuildSelfHealingPrompt(role config.RoleConfig, failingOutput string, attempt int) string {
	return fmt.Sprintf(`⚡ AGY-SWARM // ESTÁGIO 3: AUTO-CURA (TENTATIVA %d/2)
Papel: %s (%s)

## ❌ O FILTRO FRIO DE TESTES REPROVOU SUA IMPLEMENTAÇÃO COM O SEGUINTE ERRO:
`+"```"+`
%s
`+"```"+`

## 📋 SUA MISSÃO CIRÚRGICA DE REPARO:
1. Analise a falha exata de compilação ou asserção acima.
2. Corrija o código na sua worktree para fazer os testes passarem sem quebrar contratos.
3. NÃO altere ou apague os arquivos de teste; o Go restaura a integridade dos testes antes de reavaliar.
4. Ao concluir, faça 'git commit' com a correção. Execute cirurgicamente e encerre!`,
		attempt, role.Name, role.Icon, failingOutput)
}

// WritePromptFile grava o prompt com permissão estrita 0600 em pasta temporária protegida.
func WritePromptFile(workdir, prefix string, content string) (string, error) {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	var baseDir string
	if runtimeDir != "" {
		baseDir = filepath.Join(runtimeDir, "agy-swarm", "prompts")
	} else {
		baseDir = filepath.Join(workdir, ".worktrees", "prompts")
	}

	if err := os.MkdirAll(baseDir, security.RestrictedDirMode); err != nil {
		return "", fmt.Errorf("falha ao criar pasta de prompts: %w", err)
	}

	filePath := filepath.Join(baseDir, fmt.Sprintf("%s-%d-%d.txt", prefix, os.Getpid(), time.Now().UnixNano()))
	if err := security.WriteRestrictedFile(filePath, []byte(strings.TrimSpace(content)+"\n")); err != nil {
		return "", fmt.Errorf("falha ao gravar arquivo de prompt restrito: %w", err)
	}

	return filePath, nil
}
