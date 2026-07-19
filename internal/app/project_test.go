package app

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/hossainemruz/taskctl/internal/domain"
	"github.com/hossainemruz/taskctl/internal/gitcli"
	processutil "github.com/hossainemruz/taskctl/internal/process"
	"github.com/hossainemruz/taskctl/internal/vault"
)

type projectRunner struct {
	origin    string
	originErr error
	branch    string
	branchErr error
	commands  []processutil.Command
	startErr  error
}

func (r *projectRunner) Run(_ context.Context, command processutil.Command) (processutil.Result, error) {
	r.commands = append(r.commands, command)
	if len(command.Args) > 0 && command.Args[0] == "config" {
		return processutil.Result{Stdout: []byte(r.origin)}, r.originErr
	}
	return processutil.Result{Stdout: []byte(r.branch)}, r.branchErr
}

func (r *projectRunner) Start(command processutil.Command) error {
	r.commands = append(r.commands, command)
	return r.startErr
}

func newProjectServiceTest(t *testing.T, runner *projectRunner) (*ProjectService, *vault.Store) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "vault")
	if _, err := vault.Initialize(root); err != nil {
		t.Fatal(err)
	}
	store, err := vault.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	return NewProjectService(store, gitcli.NewClient(runner)), store
}

func TestRegisterProjectDerivesPortableIdentityAndPrefix(t *testing.T) {
	t.Parallel()
	runner := &projectRunner{origin: "git@GitHub.com:org/team/taskctl.git\n"}
	service, store := newProjectServiceTest(t, runner)
	input := RegisterProjectInput{Directory: "/work/repo"}
	result, err := service.RegisterProject(t.Context(), input)
	if err != nil {
		t.Fatalf("RegisterProject() error = %v", err)
	}
	if !result.Created || result.Project.ID != "org_team_taskctl" ||
		result.Project.Repository != "github.com/org/team/taskctl" || result.Project.TaskPrefix != "TASKCTL" {
		t.Fatalf("RegisterProject() = %#v", result)
	}
	loaded, err := store.LoadProject(result.Project.ID)
	if err != nil || !reflect.DeepEqual(loaded, result.Project) {
		t.Fatalf("stored project = %#v, error = %v", loaded, err)
	}

	again, err := service.RegisterProject(t.Context(), input)
	if err != nil || again.Created || !reflect.DeepEqual(again.Project, result.Project) {
		t.Fatalf("second RegisterProject() = %#v, error = %v", again, err)
	}
}

func TestRegisterProjectSupportsExplicitIdentityForUnusableOrigin(t *testing.T) {
	t.Parallel()
	runner := &projectRunner{origin: "/tmp/local-repository\n"}
	service, _ := newProjectServiceTest(t, runner)
	result, err := service.RegisterProject(t.Context(), RegisterProjectInput{
		Directory:  "/work/repo",
		ProjectID:  "local_project",
		Repository: "example.internal/team/project",
		TaskPrefix: "LOCAL",
	})
	if err != nil {
		t.Fatalf("RegisterProject() error = %v", err)
	}
	if !result.Created || result.Project.ID != "local_project" || result.Project.Repository != "example.internal/team/project" {
		t.Fatalf("RegisterProject() = %#v", result)
	}
}

func TestRegisterProjectRejectsIdentityConflicts(t *testing.T) {
	t.Parallel()
	t.Run("partial explicit identity", func(t *testing.T) {
		service, _ := newProjectServiceTest(t, &projectRunner{})
		_, err := service.RegisterProject(t.Context(), RegisterProjectInput{ProjectID: "project"})
		assertAppError(t, err, ErrorUsage, nil)
	})
	t.Run("explicit identity mismatches usable origin", func(t *testing.T) {
		service, _ := newProjectServiceTest(t, &projectRunner{origin: "https://github.com/org/actual.git\n"})
		_, err := service.RegisterProject(t.Context(), RegisterProjectInput{
			Directory: "/repo", ProjectID: "org_expected", Repository: "github.com/org/expected",
		})
		assertAppError(t, err, ErrorConflict, ErrRepositoryMismatch)
	})
	t.Run("project ID collision", func(t *testing.T) {
		service, store := newProjectServiceTest(t, &projectRunner{origin: "https://github.com/other/repo.git\n"})
		if err := store.CreateProject(vault.Project{
			SchemaVersion: vault.SchemaVersion, ID: "other_repo", Repository: "gitlab.com/other/repo", TaskPrefix: "REPO",
		}); err != nil {
			t.Fatal(err)
		}
		_, err := service.RegisterProject(t.Context(), RegisterProjectInput{Directory: "/repo"})
		assertAppError(t, err, ErrorConflict, ErrRepositoryMismatch)
	})
}

