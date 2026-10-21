// Package runner executes one command as a local process.
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
)

// Action describes a command to run. Argv[0] is the executable; the remaining
// values are passed to it as arguments. Commands are not interpreted by a shell.
type Action struct {
	Argv []string
	Dir  string
}

// Result contains the process output and exit code. An exit code other than zero
// is a command outcome, not an error returned by Run. ExitCode is -1 if the
// process could not be started or did not produce an exit status.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Run starts an action and waits for it to finish. It returns an error if the
// process could not be started, the context was cancelled, or the process could
// not be waited on. A non-zero process exit code is recorded in Result.
func Run(ctx context.Context, action Action) (Result, error) {
	result := Result{ExitCode: -1}
	if len(action.Argv) == 0 || action.Argv[0] == "" {
		return result, errors.New("action must include an executable in Argv[0]")
	}

	cmd := exec.CommandContext(ctx, action.Argv[0], action.Argv[1:]...)
	cmd.Dir = action.Dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result.Stdout = stdout.Bytes()
	result.Stderr = stderr.Bytes()
	if err == nil {
		result.ExitCode = 0
		return result, nil
	}
	if ctx.Err() != nil {
		return result, fmt.Errorf("command interrupted: %w", ctx.Err())
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	return result, fmt.Errorf("start command: %w", err)
}
