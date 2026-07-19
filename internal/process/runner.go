package process

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

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
		return result, fmt.Errorf("run %q: %w", command.Name, err)
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
		return fmt.Errorf("start %q: %w", command.Name, err)
	}
	if err := process.Process.Release(); err != nil {
		return fmt.Errorf("release %q: %w", command.Name, err)
	}
	return nil
}
