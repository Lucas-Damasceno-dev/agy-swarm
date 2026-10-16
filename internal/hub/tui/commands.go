package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/discovery"
	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/hub/model"
)

func createAgyCommand(p *model.Profile) *exec.Cmd {
	realHome := discovery.GetRealHome()
	agyProfileBin := filepath.Join(realHome, ".local/bin/agy-profile")

	var cmd *exec.Cmd
	if _, err := os.Stat(agyProfileBin); err == nil {
		cmd = exec.Command(agyProfileBin, p.ID)
	} else {
		agyBin := filepath.Join(realHome, ".local/bin/agy")
		cmd = exec.Command(agyBin)
		setupEnvironment(cmd, p, realHome)
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

func createAgyPromptCommand(p *model.Profile, prompt string) *exec.Cmd {
	realHome := discovery.GetRealHome()
	agyProfileBin := filepath.Join(realHome, ".local/bin/agy-profile")

	var cmd *exec.Cmd
	if _, err := os.Stat(agyProfileBin); err == nil {
		cmd = exec.Command(agyProfileBin, p.ID, "-p", prompt)
	} else {
		agyBin := filepath.Join(realHome, ".local/bin/agy")
		cmd = exec.Command(agyBin, "-p", prompt)
		setupEnvironment(cmd, p, realHome)
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

func createAgyLoginCommand(p *model.Profile) *exec.Cmd {
	realHome := discovery.GetRealHome()
	agyProfileBin := filepath.Join(realHome, ".local/bin/agy-profile")

	var cmd *exec.Cmd
	if _, err := os.Stat(agyProfileBin); err == nil {
		cmd = exec.Command(agyProfileBin, p.ID, "login")
	} else {
		agyBin := filepath.Join(realHome, ".local/bin/agy")
		cmd = exec.Command(agyBin, "login")
		setupEnvironment(cmd, p, realHome)
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

func setupEnvironment(cmd *exec.Cmd, p *model.Profile, realHome string) {
	customEnv := map[string]string{
		"REAL_HOME":                realHome,
		"HOME":                     p.HomeDir,
		"XDG_CONFIG_HOME":          filepath.Join(p.HomeDir, ".config"),
		"XDG_DATA_HOME":            filepath.Join(p.HomeDir, ".local/share"),
		"XDG_CACHE_HOME":           filepath.Join(p.HomeDir, ".cache"),
		"XDG_STATE_HOME":           filepath.Join(p.HomeDir, ".local/state"),
		"DBUS_SESSION_BUS_ADDRESS": "disabled",
		"GIT_CONFIG_GLOBAL":        filepath.Join(realHome, ".gitconfig"),
	}

	env := os.Environ()
	filtered := []string{}
	for _, e := range env {
		skip := false
		for k := range customEnv {
			if len(e) > len(k) && e[:len(k)+1] == k+"=" {
				skip = true
				break
			}
		}
		if !skip {
			filtered = append(filtered, e)
		}
	}

	for k, v := range customEnv {
		filtered = append(filtered, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = filtered
}
