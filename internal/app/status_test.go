package app

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/hossainemruz/taskctl/internal/config"
	"github.com/hossainemruz/taskctl/internal/domain"
	processutil "github.com/hossainemruz/taskctl/internal/process"
	"github.com/hossainemruz/taskctl/internal/vault"
)

func TestWorkflowContextUsesBranchScopedProgressAndSparseArtifacts(t *testing.T) {
	t.Parallel()
	workflow, runner, store, input := newWorkflowTest(t)
	created, err := workflow.NewTask(t.Context(), NewTaskInput{ProjectInput: input, Title: "Status", TaskPrefix: "STAT"})
	if err != nil {
		t.Fatal(err)
	}
	research, err := workflow.EnsureArtifact(t.Context(), input, "research")
	if err != nil {
		t.Fatal(err)
	}
	draft, err := workflow.Context(t.Context(), input)
	if err != nil || draft.Status != domain.TaskDraft || draft.Progress != (domain.Progress{}) || draft.CurrentPR != nil {
		t.Fatalf("draft Context() = %#v, %v", draft, err)
	}
	task := contextStatusTask(created.Task)
	if err := store.SaveTask(task); err != nil {
		t.Fatal(err)
	}

	runner.branch = "feature/one\n"
	result, err := workflow.Context(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.ProjectID != "org_repo" || result.TaskID != task.ID || result.Title != "Status" || result.Status != domain.TaskInProgress ||
		result.Progress != (domain.Progress{Skipped: 1, Total: 3}) {
		t.Fatalf("Context() = %#v", result)
	}
	if result.CurrentPR == nil || result.CurrentPR.ID != "PR-001" || result.CurrentPR.Status != domain.PRInProgress ||
		result.CurrentPR.Progress != (domain.Progress{Completed: 1, Skipped: 1, Total: 3}) ||
		result.CurrentPR.ActiveStep == nil || result.CurrentPR.ActiveStep.ID != "STEP-003" ||
		result.CurrentPR.ActiveStep.Status != domain.StepReadyForReview {
		t.Fatalf("Context() current PR = %#v", result.CurrentPR)
	}
	if result.Artifacts.Task != filepath.Join(created.Directory, "task.md") || result.Artifacts.Research != research.Path ||
		result.Artifacts.Plan != "" || result.Artifacts.Review != "" {
		t.Fatalf("Context() artifacts = %#v", result.Artifacts)
	}

	runner.branch = "main\n"
	fallback, err := workflow.Context(t.Context(), input)
	if err != nil || fallback.TaskID != task.ID || fallback.CurrentPR != nil {
		t.Fatalf("fallback Context() = %#v, %v", fallback, err)
	}

	runner.branch = "feature/one\n"
	task.PRs[0].Steps[2].Status = domain.StepCompleted
	if err := store.SaveTask(task); err != nil {
		t.Fatal(err)
	}
	completed, err := workflow.Context(t.Context(), input)
	if err != nil || completed.CurrentPR == nil || completed.CurrentPR.Status != domain.PRCompleted || completed.CurrentPR.ActiveStep != nil {
		t.Fatalf("completed-branch Context() = %#v, %v", completed, err)
	}
}

func TestWorkflowDetailedStatusMarksCurrentAndActiveState(t *testing.T) {
	t.Parallel()
	workflow, runner, store, input := newWorkflowTest(t)
	created, err := workflow.NewTask(t.Context(), NewTaskInput{ProjectInput: input, Title: "Status", TaskPrefix: "STAT"})
	if err != nil {
		t.Fatal(err)
	}
	task := contextStatusTask(created.Task)
	if err := store.SaveTask(task); err != nil {
		t.Fatal(err)
	}
	runner.branch = "feature/one\n"
	result, err := workflow.Status(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.CurrentPR != "PR-001" || result.ActiveStep != "STEP-003" || len(result.PRs) != 3 ||
		!result.PRs[0].Current || result.PRs[1].Current || !result.PRs[0].Steps[2].Active ||
		result.PRs[0].Steps[1].SkipReason != "superseded" || result.PRs[2].SkipReason != "deferred" {
		t.Fatalf("Status() = %#v", result)
	}
	if result.Vault.State != VaultStatusNotRepository {
		t.Fatalf("Status() vault = %#v", result.Vault)
	}
}

func TestWorkflowContextBranchPrecedenceAcrossCopiedVaults(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	vaultOne := filepath.Join(root, "vault-one")
	if _, err := vault.Initialize(vaultOne); err != nil {
		t.Fatal(err)
	}
	store, err := vault.NewStore(vaultOne)
	if err != nil {
		t.Fatal(err)
	}
	project := contextTestProject("TASKCTL-002")
	if err := store.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	for _, task := range []domain.Task{
		contextTestTask("TASKCTL-001", "feature/one"),
		contextTestTask("TASKCTL-002", "feature/two"),
	} {
		if err := store.CreateTask(task); err != nil {
			t.Fatal(err)
		}
	}
	vaultTwo := filepath.Join(root, "vault-two")
	copyTree(t, vaultOne, vaultTwo)

	repoOne := filepath.Join(root, "checkout-one")
	repoTwo := filepath.Join(root, "different", "checkout-two")
	for _, fixture := range []struct {
		path   string
		branch string
	}{
		{path: repoOne, branch: "feature/one"},
		{path: repoTwo, branch: "feature/unassociated"},
	} {
		runAppGit(t, "init", fixture.path)
		runAppGit(t, "-C", fixture.path, "remote", "add", "origin", "git@github.com:org/repo.git")
		runAppGit(t, "-C", fixture.path, "symbolic-ref", "HEAD", "refs/heads/"+fixture.branch)
	}

	environmentOne := workflowEnvironment{home: filepath.Join(root, "home-one"), working: repoOne, xdg: filepath.Join(root, "config-one")}
	environmentTwo := workflowEnvironment{home: filepath.Join(root, "home-two"), working: repoTwo, xdg: filepath.Join(root, "config-two")}
	saveWorkflowConfig(t, environmentOne, vaultOne)
	saveWorkflowConfig(t, environmentTwo, vaultTwo)
	one, err := NewWorkflow(environmentOne, processutil.ExecRunner{}).Context(t.Context(), ProjectInput{Directory: repoOne})
	if err != nil || one.TaskID != "TASKCTL-001" || one.CurrentPR == nil {
		t.Fatalf("branch context = %#v, %v", one, err)
	}
	two, err := NewWorkflow(environmentTwo, processutil.ExecRunner{}).Context(t.Context(), ProjectInput{Directory: repoTwo})
	if err != nil || two.TaskID != "TASKCTL-002" || two.CurrentPR != nil {
		t.Fatalf("copied-vault fallback context = %#v, %v", two, err)
	}
}

func contextStatusTask(base domain.Task) domain.Task {
	started := base.CreatedAt.Add(time.Hour)
	skipped := started.Add(time.Hour)
	base.PRs = []domain.PR{
		{
			ID: "PR-001", Title: "Current delivery", Branch: "feature/one", StartedAt: &started,
			Steps: []domain.Step{
				{ID: "STEP-001", Title: "Accepted", Status: domain.StepCompleted},
				{ID: "STEP-002", Title: "Removed", Status: domain.StepSkipped, SkipReason: "superseded"},
				{ID: "STEP-003", Title: "Review", Status: domain.StepReadyForReview},
			},
		},
		{
			ID: "PR-002", Title: "Other delivery", Branch: "feature/two", StartedAt: &started,
			Steps: []domain.Step{{ID: "STEP-004", Title: "Other work", Status: domain.StepInProgress}},
		},
		{
			ID: "PR-003", Title: "Deferred", SkippedAt: &skipped, SkipReason: "deferred",
			Steps: []domain.Step{{ID: "STEP-005", Title: "Unused", Status: domain.StepPending}},
		},
	}
	return base
}

func saveWorkflowConfig(t *testing.T, environment workflowEnvironment, vaultPath string) {
	t.Helper()
	path, err := (config.PathResolver{Environment: environment}).Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.NewStore(path).Save(config.Config{SchemaVersion: config.SchemaVersion, Vault: vaultPath,
		Viewer: config.Viewer{Command: "typora", Args: []string{}}}); err != nil {
		t.Fatal(err)
	}
}

func copyTree(t *testing.T, source, destination string) {
	t.Helper()
	if err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, contents, 0o644)
	}); err != nil {
		t.Fatal(err)
	}
}

func runAppGit(t *testing.T, arguments ...string) {
	t.Helper()
	command := exec.Command("git", arguments...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
}
