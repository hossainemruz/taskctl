package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hossainemruz/taskctl/internal/config"
	"github.com/hossainemruz/taskctl/internal/domain"
	processutil "github.com/hossainemruz/taskctl/internal/process"
	"github.com/hossainemruz/taskctl/internal/vault"
)

type workflowEnvironment struct {
	home, working, xdg string
}

func (e workflowEnvironment) GOOS() string { return "linux" }
func (e workflowEnvironment) LookupEnv(key string) (string, bool) {
	if key == "XDG_CONFIG_HOME" {
		return e.xdg, true
	}
	return "", false
}
func (e workflowEnvironment) UserHomeDir() (string, error)      { return e.home, nil }
func (e workflowEnvironment) WorkingDirectory() (string, error) { return e.working, nil }

func newWorkflowTest(t *testing.T) (*Workflow, *projectRunner, *vault.Store, ProjectInput) {
	t.Helper()
	root := t.TempDir()
	environment := workflowEnvironment{home: filepath.Join(root, "home"), working: filepath.Join(root, "repo"), xdg: filepath.Join(root, "config")}
	vaultRoot := filepath.Join(root, "vault")
	if _, err := vault.Initialize(vaultRoot); err != nil {
		t.Fatal(err)
	}
	configPath, err := (config.PathResolver{Environment: environment}).Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.NewStore(configPath).Save(config.Config{SchemaVersion: config.SchemaVersion, Vault: vaultRoot,
		Viewer: config.Viewer{Command: "open", Args: []string{"-a", "Typora"}}}); err != nil {
		t.Fatal(err)
	}
	store, err := vault.NewStore(vaultRoot)
	if err != nil {
		t.Fatal(err)
	}
	runner := &projectRunner{origin: "git@github.com:org/repo.git\n", branch: "main\n"}
	workflow := NewWorkflow(environment, runner)
	fixed := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	workflow.SetClock(func() time.Time { return fixed })
	return workflow, runner, store, ProjectInput{Directory: environment.working}
}

