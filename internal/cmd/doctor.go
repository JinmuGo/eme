package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/jinmu/eme/internal/errors"
	"github.com/jinmu/eme/internal/git"
	"github.com/jinmu/eme/internal/runner"
	"github.com/jinmu/eme/internal/tmux"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Verify eme environment",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDoctor()
	},
}

func runDoctor() error {
	checks := []struct {
		name string
		fn   func() (bool, string)
	}{
		{"tmux installed", checkTmuxInstalled},
		{"tmux server reachable", checkTmuxServer},
		{"tmux popup support", checkTmuxPopup},
		{"git installed", checkGitInstalled},
		{"agent on PATH", checkAgent},
		{"state dir writable", checkStateDir},
	}

	allOK := true
	for _, c := range checks {
		ok, msg := c.fn()
		status := "ok"
		if !ok {
			status = "fail"
			allOK = false
		}
		fmt.Printf("[%s] %s: %s\n", status, c.name, msg)
	}

	if !allOK {
		return errors.New(errors.CodeCommandFailed,
			"eme doctor found problems.",
			"One or more environment checks failed.",
			"Fix the failed checks above and run `eme doctor` again.")
	}
	return nil
}

func checkTmuxInstalled() (bool, string) {
	v, err := tmux.Version()
	if err != nil {
		return false, err.Error()
	}
	return true, v
}

func checkTmuxServer() (bool, string) {
	if tmux.ServerReachable() {
		return true, "server reachable"
	}
	env := tmux.DetectEnv()
	if env.SocketPath != "" {
		return false, fmt.Sprintf("server not reachable on %s", env.SocketPath)
	}
	return false, "server not running"
}

func checkTmuxPopup() (bool, string) {
	v, err := tmux.Version()
	if err != nil {
		return false, err.Error()
	}
	major, minor, ok := parseTmuxVersion(v)
	if !ok {
		return false, fmt.Sprintf("could not parse tmux version: %s", v)
	}
	if major > 3 || (major == 3 && minor >= 2) {
		return true, fmt.Sprintf("%s (popup support available)", v)
	}
	return false, fmt.Sprintf("%s (popups require tmux 3.2+)", v)
}

func parseTmuxVersion(v string) (major, minor int, ok bool) {
	var prefix string
	_, err := fmt.Sscanf(v, "%s %d.%d", &prefix, &major, &minor)
	if err != nil {
		return 0, 0, false
	}
	return major, minor, true
}

func checkGitInstalled() (bool, string) {
	v, err := git.Version()
	if err != nil {
		return false, err.Error()
	}
	return true, v
}

func checkAgent() (bool, string) {
	agent := cfg.Agent.Command
	if agent == "" {
		agent = "opencode"
	}
	_, _, err := runner.Default.Run(context.Background(), "which", agent)
	if err != nil {
		return false, fmt.Sprintf("%s not found on PATH", agent)
	}
	return true, fmt.Sprintf("%s found", agent)
}

func checkStateDir() (bool, string) {
	dir := filepath.Dir(statePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err.Error()
	}
	return true, dir
}
