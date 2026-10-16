package notify

import (
	"testing"
)

func TestSendDesktop_NoPanic(t *testing.T) {
	// Garante que a chamada não entra em panic mesmo sem display ou notify-send
	SendDesktop("Test Suite", "Teste de notificação unitária")
}