func TestResolveContextUsesBranchBeforeCurrentTask(t *testing.T) {
	t.Parallel()
	runner := &projectRunner{origin: "https://github.com/org/repo.git\n", branch: "feature/one\n"}
	service, store := newProjectServiceTest(t, runner)
	project := contextTestProject("TASKCTL-002")
	if err := store.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	first := contextTestTask("TASKCTL-001", "feature/one")
	second := contextTestTask("TASKCTL-002", "feature/two")
	for _, task := range []domain.Task{first, second} {
		if err := store.CreateTask(task); err != nil {
			t.Fatal(err)
		}
	}

	resolved, err := service.ResolveContext(t.Context(), ResolveContextInput{Directory: "/repo"})
	if err != nil {
		t.Fatalf("ResolveContext() error = %v", err)
	}
	if resolved.Task.ID != first.ID || resolved.CurrentPR == nil || resolved.CurrentPR.ID != "PR-001" || resolved.Selection != SelectionBranch {
		t.Fatalf("branch ResolveContext() = %#v", resolved)
	}

	runner.branch = "feature/unassociated\n"
	resolved, err = service.ResolveContext(t.Context(), ResolveContextInput{Directory: "/repo"})
	if err != nil {
		t.Fatalf("fallback ResolveContext() error = %v", err)
	}
	if resolved.Task.ID != second.ID || resolved.CurrentPR != nil || resolved.Selection != SelectionCurrent {
		t.Fatalf("fallback ResolveContext() = %#v", resolved)
	}
}

func TestResolveContextAgainstTemporaryGitRepository(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	repositoryDirectory := filepath.Join(root, "checkout")
	for _, arguments := range [][]string{
		{"init", repositoryDirectory},
		{"-C", repositoryDirectory, "remote", "add", "origin", "git@github.com:org/repo.git"},
		{"-C", repositoryDirectory, "symbolic-ref", "HEAD", "refs/heads/feature/integration"},
	} {
		command := exec.Command("git", arguments...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", arguments, err, output)
		}
	}
	vaultRoot := filepath.Join(root, "vault")
	if _, err := vault.Initialize(vaultRoot); err != nil {
		t.Fatal(err)
	}
	store, err := vault.NewStore(vaultRoot)
	if err != nil {
		t.Fatal(err)
	}
	project := contextTestProject("TASKCTL-002")
	if err := store.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateTask(contextTestTask("TASKCTL-001", "feature/integration")); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateTask(contextTestTask("TASKCTL-002", "feature/other")); err != nil {
		t.Fatal(err)
	}

	service := NewProjectService(store, gitcli.NewClient(processutil.ExecRunner{}))
	resolved, err := service.ResolveContext(t.Context(), ResolveContextInput{Directory: repositoryDirectory})
	if err != nil {
		t.Fatalf("ResolveContext() error = %v", err)
	}
	if resolved.Task.ID != "TASKCTL-001" || resolved.CurrentPR == nil || resolved.Selection != SelectionBranch {
		t.Fatalf("ResolveContext() = %#v", resolved)
	}
}

