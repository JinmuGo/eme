// Package runner abstracts external command execution so eme can be tested
// without real tmux or git installations.
package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// Runner runs an external command and returns its output.
type Runner interface {
	// Run executes name with the given arguments and returns stdout, stderr, and an error.
	Run(ctx context.Context, name string, args ...string) (string, string, error)
}

// Default is the production runner.
var Default Runner = &defaultRunner{}

// Verbose enables printing of every command to stderr before execution.
var Verbose bool

type defaultRunner struct{}

func (r *defaultRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	if Verbose {
		fmt.Fprintf(os.Stderr, "+ %s %v\n", name, args)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	stdout := string(out)
	stderr := stdout // combined
	if err != nil {
		return stdout, stderr, fmt.Errorf("%s %v: %w", name, args, err)
	}
	return stdout, stderr, nil
}

// Mock records invocations and returns canned responses.
type Mock struct {
	Calls   []MockCall
	Outputs map[string]MockResponse
}

// MockCall records one invocation.
type MockCall struct {
	Name string
	Args []string
}

// MockResponse is the canned response for a command key.
type MockResponse struct {
	Stdout string
	Stderr string
	Err    error
}

// NewMock creates a mock runner.
func NewMock() *Mock {
	return &Mock{Outputs: make(map[string]MockResponse)}
}

// Key builds a lookup key from a command and arguments.
func Key(name string, args ...string) string {
	s := name
	for _, a := range args {
		s += " " + a
	}
	return s
}

func (m *Mock) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	m.Calls = append(m.Calls, MockCall{Name: name, Args: args})
	resp, ok := m.Outputs[Key(name, args...)]
	if !ok {
		return "", "", fmt.Errorf("mock runner: unexpected command %s %v", name, args)
	}
	return resp.Stdout, resp.Stderr, resp.Err
}

// Set configures a canned response for a command.
func (m *Mock) Set(name string, args []string, stdout, stderr string, err error) {
	m.Outputs[Key(name, args...)] = MockResponse{Stdout: stdout, Stderr: stderr, Err: err}
}

// EnvOrDefault returns the value of envVar if set, otherwise defaultValue.
func EnvOrDefault(envVar, defaultValue string) string {
	if v := os.Getenv(envVar); v != "" {
		return v
	}
	return defaultValue
}
