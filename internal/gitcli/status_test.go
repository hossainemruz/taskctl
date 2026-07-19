package gitcli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	processutil "github.com/hossainemruz/taskctl/internal/process"
)

type vaultStatusRunner struct {
	results  []processutil.Result
	errors   []error
	commands []processutil.Command
}

func (r *vaultStatusRunner) Run(_ context.Context, command processutil.Command) (processutil.Result, error) {
	r.commands = append(r.commands, command)
	index := len(r.commands) - 1
	return r.results[index], r.errors[index]
}

func (*vaultStatusRunner) Start(processutil.Command) error { return nil }

func TestInspectVaultUsesFreshNoninteractiveGitFacts(t *testing.T) {
	t.Parallel()
	runner := &vaultStatusRunner{
		results: []processutil.Result{
			{Stdout: []byte("true\n")},
			{},
			{Stdout: []byte(" M one.md\n?? two.md\n")},
			{Stdout: []byte("abc123\n")},
			{Stdout: []byte("2\t3\n")},
		},
		errors: make([]error, 5),
	}
	status := NewClient(runner).InspectVault(t.Context(), "/vault")
	if status != (VaultStatus{State: VaultStatusOK, Dirty: 2, Ahead: 2, Behind: 3}) {
		t.Fatalf("InspectVault() = %#v", status)
	}
	wantArgs := [][]string{
		{"rev-parse", "--is-inside-work-tree"},
		{"fetch", "--quiet"},
		{"status", "--porcelain=v1", "--untracked-files=all"},
		{"rev-parse", "--verify", "--quiet", "@{upstream}"},
		{"rev-list", "--left-right", "--count", "HEAD...@{upstream}"},
	}
	for index, command := range runner.commands {
		if command.Name != "git" || command.Dir != "/vault" || !reflect.DeepEqual(command.Args, wantArgs[index]) {
			t.Fatalf("command %d = %#v, want args %#v", index, command, wantArgs[index])
		}
		if index == 1 {
			if !environmentContains(command.Env, "GIT_TERMINAL_PROMPT=0") {
				t.Fatalf("fetch environment does not disable prompts: %#v", command.Env)
			}
		} else if command.Env != nil {
			t.Fatalf("non-fetch command %d has environment overrides: %#v", index, command.Env)
		}
	}
}

func TestInspectVaultClassifiesUnavailableAndNoUpstreamStates(t *testing.T) {
	t.Parallel()
	exitOne := &processutil.CommandError{Name: "git", ExitCode: 1, Cause: errors.New("exit status 1")}
	exit128 := &processutil.CommandError{Name: "git", ExitCode: 128, Cause: errors.New("exit status 128")}
	missing := &processutil.CommandError{Name: "git", ExitCode: -1, Cause: errors.New("missing")}
	tests := []struct {
		name        string
		results     []processutil.Result
		errors      []error
		want        VaultStatus
		wantCommand int
	}{
		{
			name: "not repository", results: []processutil.Result{{}}, errors: []error{exit128},
			want: VaultStatus{State: VaultStatusNotRepository}, wantCommand: 1,
		},
		{
			name: "Git unavailable", results: []processutil.Result{{}}, errors: []error{missing},
			want: VaultStatus{State: VaultStatusUnavailable}, wantCommand: 1,
		},
		{
			name:    "no upstream despite fetch failure",
			results: []processutil.Result{{Stdout: []byte("true\n")}, {}, {Stdout: []byte("?? local.md\n")}, {}},
			errors:  []error{nil, exit128, nil, exitOne},
			want:    VaultStatus{State: VaultStatusNoUpstream, Dirty: 1}, wantCommand: 4,
		},
		{
			name:    "remote unavailable",
			results: []processutil.Result{{Stdout: []byte("true\n")}, {}, {}, {Stdout: []byte("abc\n")}},
			errors:  []error{nil, exit128, nil, nil},
			want:    VaultStatus{State: VaultStatusRemoteUnavailable}, wantCommand: 4,
		},
		{
			name:    "malformed fresh counts",
			results: []processutil.Result{{Stdout: []byte("true\n")}, {}, {}, {Stdout: []byte("abc\n")}, {Stdout: []byte("bad\n")}},
			errors:  []error{nil, nil, nil, nil, nil},
			want:    VaultStatus{State: VaultStatusRemoteUnavailable}, wantCommand: 5,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			runner := &vaultStatusRunner{results: test.results, errors: test.errors}
			got := NewClient(runner).InspectVault(t.Context(), "/vault")
			if got != test.want || len(runner.commands) != test.wantCommand {
				t.Fatalf("InspectVault() = %#v with %d commands, want %#v with %d", got, len(runner.commands), test.want, test.wantCommand)
			}
		})
	}
}