func TestResolveContextUsesFallbackOnDetachedHEAD(t *testing.T) {
	t.Parallel()
	exitOne := &processutil.CommandError{Name: "git", ExitCode: 1, Cause: errors.New("exit status 1")}
	runner := &projectRunner{origin: "git@github.com:org/repo.git\n", branchErr: exitOne}
	service, store := newProjectServiceTest(t, runner)
	project := contextTestProject("TASKCTL-001")
	if err := store.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateTask(contextTestTask("TASKCTL-001", "feature/one")); err != nil {
		t.Fatal(err)
	}
	resolved, err := service.ResolveContext(t.Context(), ResolveContextInput{Directory: "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Task.ID != "TASKCTL-001" || resolved.Branch != "" || resolved.Selection != SelectionCurrent {
		t.Fatalf("ResolveContext() = %#v", resolved)
	}
}

func TestResolveContextRejectsAmbiguousBranchAndStaleFallback(t *testing.T) {
	t.Parallel()
	t.Run("ambiguous branch", func(t *testing.T) {
		runner := &projectRunner{origin: "https://github.com/org/repo.git\n", branch: "feature/shared\n"}
		service, store := newProjectServiceTest(t, runner)
		if err := store.CreateProject(contextTestProject("TASKCTL-001")); err != nil {
			t.Fatal(err)
		}
		for _, id := range []domain.TaskID{"TASKCTL-001", "TASKCTL-002"} {
			if err := store.CreateTask(contextTestTask(id, "feature/shared")); err != nil {
				t.Fatal(err)
			}
		}
		_, err := service.ResolveContext(t.Context(), ResolveContextInput{Directory: "/repo"})
		assertAppError(t, err, ErrorInvalidData, ErrAmbiguousBranch)
	})

	t.Run("stale current Task", func(t *testing.T) {
		runner := &projectRunner{origin: "https://github.com/org/repo.git\n", branch: "feature/none\n"}
		service, store := newProjectServiceTest(t, runner)
		if err := store.CreateProject(contextTestProject("TASKCTL-999")); err != nil {
			t.Fatal(err)
		}
		_, err := service.ResolveContext(t.Context(), ResolveContextInput{Directory: "/repo"})
		assertAppError(t, err, ErrorInvalidData, ErrStaleCurrentTask)
	})

	t.Run("no current Task", func(t *testing.T) {
		runner := &projectRunner{origin: "https://github.com/org/repo.git\n", branch: "main\n"}
		service, store := newProjectServiceTest(t, runner)
		if err := store.CreateProject(contextTestProject("")); err != nil {
			t.Fatal(err)
		}
		_, err := service.ResolveContext(t.Context(), ResolveContextInput{Directory: "/repo"})
		assertAppError(t, err, ErrorMissingContext, ErrNoCurrentTask)
	})
}

func TestResolveContextValidatesExplicitIdentityAgainstOrigin(t *testing.T) {
	t.Parallel()
	runner := &projectRunner{origin: "https://github.com/org/other.git\n", branch: "main\n"}
	service, store := newProjectServiceTest(t, runner)
	if err := store.CreateProject(contextTestProject("")); err != nil {
		t.Fatal(err)
	}
	_, err := service.ResolveContext(t.Context(), ResolveContextInput{
		Directory: "/repo", ProjectID: "org_repo", Repository: "github.com/org/repo",
	})
	assertAppError(t, err, ErrorConflict, ErrRepositoryMismatch)
}

func TestResolveContextRejectsAmbiguousRepositoryRegistration(t *testing.T) {
	t.Parallel()
	runner := &projectRunner{origin: "https://github.com/org/repo.git\n", branch: "main\n"}
	service, store := newProjectServiceTest(t, runner)
	first := contextTestProject("")
	second := first
	second.ID = "another_repo"
	for _, project := range []vault.Project{first, second} {
		if err := store.CreateProject(project); err != nil {
			t.Fatal(err)
		}
	}
	_, err := service.ResolveContext(t.Context(), ResolveContextInput{Directory: "/repo"})
	assertAppError(t, err, ErrorInvalidData, vault.ErrAmbiguous)
}

func contextTestProject(current domain.TaskID) vault.Project {
	return vault.Project{
		SchemaVersion: vault.SchemaVersion,
		ID:            "org_repo",
		Repository:    "github.com/org/repo",
		TaskPrefix:    "TASKCTL",
		CurrentTask:   current,
	}
}

func contextTestTask(id domain.TaskID, branch string) domain.Task {
	created := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	started := created.Add(time.Hour)
	task := domain.Task{
		SchemaVersion: domain.SchemaVersion,
		ID:            id,
		Title:         "Task " + string(id),
		ProjectID:     "org_repo",
		CreatedAt:     created,
		PRs:           []domain.PR{},
	}
	if branch != "" {
		task.PRs = []domain.PR{{
			ID: "PR-001", Title: "Implementation", Branch: branch, StartedAt: &started,
			Steps: []domain.Step{{ID: "STEP-001", Title: "Build", Status: domain.StepPending}},
		}}
	}
	return task
}

func assertAppError(t *testing.T, err error, wantKind ErrorKind, wantIs error) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil")
	}
	kind, ok := ErrorKindOf(err)
	if !ok || kind != wantKind {
		t.Fatalf("error = %v, kind = %v, categorized = %v, want %v", err, kind, ok, wantKind)
	}
	if wantIs != nil && !errors.Is(err, wantIs) {
		t.Fatalf("error = %v, want errors.Is(_, %v)", err, wantIs)
	}
}
