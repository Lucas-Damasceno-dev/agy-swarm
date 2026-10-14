package tmux_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/config"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/tmux"
)

func TestMockMultiplexer_Lifecycle(t *testing.T) {
	mock := tmux.NewMockMultiplexer("test-session")

	if mock.SessionExists() {
		t.Fatalf("esperava sessão inexistente inicialmente")
	}

	roles := []config.RoleConfig{
		{Type: config.RoleLead, Name: "Maestro"},
		{Type: config.RoleQA, Name: "QA-Testes"},
	}

	if err := mock.BuildLayout(roles); err != nil {
		t.Fatalf("erro inesperado em BuildLayout: %v", err)
	}

	if !mock.SessionExists() {
		t.Fatalf("esperava sessão existente após BuildLayout")
	}

	mock.SetupHUD()
	mock.UpdatePaneStatus(config.RoleQA, "Executando", "green")

	if state := mock.GetPaneState(config.RoleQA); state != "Executando" {
		t.Errorf("esperava estado 'Executando', obteve %q", state)
	}

	cmd := "go test ./..."
	if err := mock.SendKeys(config.RoleQA, cmd); err != nil {
		t.Fatalf("erro inesperado em SendKeys: %v", err)
	}

	buf := mock.GetBuffer(config.RoleQA)
	if !strings.Contains(buf, cmd) {
		t.Errorf("esperava buffer conter %q, obteve %q", cmd, buf)
	}

	calls := mock.GetRecordedCalls()
	if len(calls) < 4 {
		t.Errorf("esperava pelo menos 4 chamadas registradas, obteve %d", len(calls))
	}

	if err := mock.KillSession(); err != nil {
		t.Fatalf("erro ao matar sessão: %v", err)
	}

	if mock.SessionExists() {
		t.Errorf("sessão não deveria existir após KillSession")
	}
}

func TestMockMultiplexer_ErrorInjection(t *testing.T) {
	mock := tmux.NewMockMultiplexer("err-session")

	expectedErr := errors.New("falha injetada de pty")
	mock.SetSendError(expectedErr)

	err := mock.SendKeys(config.RoleLead, "echo teste")
	if !errors.Is(err, expectedErr) {
		t.Errorf("esperava erro %v, obteve %v", expectedErr, err)
	}

	buildErr := errors.New("falha ao alocar layout")
	mock.SetBuildError(buildErr)
	if err := mock.BuildLayout([]config.RoleConfig{{Type: config.RoleLead}}); !errors.Is(err, buildErr) {
		t.Errorf("esperava erro %v, obteve %v", buildErr, err)
	}
}