func TestWorkflowTaskCatalogAndCreationRollback(t *testing.T) {
	t.Parallel()
	workflow, runner, store, projectInput := newWorkflowTest(t)
	defaults, err := workflow.NewDefaults(t.Context(), projectInput)
	if err != nil {
		t.Fatal(err)
	}
	if defaults.ProjectRegistered || defaults.ProjectID != "org_repo" || defaults.TaskPrefix != "REPO" {
		t.Fatalf("NewDefaults() = %#v", defaults)
	}
	first, err := workflow.NewTask(t.Context(), NewTaskInput{ProjectInput: projectInput, Title: "First", TaskPrefix: "WORK"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := workflow.NewTask(t.Context(), NewTaskInput{ProjectInput: projectInput, Title: "Second"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Task.ID != "WORK-001" || second.Task.ID != "WORK-002" {
		t.Fatalf("allocated IDs = %s, %s", first.Task.ID, second.Task.ID)
	}
	entries, err := os.ReadDir(first.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("new Task entries = %d, want task.yaml and task.md", len(entries))
	}
	markdown, err := os.ReadFile(filepath.Join(first.Directory, "task.md"))
	if err != nil || !strings.Contains(string(markdown), "Created: 2026-07-19T12:00:00Z") {
		t.Fatalf("task.md = %q, error = %v", markdown, err)
	}

	items, err := workflow.ListTasks(t.Context(), projectInput)
	if err != nil || len(items) != 2 || items[0].Current || !items[1].Current {
		t.Fatalf("ListTasks() = %#v, error = %v", items, err)
	}
	if _, err := workflow.UseTask(t.Context(), projectInput, "WORK-001"); err != nil {
		t.Fatal(err)
	}
	cancelled, err := workflow.CancelTask(t.Context(), projectInput, "")
	if err != nil || cancelled.ID != "WORK-001" || cancelled.Status() != domain.TaskCancelled {
		t.Fatalf("CancelTask() = %#v, error = %v", cancelled, err)
	}
	if _, err := workflow.CancelTask(t.Context(), projectInput, "WORK-001"); err == nil {
		t.Fatal("second cancellation succeeded")
	}
	runner.origin = "https://github.com/org/other.git\n"
	other, err := workflow.NewTask(t.Context(), NewTaskInput{ProjectInput: projectInput, Title: "Other project", TaskPrefix: "OTHER"})
	if err != nil {
		t.Fatal(err)
	}
	otherItems, err := workflow.ListTasks(t.Context(), projectInput)
	if err != nil || len(otherItems) != 1 || otherItems[0].ID != other.Task.ID {
		t.Fatalf("other project Tasks = %#v, error = %v", otherItems, err)
	}
	runner.origin = "git@github.com:org/repo.git\n"

	badTemplate := filepath.Join(store.Root(), vault.TemplatesDirName, "task.md.tmpl")
	if err := os.WriteFile(badTemplate, []byte("{{ .Unknown }}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.NewTask(t.Context(), NewTaskInput{ProjectInput: projectInput, Title: "Third"}); err == nil {
		t.Fatal("NewTask() accepted malformed template")
	}
	if _, err := store.LoadTask("org_repo", "WORK-003"); !errors.Is(err, vault.ErrNotFound) {
		t.Fatalf("failed rendering created Task: %v", err)
	}
	project, err := store.LoadProject("org_repo")
	if err != nil || project.CurrentTask != "WORK-001" {
		t.Fatalf("current Task after failure = %s, error = %v", project.CurrentTask, err)
	}
}

func TestWorkflowFailedFirstRenderDoesNotRegisterProject(t *testing.T) {
	t.Parallel()
	workflow, _, store, projectInput := newWorkflowTest(t)
	templatePath := filepath.Join(store.Root(), vault.TemplatesDirName, "task.md.tmpl")
	if err := os.WriteFile(templatePath, []byte("{{ .Unknown }}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.NewTask(t.Context(), NewTaskInput{ProjectInput: projectInput, Title: "First", TaskPrefix: "WORK"}); !hasErrorKind(err, ErrorInvalidData) {
		t.Fatalf("NewTask() error = %v", err)
	}
	projects, err := store.ListProjects()
	if err != nil || len(projects) != 0 {
		t.Fatalf("project registrations = %#v, error = %v", projects, err)
	}
}

func TestWorkflowArtifactsAreLazyIdempotentAndViewerIsDirect(t *testing.T) {
	t.Parallel()
	workflow, runner, store, projectInput := newWorkflowTest(t)
	created, err := workflow.NewTask(t.Context(), NewTaskInput{ProjectInput: projectInput, Title: "Artifacts", TaskPrefix: "ART"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.ArtifactPath(t.Context(), projectInput, "research"); !hasErrorKind(err, ErrorNotFound) {
		t.Fatalf("missing ArtifactPath() error = %v", err)
	}
	templatePath := filepath.Join(store.Root(), vault.TemplatesDirName, "research.md.tmpl")
	if err := os.WriteFile(templatePath, []byte("# Custom {{ .TaskID }} / {{ .Title }}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := workflow.EnsureArtifact(t.Context(), projectInput, "research")
	if err != nil || !result.Created {
		t.Fatalf("EnsureArtifact() = %#v, error = %v", result, err)
	}
	if got, _ := os.ReadFile(result.Path); string(got) != "# Custom ART-001 / Artifacts\n" {
		t.Fatalf("research.md = %q", got)
	}
	for _, name := range []string{"plan", "review"} {
		artifact, ensureErr := workflow.EnsureArtifact(t.Context(), projectInput, name)
		if ensureErr != nil || !artifact.Created || !filepath.IsAbs(artifact.Path) {
			t.Fatalf("EnsureArtifact(%s) = %#v, error = %v", name, artifact, ensureErr)
		}
	}
	const edited = "# Edited by user\n"
	if err := os.WriteFile(result.Path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(templatePath, []byte("{{ broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	again, err := workflow.EnsureArtifact(t.Context(), projectInput, "research")
	if err != nil || again.Created || again.Path != result.Path {
		t.Fatalf("second EnsureArtifact() = %#v, error = %v", again, err)
	}
	if got, _ := os.ReadFile(result.Path); string(got) != edited {
		t.Fatalf("existing artifact overwritten = %q", got)
	}
	if _, err := workflow.EnsureArtifact(t.Context(), projectInput, "task"); !hasErrorKind(err, ErrorUsage) {
		t.Fatalf("ensure task error = %v", err)
	}

	directory, err := workflow.ViewArtifacts(t.Context(), projectInput)
	if err != nil {
		t.Fatal(err)
	}
	if directory != created.Directory || len(runner.commands) == 0 {
		t.Fatalf("ViewArtifacts() directory = %q, commands = %#v", directory, runner.commands)
	}
	viewer := runner.commands[len(runner.commands)-1]
	if viewer.Name != "open" || strings.Join(viewer.Args, "|") != "-a|Typora|"+created.Directory {
		t.Fatalf("viewer command = %#v", viewer)
	}
	runner.startErr = errors.New("viewer unavailable")
	if _, err := workflow.ViewArtifacts(t.Context(), projectInput); !hasErrorKind(err, ErrorExternalCommand) {
		t.Fatalf("viewer failure = %v", err)
	}
}

func TestWorkflowExplicitIdentityWorksOutsideGit(t *testing.T) {
	t.Parallel()
	workflow, runner, _, _ := newWorkflowTest(t)
	runner.originErr = errors.New("Git must not be inspected")
	runner.branchErr = errors.New("Git must not be inspected")
	input := ProjectInput{ProjectID: "offline_project", Repository: "example.com/team/project"}
	created, err := workflow.NewTask(t.Context(), NewTaskInput{ProjectInput: input, Title: "Offline", TaskPrefix: "OFF"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Task.ID != "OFF-001" {
		t.Fatalf("created Task = %#v", created.Task)
	}
	if _, err := workflow.EnsureArtifact(t.Context(), input, "research"); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("explicit identity invoked processes: %#v", runner.commands)
	}
}

func TestWorkflowStructuredPlanningEvolutionAndProjection(t *testing.T) {
	t.Parallel()
	workflow, _, store, projectInput := newWorkflowTest(t)
	created, err := workflow.NewTask(t.Context(), NewTaskInput{ProjectInput: projectInput, Title: "Planning", TaskPrefix: "PLAN"})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := workflow.EnsureArtifact(t.Context(), projectInput, "plan")
	if err != nil {
		t.Fatal(err)
	}
	writePlan := func(headings string) {
		t.Helper()
		contents := "# Plan\n\n## Progress\n\n<!-- taskctl:progress:start -->\n\nold\n\n<!-- taskctl:progress:end -->\n\n## Plan\n\n" + headings
		if err := os.WriteFile(artifact.Path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	writePlan("### PR-001: Storage\n\n#### STEP-001: Schema\n")
	initialJSON := `{"prs":[{"id":"PR-001","title":"Storage","steps":[{"id":"STEP-001","title":"Schema"}]}]}`
	result, err := workflow.ApplyPlan(t.Context(), projectInput, strings.NewReader(initialJSON))
	if err != nil || result.PRCount != 1 || result.StepCount != 1 {
		t.Fatalf("ApplyPlan(initial) = %#v, %v", result, err)
	}
	projection, _ := os.ReadFile(artifact.Path)
	if !strings.Contains(string(projection), "- PR-001: Storage — Pending") || !strings.Contains(string(projection), "STEP-001: Schema — Pending") {
		t.Fatalf("initial projection = %q", projection)
	}

	writePlan("### PR-001: Storage\n\n#### STEP-001: Schema\n\n### PR-002: CLI\n\n#### STEP-002: Commands\n")
	replacementJSON := `{"prs":[{"id":"PR-001","title":"Storage","steps":[{"id":"STEP-001","title":"Schema"}]},{"id":"PR-002","title":"CLI","steps":[{"id":"STEP-002","title":"Commands"}]}]}`
	if _, err := workflow.ApplyPlan(t.Context(), projectInput, strings.NewReader(replacementJSON)); err != nil {
		t.Fatalf("ApplyPlan(draft replacement) error = %v", err)
	}
	task, err := store.LoadTask("org_repo", created.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := task.StartPR("PR-001", "feat/storage", task.CreatedAt); err != nil {
		t.Fatal(err)
	}
	startedAt := *task.PRs[0].StartedAt
	if err := store.SaveTask(task); err != nil {
		t.Fatal(err)
	}

	writePlan("### PR-001: Revised storage\n\n#### STEP-001: Revised schema\n\n### PR-002: CLI\n\n#### STEP-002: Commands\n")
	correctedJSON := `{"prs":[{"id":"PR-001","title":"Revised storage","steps":[{"id":"STEP-001","title":"Revised schema"}]},{"id":"PR-002","title":"CLI","steps":[{"id":"STEP-002","title":"Commands"}]}]}`
	if _, err := workflow.ApplyPlan(t.Context(), projectInput, strings.NewReader(correctedJSON)); err != nil {
		t.Fatalf("ApplyPlan(title correction) error = %v", err)
	}
	task, err = store.LoadTask("org_repo", created.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task.PRs[0].Title != "Revised storage" || task.PRs[0].Branch != "feat/storage" || !task.PRs[0].StartedAt.Equal(startedAt) {
		t.Fatalf("corrected task = %#v", task)
	}
	if _, err := workflow.ApplyPlan(t.Context(), projectInput, strings.NewReader(initialJSON)); !hasErrorKind(err, ErrorConflict) {
		t.Fatalf("post-start topology replacement error = %v", err)
	}

	broken := strings.Replace(string(mustReadFile(t, artifact.Path)), "<!-- taskctl:progress:end -->", "", 1)
	if err := os.WriteFile(artifact.Path, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := store.LoadTask("org_repo", created.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.AddStep(t.Context(), projectInput, "PR-001", "Must not persist"); !hasErrorKind(err, ErrorInvalidData) {
		t.Fatalf("AddStep() marker error = %v", err)
	}
	after, err := store.LoadTask("org_repo", created.Task.ID)
	if err != nil || len(after.PRs[0].Steps) != len(before.PRs[0].Steps) {
		t.Fatalf("failed projection preparation changed Task: %#v, %v", after, err)
	}

	writePlan("### PR-001: Revised storage\n\n#### STEP-001: Revised schema\n\n### PR-002: CLI\n\n#### STEP-002: Commands\n")
	stepID, err := workflow.AddStep(t.Context(), projectInput, "PR-001", "Address review")
	if err != nil || stepID != "STEP-003" {
		t.Fatalf("AddStep() = %s, %v", stepID, err)
	}
	prID, err := workflow.AddPR(t.Context(), projectInput, "Documentation")
	if err != nil || prID != "PR-003" {
		t.Fatalf("AddPR() = %s, %v", prID, err)
	}
	lastStep, err := workflow.AddStep(t.Context(), projectInput, string(prID), "Write docs")
	if err != nil || lastStep != "STEP-004" {
		t.Fatalf("AddStep(new PR) = %s, %v", lastStep, err)
	}
	steps, err := workflow.ListSteps(t.Context(), projectInput)
	if err != nil || len(steps) != 4 || steps[1].ID != "STEP-003" || steps[2].ID != "STEP-002" {
		t.Fatalf("ListSteps() = %#v, %v", steps, err)
	}
	prs, err := workflow.ListPRs(t.Context(), projectInput)
	if err != nil || len(prs) != 3 || prs[2].ID != "PR-003" {
		t.Fatalf("ListPRs() = %#v, %v", prs, err)
	}
	if item, err := workflow.SkipStep(t.Context(), projectInput, string(stepID), "not needed"); err != nil || item.Status != domain.StepSkipped {
		t.Fatalf("SkipStep() = %#v, %v", item, err)
	}
	if item, err := workflow.SkipPR(t.Context(), projectInput, string(prID), "deferred"); err != nil || item.Status != domain.PRSkipped {
		t.Fatalf("SkipPR() = %#v, %v", item, err)
	}
	projection = mustReadFile(t, artifact.Path)
	if !strings.Contains(string(projection), "STEP-003: Address review — Skipped") || !strings.Contains(string(projection), "PR-003: Documentation — Skipped") {
		t.Fatalf("evolved projection = %q", projection)
	}
}

func TestWorkflowApplyPlanRejectsInputAndHeadingMismatches(t *testing.T) {
	t.Parallel()
	workflow, _, _, projectInput := newWorkflowTest(t)
	if _, err := workflow.NewTask(t.Context(), NewTaskInput{ProjectInput: projectInput, Title: "Planning", TaskPrefix: "PLAN"}); err != nil {
		t.Fatal(err)
	}
	artifact, err := workflow.EnsureArtifact(t.Context(), projectInput, "plan")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.ApplyPlan(t.Context(), projectInput, strings.NewReader(`{"prs":[],"unknown":true}`)); !hasErrorKind(err, ErrorUsage) {
		t.Fatalf("unknown JSON field error = %v", err)
	}
	contents := strings.Replace(string(mustReadFile(t, artifact.Path)), "Use `### PR-NNN: Title`", "### PR-001: Wrong\n\n#### STEP-001: Schema\n\nUse `### PR-NNN: Title`", 1)
	if err := os.WriteFile(artifact.Path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	input := `{"prs":[{"id":"PR-001","title":"Storage","steps":[{"id":"STEP-001","title":"Schema"}]}]}`
	if _, err := workflow.ApplyPlan(t.Context(), projectInput, strings.NewReader(input)); !hasErrorKind(err, ErrorInvalidData) {
		t.Fatalf("heading mismatch error = %v", err)
	}
}

func TestSaveTaskAndProjectionReportsCanonicalPartialUpdate(t *testing.T) {
	t.Parallel()
	workflow, _, store, projectInput := newWorkflowTest(t)
	created, err := workflow.NewTask(t.Context(), NewTaskInput{ProjectInput: projectInput, Title: "Before", TaskPrefix: "PART"})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := workflow.EnsureArtifact(t.Context(), projectInput, "plan")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(artifact.Path); err != nil {
		t.Fatal(err)
	}
	candidate := created.Task.Clone()
	candidate.Title = "After"
	err = saveTaskAndProjection(store, &ProjectService{store: store}, candidate, []byte("projection"))
	if !hasErrorKind(err, ErrorPartialUpdate) {
		t.Fatalf("saveTaskAndProjection() error = %v, want ErrorPartialUpdate", err)
	}
	saved, loadErr := store.LoadTask(candidate.ProjectID, candidate.ID)
	if loadErr != nil || saved.Title != "After" {
		t.Fatalf("canonical Task was not retained: %#v, %v", saved, loadErr)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return contents
}

func hasErrorKind(err error, wanted ErrorKind) bool {
	kind, ok := ErrorKindOf(err)
	return ok && kind == wanted
}

var _ processutil.Runner = (*projectRunner)(nil)
