package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hossainemruz/taskctl/internal/domain"
	processutil "github.com/hossainemruz/taskctl/internal/process"
)

func TestWorkflowIncrementalExecutionReviewAndReopening(t *testing.T) {
	t.Parallel()
	workflow, runner, store, input := newWorkflowTest(t)
	created, planPath := prepareExecutionTask(t, workflow, input,
		"### PR-001: Implementation\n\n#### STEP-001: Build lifecycle\n",
		`{"prs":[{"id":"PR-001","title":"Implementation","steps":[{"id":"STEP-001","title":"Build lifecycle"}]}]}`)
	research, err := workflow.EnsureArtifact(t.Context(), input, "research")
	if err != nil {
		t.Fatal(err)
	}

	runner.branch = "feature/team/execution\n"
	started, err := workflow.StartPR(t.Context(), input, "PR-001")
	if err != nil {
		t.Fatal(err)
	}
	if started.Status != domain.PRInProgress || started.Branch != "feature/team/execution" {
		t.Fatalf("StartPR() = %#v", started)
	}

	selected, err := workflow.GetStep(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if selected.TaskID != created.ID || selected.PRID != "PR-001" || selected.StepID != "STEP-001" || selected.Status != domain.StepPending {
		t.Fatalf("GetStep() = %#v", selected)
	}
	if selected.Artifacts.Task != filepath.Join(filepath.Dir(planPath), "task.md") || selected.Artifacts.Plan != planPath ||
		selected.Artifacts.Research != research.Path || selected.Artifacts.Review != "" {
		t.Fatalf("GetStep() artifacts = %#v", selected.Artifacts)
	}

	for _, transition := range []struct {
		name string
		run  func() (StepListItem, error)
		want domain.StepStatus
	}{
		{name: "start", run: func() (StepListItem, error) { return workflow.StartStep(t.Context(), input, "") }, want: domain.StepInProgress},
		{name: "submit", run: func() (StepListItem, error) { return workflow.SubmitStep(t.Context(), input, "") }, want: domain.StepReadyForReview},
		{name: "revise", run: func() (StepListItem, error) { return workflow.ReviseStep(t.Context(), input, "") }, want: domain.StepInProgress},
		{name: "resubmit", run: func() (StepListItem, error) { return workflow.SubmitStep(t.Context(), input, "") }, want: domain.StepReadyForReview},
		{name: "complete", run: func() (StepListItem, error) { return workflow.CompleteStep(t.Context(), input, "") }, want: domain.StepCompleted},
	} {
		item, transitionErr := transition.run()
		if transitionErr != nil || item.Status != transition.want {
			t.Fatalf("%s = %#v, %v", transition.name, item, transitionErr)
		}
		if transition.want == domain.StepInProgress || transition.want == domain.StepReadyForReview {
			current, getErr := workflow.GetStep(t.Context(), input)
			if getErr != nil || current.StepID != item.ID || current.Status != transition.want {
				t.Fatalf("GetStep() after %s = %#v, %v", transition.name, current, getErr)
			}
		}
	}
	task, err := store.LoadTask("org_repo", created.ID)
	if err != nil || task.PRs[0].Status() != domain.PRCompleted || task.Status() != domain.TaskCompleted {
		t.Fatalf("completed Task = %#v, %v", task, err)
	}
	if _, err := workflow.GetStep(t.Context(), input); !hasErrorKind(err, ErrorNotFound) {
		t.Fatalf("GetStep() after completion error = %v", err)
	}

	correction, err := workflow.AddStep(t.Context(), input, "PR-001", "Address final review")
	if err != nil || correction != "STEP-002" {
		t.Fatalf("AddStep() = %s, %v", correction, err)
	}
	appendFile(t, planPath, "\n#### STEP-002: Address final review\n")
	task, err = store.LoadTask("org_repo", created.ID)
	if err != nil || task.PRs[0].Status() != domain.PRInProgress || task.Status() != domain.TaskInProgress {
		t.Fatalf("reopened Task = %#v, %v", task, err)
	}
	selected, err = workflow.GetStep(t.Context(), input)
	if err != nil || selected.StepID != correction || selected.Status != domain.StepPending {
		t.Fatalf("corrective GetStep() = %#v, %v", selected, err)
	}
	if _, err := workflow.StartStep(t.Context(), input, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.SubmitStep(t.Context(), input, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.CompleteStep(t.Context(), input, ""); err != nil {
		t.Fatal(err)
	}
	task, err = store.LoadTask("org_repo", created.ID)
	if err != nil || task.PRs[0].Status() != domain.PRCompleted || task.Status() != domain.TaskCompleted {
		t.Fatalf("recompleted Task = %#v, %v", task, err)
	}
	if projected := string(mustReadFile(t, planPath)); !strings.Contains(projected, "STEP-002: Address final review — Completed") {
		t.Fatalf("final plan projection = %q", projected)
	}

	runner.branch = "feature/unassociated\n"
	if _, err := workflow.GetStep(t.Context(), input); !hasErrorKind(err, ErrorMissingContext) {
		t.Fatalf("GetStep() on wrong branch error = %v", err)
	}
}

func TestWorkflowStepTransitionPreparesProjectionBeforeSaving(t *testing.T) {
	t.Parallel()
	workflow, runner, store, input := newWorkflowTest(t)
	created, planPath := prepareExecutionTask(t, workflow, input,
		"### PR-001: Implementation\n\n#### STEP-001: Build\n",
		`{"prs":[{"id":"PR-001","title":"Implementation","steps":[{"id":"STEP-001","title":"Build"}]}]}`)
	runner.branch = "feature/projection\n"
	if _, err := workflow.StartPR(t.Context(), input, "PR-001"); err != nil {
		t.Fatal(err)
	}
	broken := strings.Replace(string(mustReadFile(t, planPath)), "<!-- taskctl:progress:end -->", "", 1)
	if err := os.WriteFile(planPath, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.StartStep(t.Context(), input, ""); !hasErrorKind(err, ErrorInvalidData) {
		t.Fatalf("StartStep() projection error = %v", err)
	}
	task, err := store.LoadTask("org_repo", created.ID)
	if err != nil || task.PRs[0].Steps[0].Status != domain.StepPending {
		t.Fatalf("failed projection preparation changed Task = %#v, %v", task, err)
	}
}

func TestWorkflowExecutionValidatesBranchPlanAndCurrentPR(t *testing.T) {
	t.Parallel()
	workflow, runner, _, input := newWorkflowTest(t)
	_, planPath := prepareExecutionTask(t, workflow, input,
		"### PR-001: First\n\n#### STEP-001: One\n\n### PR-002: Second\n\n#### STEP-002: Two\n",
		`{"prs":[{"id":"PR-001","title":"First","steps":[{"id":"STEP-001","title":"One"}]},{"id":"PR-002","title":"Second","steps":[{"id":"STEP-002","title":"Two"}]}]}`)

	runner.branchErr = &processutil.CommandError{Name: "git", ExitCode: 1, Cause: errors.New("exit status 1")}
	if _, err := workflow.StartPR(t.Context(), input, "PR-001"); !hasErrorKind(err, ErrorMissingContext) {
		t.Fatalf("StartPR() detached error = %v", err)
	}
	runner.branchErr = nil
	runner.branch = "feature/first\n"
	validPlan := mustReadFile(t, planPath)
	brokenPlan := strings.Replace(string(validPlan), "### PR-001: First\n\n#### STEP-001: One\n\n", "", 1)
	if err := os.WriteFile(planPath, []byte(brokenPlan), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.StartPR(t.Context(), input, "PR-001"); !hasErrorKind(err, ErrorInvalidData) {
		t.Fatalf("StartPR() heading error = %v", err)
	}
	if err := os.WriteFile(planPath, validPlan, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.StartPR(t.Context(), input, "PR-001"); err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.StartPR(t.Context(), input, "PR-001"); !hasErrorKind(err, ErrorConflict) {
		t.Fatalf("second StartPR() error = %v", err)
	}
	if _, err := workflow.StartPR(t.Context(), input, "PR-002"); !hasErrorKind(err, ErrorConflict) {
		t.Fatalf("StartPR() on an associated branch error = %v", err)
	}

	runner.branch = "feature/second\n"
	if _, err := workflow.StartPR(t.Context(), input, "PR-002"); err != nil {
		t.Fatal(err)
	}
	runner.branch = "feature/first\n"
	if _, err := workflow.StartStep(t.Context(), input, "STEP-002"); !hasErrorKind(err, ErrorConflict) {
		t.Fatalf("StartStep() for another PR error = %v", err)
	}
	if id, err := workflow.AddPR(t.Context(), input, "Empty"); err != nil || id != "PR-003" {
		t.Fatalf("AddPR(empty) = %s, %v", id, err)
	}
	appendFile(t, planPath, "\n### PR-003: Empty\n")
	runner.branch = "feature/empty\n"
	if _, err := workflow.StartPR(t.Context(), input, "PR-003"); !hasErrorKind(err, ErrorConflict) {
		t.Fatalf("StartPR(empty) error = %v", err)
	}
}

func TestWorkflowStepSkipAndReopenDefaults(t *testing.T) {
	t.Parallel()
	workflow, runner, _, input := newWorkflowTest(t)
	_, _ = prepareExecutionTask(t, workflow, input,
		"### PR-001: Implementation\n\n#### STEP-001: Optional\n",
		`{"prs":[{"id":"PR-001","title":"Implementation","steps":[{"id":"STEP-001","title":"Optional"}]}]}`)
	runner.branch = "feature/optional\n"
	if _, err := workflow.StartPR(t.Context(), input, "PR-001"); err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.SkipStep(t.Context(), input, "", "not needed"); err != nil {
		t.Fatal(err)
	}
	item, err := workflow.ReopenStep(t.Context(), input, "")
	if err != nil || item.Status != domain.StepPending || item.SkipReason != "" {
		t.Fatalf("ReopenStep() = %#v, %v", item, err)
	}
}

func prepareExecutionTask(t *testing.T, workflow *Workflow, input ProjectInput, headings, structured string) (domain.Task, string) {
	t.Helper()
	created, err := workflow.NewTask(t.Context(), NewTaskInput{ProjectInput: input, Title: "Execution", TaskPrefix: "EXEC"})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := workflow.EnsureArtifact(t.Context(), input, "plan")
	if err != nil {
		t.Fatal(err)
	}
	markdown := "# Plan\n\n## Progress\n\n<!-- taskctl:progress:start -->\n\nold\n\n<!-- taskctl:progress:end -->\n\n## Plan\n\n" + headings
	if err := os.WriteFile(artifact.Path, []byte(markdown), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.ApplyPlan(t.Context(), input, strings.NewReader(structured)); err != nil {
		t.Fatal(err)
	}
	return created.Task, artifact.Path
}

func appendFile(t *testing.T, path, contents string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(contents); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