func TestInspectVaultAgainstLocalBareRemote(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	checkout := filepath.Join(root, "vault")
	runGitTestCommand(t, "init", "--bare", remote)
	runGitTestCommand(t, "init", checkout)
	runGitTestCommand(t, "-C", checkout, "branch", "-M", "main")
	writeGitFixture(t, filepath.Join(checkout, "initial.md"), "initial\n")
	gitCommitAll(t, checkout, "initial")
	runGitTestCommand(t, "-C", checkout, "remote", "add", "origin", remote)
	runGitTestCommand(t, "-C", checkout, "push", "-u", "origin", "main")

	client := NewClient(processutil.ExecRunner{})
	if got := client.InspectVault(t.Context(), checkout); got != (VaultStatus{State: VaultStatusOK}) {
		t.Fatalf("clean status = %#v", got)
	}
	writeGitFixture(t, filepath.Join(checkout, "dirty.md"), "dirty\n")
	if got := client.InspectVault(t.Context(), checkout); got.State != VaultStatusOK || got.Dirty != 1 {
		t.Fatalf("dirty status = %#v", got)
	}
	if err := os.Remove(filepath.Join(checkout, "dirty.md")); err != nil {
		t.Fatal(err)
	}

	writeGitFixture(t, filepath.Join(checkout, "ahead.md"), "ahead\n")
	gitCommitAll(t, checkout, "ahead")
	if got := client.InspectVault(t.Context(), checkout); got.Ahead != 1 || got.Behind != 0 {
		t.Fatalf("ahead status = %#v", got)
	}
	runGitTestCommand(t, "-C", checkout, "reset", "--hard", "origin/main")

	peer := filepath.Join(root, "peer")
	runGitTestCommand(t, "clone", remote, peer)
	runGitTestCommand(t, "-C", peer, "checkout", "main")
	writeGitFixture(t, filepath.Join(peer, "behind.md"), "behind\n")
	gitCommitAll(t, peer, "behind")
	runGitTestCommand(t, "-C", peer, "push", "origin", "main")
	if got := client.InspectVault(t.Context(), checkout); got.Ahead != 0 || got.Behind != 1 {
		t.Fatalf("behind status = %#v", got)
	}

	writeGitFixture(t, filepath.Join(checkout, "diverged.md"), "diverged\n")
	gitCommitAll(t, checkout, "diverged")
	if got := client.InspectVault(t.Context(), checkout); got.Ahead != 1 || got.Behind != 1 {
		t.Fatalf("diverged status = %#v", got)
	}

	if err := os.Rename(remote, remote+".unavailable"); err != nil {
		t.Fatal(err)
	}
	if got := client.InspectVault(t.Context(), checkout); got.State != VaultStatusRemoteUnavailable || got.Ahead != 0 || got.Behind != 0 {
		t.Fatalf("unavailable remote status = %#v", got)
	}

	noUpstream := filepath.Join(root, "no-upstream")
	runGitTestCommand(t, "init", noUpstream)
	writeGitFixture(t, filepath.Join(noUpstream, "local.md"), "local\n")
	if got := client.InspectVault(t.Context(), noUpstream); got.State != VaultStatusNoUpstream || got.Dirty != 1 {
		t.Fatalf("no-upstream status = %#v", got)
	}
	plain := filepath.Join(root, "plain-directory")
	if err := os.Mkdir(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := client.InspectVault(t.Context(), plain); got.State != VaultStatusNotRepository {
		t.Fatalf("not-repository status = %#v", got)
	}
}

type blockingVaultRunner struct{}

func (blockingVaultRunner) Run(ctx context.Context, _ processutil.Command) (processutil.Result, error) {
	<-ctx.Done()
	return processutil.Result{}, &processutil.CommandError{Name: "git", ExitCode: -1, Cause: ctx.Err()}
}

func (blockingVaultRunner) Start(processutil.Command) error { return nil }

func TestInspectVaultHonorsContextTimeout(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	status := NewClient(blockingVaultRunner{}).InspectVault(ctx, "/vault")
	if status.State != VaultStatusUnavailable {
		t.Fatalf("timed-out status = %#v", status)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("InspectVault() ignored context timeout; elapsed %s", elapsed)
	}
}

type fetchTimeoutRunner struct {
	commands int
}

func (r *fetchTimeoutRunner) Run(ctx context.Context, command processutil.Command) (processutil.Result, error) {
	r.commands++
	switch command.Args[0] {
	case "rev-parse":
		if len(command.Args) > 1 && command.Args[1] == "--is-inside-work-tree" {
			return processutil.Result{Stdout: []byte("true\n")}, nil
		}
		return processutil.Result{Stdout: []byte("abc123\n")}, nil
	case "fetch":
		<-ctx.Done()
		return processutil.Result{}, &processutil.CommandError{Name: "git", ExitCode: -1, Cause: ctx.Err()}
	case "status":
		return processutil.Result{Stdout: []byte("?? local.md\n")}, nil
	default:
		return processutil.Result{}, errors.New("unexpected Git command")
	}
}

func (*fetchTimeoutRunner) Start(processutil.Command) error { return nil }

func TestInspectVaultFetchTimeoutRetainsLocalStatus(t *testing.T) {
	t.Parallel()
	runner := &fetchTimeoutRunner{}
	client := NewClient(runner)
	client.fetchTimeout = 20 * time.Millisecond
	started := time.Now()
	status := client.InspectVault(t.Context(), "/vault")
	if status != (VaultStatus{State: VaultStatusRemoteUnavailable, Dirty: 1}) {
		t.Fatalf("fetch-timeout status = %#v", status)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("fetch timeout took %s", elapsed)
	}
	if runner.commands != 4 {
		t.Fatalf("commands = %d, want repository, fetch, status, and upstream", runner.commands)
	}
}

func environmentContains(environment []string, wanted string) bool {
	for _, entry := range environment {
		if entry == wanted {
			return true
		}
	}
	return false
}

func writeGitFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitCommitAll(t *testing.T, directory, message string) {
	t.Helper()
	runGitTestCommand(t, "-C", directory, "add", ".")
	runGitTestCommand(t, "-C", directory, "-c", "user.name=Taskctl Test", "-c", "user.email=taskctl@example.invalid", "commit", "-m", message)
}
