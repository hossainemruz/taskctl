package process

import (
	"errors"
	"strings"
	"testing"
)

func TestExecRunnerCapturesOutputAndTypedFailure(t *testing.T) {
	t.Parallel()
	runner := ExecRunner{}
	result, err := runner.Run(t.Context(), Command{
		Name: "sh",
		Args: []string{"-c", "printf stdout; printf stderr >&2; exit 7", "credential-argument"},
	})
	if !errors.Is(err, ErrCommandFailed) {
		t.Fatalf("Run() error = %v, want ErrCommandFailed", err)
	}
	var commandError *CommandError
	if !errors.As(err, &commandError) || commandError.ExitCode != 7 {
		t.Fatalf("Run() error = %#v, want exit code 7", err)
	}
	if string(result.Stdout) != "stdout" || string(result.Stderr) != "stderr" {
		t.Fatalf("Run() result = %#v", result)
	}
	if strings.Contains(err.Error(), "credential-argument") || strings.Contains(err.Error(), "stderr") {
		t.Fatalf("Run() error exposes arguments or stderr: %v", err)
	}
}

func TestExecRunnerReportsMissingExecutable(t *testing.T) {
	t.Parallel()
	runner := ExecRunner{}
	_, err := runner.Run(t.Context(), Command{Name: "taskctl-executable-that-does-not-exist"})
	if !errors.Is(err, ErrCommandFailed) {
		t.Fatalf("Run() error = %v, want ErrCommandFailed", err)
	}
	var commandError *CommandError
	if !errors.As(err, &commandError) || commandError.ExitCode != -1 {
		t.Fatalf("Run() error = %#v, want unavailable command", err)
	}
	if err := runner.Start(Command{Name: "taskctl-executable-that-does-not-exist"}); !errors.Is(err, ErrCommandFailed) {
		t.Fatalf("Start() error = %v, want ErrCommandFailed", err)
	}
}
