package gitcli

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	processutil "github.com/hossainemruz/taskctl/internal/process"
)

type clientRunner struct {
	results  []processutil.Result
	errors   []error
	commands []processutil.Command
}

func (r *clientRunner) Run(_ context.Context, command processutil.Command) (processutil.Result, error) {
	r.commands = append(r.commands, command)
	index := len(r.commands) - 1
	return r.results[index], r.errors[index]
}

func (*clientRunner) Start(processutil.Command) error { return nil }

func TestClientUsesExactGitCommands(t *testing.T) {
	t.Parallel()
	runner := &clientRunner{
		results: []processutil.Result{{Stdout: []byte("git@github.com:org/repo.git\n")}, {Stdout: []byte("feature/work\n")}},
		errors:  []error{nil, nil},
	}
	client := NewClient(runner)
	repository, err := client.Repository(t.Context(), "/work/repo")
	if err != nil {
		t.Fatal(err)
	}
	branch, err := client.CurrentBranch(t.Context(), "/work/repo")
	if err != nil {
		t.Fatal(err)
	}
	if repository.Normalized != "github.com/org/repo" || branch != "feature/work" {
		t.Fatalf("repository = %#v, branch = %q", repository, branch)
	}
	want := []processutil.Command{
		{Name: "git", Args: []string{"config", "--get", "remote.origin.url"}, Dir: "/work/repo"},
		{Name: "git", Args: []string{"symbolic-ref", "--quiet", "--short", "HEAD"}, Dir: "/work/repo"},
	}
	if !reflect.DeepEqual(runner.commands, want) {
		t.Fatalf("commands = %#v, want %#v", runner.commands, want)
	}
}

func TestClientClassifiesMissingOriginAndDetachedHEAD(t *testing.T) {
	t.Parallel()
	exitOne := &processutil.CommandError{Name: "git", ExitCode: 1, Cause: errors.New("exit status 1")}
	runner := &clientRunner{
		results: []processutil.Result{{}, {}},
		errors:  []error{exitOne, exitOne},
	}
	client := NewClient(runner)
	if _, err := client.Origin(t.Context(), "/repo"); !errors.Is(err, ErrNoOrigin) {
		t.Fatalf("Origin() error = %v, want ErrNoOrigin", err)
	}
	if _, err := client.CurrentBranch(t.Context(), "/repo"); !errors.Is(err, ErrDetachedHEAD) {
		t.Fatalf("CurrentBranch() error = %v, want ErrDetachedHEAD", err)
	}
}

func TestClientAgainstTemporaryGitRepository(t *testing.T) {
	t.Parallel()
	repositoryDirectory := filepath.Join(t.TempDir(), "checkout")
	runGitTestCommand(t, "init", repositoryDirectory)
	runGitTestCommand(t, "-C", repositoryDirectory, "remote", "add", "origin", "https://github.com/org/repo.git")
	runGitTestCommand(t, "-C", repositoryDirectory, "symbolic-ref", "HEAD", "refs/heads/feature/test")

	client := NewClient(processutil.ExecRunner{})
	repository, err := client.Repository(t.Context(), repositoryDirectory)
	if err != nil {
		t.Fatal(err)
	}
	branch, err := client.CurrentBranch(t.Context(), repositoryDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if repository.Normalized != "github.com/org/repo" || branch != "feature/test" {
		t.Fatalf("repository = %#v, branch = %q", repository, branch)
	}
}

func runGitTestCommand(t *testing.T, arguments ...string) {
	t.Helper()
	command := exec.Command("git", arguments...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
}
