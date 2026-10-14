package notify

import (
	"fmt"
	"os"
	"os/exec"
)

// SendDesktop envia uma notificação desktop via notify-send e emite um bell no terminal.
func SendDesktop(title, message string) {
	// 1. Toca o bell do terminal
	fmt.Fprint(os.Stderr, "\a")

	// 2. Executa notify-send se disponível
	if _, err := exec.LookPath("notify-send"); err == nil {
		_ = exec.Command("notify-send", "-a", "agy-swarm", "-i", "terminal", title, message).Run()
	}
}
