package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
)

var ErrCommandFailed = errors.New("external command failed")

// Command describes a direct executable invocation without shell parsing.
type Command struct {
	Name string
	Args []string
	Dir  string
	Env  []string
}

type Result struct {
	Stdout []byte
	Stderr []byte
}

// CommandError retains the underlying process failure without including the
// argument vector or captured output in its message. Callers can inspect the
// returned Result when command-specific handling is appropriate without
// accidentally exposing credentials from arguments or stderr.
type CommandError struct {
	Name     string
	ExitCode int
	Cause    error
}

func (e *CommandError) Error() string {
	if e.ExitCode >= 0 {
		return fmt.Sprintf("run %q: command exited with status %d", e.Name, e.ExitCode)
	}
	return fmt.Sprintf("run %q: %v", e.Name, e.Cause)
}

func (e *CommandError) Unwrap() error { return e.Cause }

func (e *CommandError) Is(target error) bool {
	return target == ErrCommandFailed || errors.Is(e.Cause, target)
}

// Runner is the external-process seam used by Git and viewer workflows.
type Runner interface {
	Run(context.Context, Command) (Result, error)
	Start(Command) error
}

// ExecRunner invokes processes through os/exec.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, command Command) (Result, error) {
	process := exec.CommandContext(ctx, command.Name, command.Args...)
	process.Dir = command.Dir
	if command.Env != nil {
		process.Env = append([]string(nil), command.Env...)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	process.Stdout = &stdout
	process.Stderr = &stderr
	err := process.Run()
	result := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if err != nil {
		exitCode := -1
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			exitCode = exitError.ExitCode()
		}
		return result, &CommandError{Name: command.Name, ExitCode: exitCode, Cause: err}
	}
	return result, nil
}

func (ExecRunner) Start(command Command) error {
	process := exec.Command(command.Name, command.Args...)
	process.Dir = command.Dir
	if command.Env != nil {
		process.Env = append([]string(nil), command.Env...)
	}
	if err := process.Start(); err != nil {
		return &CommandError{Name: command.Name, ExitCode: -1, Cause: err}
	}
	if err := process.Process.Release(); err != nil {
		return &CommandError{Name: command.Name, ExitCode: -1, Cause: err}
	}
	return nil
}
