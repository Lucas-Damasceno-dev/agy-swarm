package pipeline

import (
	"time"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
)

// StageType identifica cada um dos 4 estágios do pipeline determinístico.
type StageType string

const (
	StageContract    StageType = "1_CONTRACT"    // Arch: define interfaces, tipos e contratos
	StageRedTeam     StageType = "2_RED_TEAM"    // QA + Sec: escreve testes falhando (*_test.go)
	StageSpeculative StageType = "3_SPECULATIVE" // Workers competem no código até test-cmd passar
	StageAuditMerge  StageType = "4_AUDIT_MERGE" // Maestro: filtra sobreviventes e integra no HEAD
)

// CandidateResult resume a execução e os resultados obtidos por um worker no estágio de competição.
type CandidateResult struct {
	Role         config.RoleConfig
	AccountID    string
	WorktreeDir  string
	Branch       string
	ExitCode     int
	TestOutput   string
	TestPassed   bool
	Diff         string
	DiffStat     string
	LinesAdded   int
	LinesDeleted int
	Error        error
	Duration     time.Duration
}

// StageResult armazena o status e os artefatos de um estágio concluído.
type StageResult struct {
	Stage       StageType
	Success     bool
	Candidates  []CandidateResult
	Winner      *CandidateResult
	Error       error
	Duration    time.Duration
	ArtifactMsg string
}
