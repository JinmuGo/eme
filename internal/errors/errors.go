// Package errors defines structured, conversational error values for eme.
package errors

import (
	"fmt"
	"strings"
)

// Common error codes.
const (
	CodeTmuxNotFound      = "tmux_not_found"
	CodeGitNotFound       = "git_not_found"
	CodeAgentNotFound     = "agent_not_found"
	CodeTmuxServerMissing = "tmux_server_missing"
	CodeInvalidFolder     = "invalid_folder"
	CodeExistingGitRepo   = "existing_git_repo"
	CodeWorktreeExists    = "worktree_exists"
	CodeSessionExists     = "session_exists"
	CodeSessionNotFound   = "session_not_found"
	CodeBranchExists      = "branch_exists"
	CodeCommandFailed     = "command_failed"
	CodeStateCorrupted    = "state_corrupted"
	CodeConfigInvalid     = "config_invalid"
)

// EmeError is a structured error with a problem, cause, and suggested fix.
type EmeError struct {
	Code    string // machine-readable code
	Message string // what happened
	Cause   string // why it happened
	Fix     string // what the user should do next
	Err     error  // underlying error, optional
}

// Error implements the error interface.
func (e *EmeError) Error() string {
	var b strings.Builder
	b.WriteString(e.Message)
	if e.Cause != "" {
		b.WriteString("\n  Cause: ")
		b.WriteString(e.Cause)
	}
	if e.Fix != "" {
		b.WriteString("\n  Fix: ")
		b.WriteString(e.Fix)
	}
	if e.Err != nil {
		b.WriteString("\n  Details: ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

// Unwrap returns the underlying error.
func (e *EmeError) Unwrap() error {
	return e.Err
}

// New creates a new EmeError.
func New(code, message, cause, fix string) *EmeError {
	return &EmeError{Code: code, Message: message, Cause: cause, Fix: fix}
}

// Wrap wraps an underlying error with a structured eme error.
func Wrap(code, message, cause, fix string, err error) *EmeError {
	return &EmeError{Code: code, Message: message, Cause: cause, Fix: fix, Err: err}
}

// Is reports whether err is an EmeError.
func Is(err error) bool {
	_, ok := err.(*EmeError)
	return ok
}

// As returns the EmeError inside err, if any.
func As(err error) *EmeError {
	if e, ok := err.(*EmeError); ok {
		return e
	}
	return nil
}

// FromCommand wraps an exec error with a code and human-readable prefix.
func FromCommand(code string, cmd string, err error) *EmeError {
	return Wrap(code,
		fmt.Sprintf("command failed: %s", cmd),
		"the external command returned a non-zero exit code",
		"run with --verbose to see full command output",
		err,
	)
}
