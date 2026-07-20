package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/hossainemruz/taskctl/internal/process"
)

type mvpCLIFixture struct {
	t           *testing.T
	repository  string
	vault       string
	environment cliEnvironment
}

func newMVPCLIFixture(t *testing.T) *mvpCLIFixture {
	t.Helper()
	root := t.TempDir()
	repository := filepath.Join(root, "checkout in a different location")
	runCLIGit(t, "init", repository)
	runCLIGit(t, "-C", repository, "remote", "add", "origin", "https://example.com/acme/taskctl.git")
	runCLIGit(t, "-C", repository, "symbolic-ref", "HEAD", "refs/heads/feature/delivery")
	return &mvpCLIFixture{
		t:          t,
		repository: repository,
		vault:      filepath.Join(root, "synchronized vault"),
		environment: cliEnvironment{
			home:    filepath.Join(root, "home"),
			working: repository,
			xdg:     filepath.Join(root, "config"),
		},
	}
}

func (f *mvpCLIFixture) run(stdin string, args ...string) (int, string, string) {
	f.t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Execute(context.Background(), Dependencies{
		Stdin:       strings.NewReader(stdin),
		Stdout:      &stdout,
		Stderr:      &stderr,
		Environment: f.environment,
		Processes:   process.ExecRunner{},
	}, args)
	return code, stdout.String(), stderr.String()
}

func (f *mvpCLIFixture) success(stdin string, args ...string) string {
	f.t.Helper()
	code, stdout, stderr := f.run(stdin, args...)
	if code != ExitSuccess || stderr != "" {
		f.t.Fatalf("taskctl %v = %d, stdout = %q, stderr = %q", args, code, stdout, stderr)
	}
	return stdout
}

func (f *mvpCLIFixture) setBranch(branch string) {
	f.t.Helper()
	runCLIGit(f.t, "-C", f.repository, "symbolic-ref", "HEAD", "refs/heads/"+branch)
}

func (f *mvpCLIFixture) initializeAndCreateTask() {
	f.t.Helper()
	f.success("", "init", "--vault", f.vault, "--viewer", "typora", "--non-interactive")
	f.success("", "new", "Release the MVP", "--prefix", "FLOW", "--non-interactive")
}

func TestFullCLIWorkflowAgainstRealGitRepository(t *testing.T) {
	t.Parallel()
	fixture := newMVPCLIFixture(t)
	fixture.initializeAndCreateTask()

	researchPath := strings.TrimSpace(fixture.success("", "artifact", "ensure", "research"))
	planPath := strings.TrimSpace(fixture.success("", "artifact", "ensure", "plan"))
	reviewPath := strings.TrimSpace(fixture.success("", "artifact", "ensure", "review"))
	taskDirectory := filepath.Dir(planPath)
	taskPath := filepath.Join(taskDirectory, "task.md")
	for _, path := range []string{taskPath, researchPath, planPath, reviewPath} {
		if !filepath.IsAbs(path) {
			t.Fatalf("artifact path = %q, want absolute", path)
		}
	}

	planMarkdown := strings.Replace(
		string(mustReadCLIFile(t, planPath)),
		"Use `### PR-NNN: Title` and `#### STEP-NNN: Title` headings for registered work.",
		"### PR-001: Deliver lifecycle\n\n#### STEP-001: Implement workflow\n\n#### STEP-002: Remove obsolete path\n\n"+
			"### PR-002: Provider integration\n\n#### STEP-003: Add hosted provider\n",
		1,
	)
	if err := os.WriteFile(planPath, []byte(planMarkdown), 0o644); err != nil {
		t.Fatal(err)
	}
	structuredPlan := `{"prs":[{"id":"PR-001","title":"Deliver lifecycle","steps":[{"id":"STEP-001","title":"Implement workflow"},{"id":"STEP-002","title":"Remove obsolete path"}]},{"id":"PR-002","title":"Provider integration","steps":[{"id":"STEP-003","title":"Add hosted provider"}]}]}`
	if output := fixture.success(structuredPlan, "plan", "apply"); !strings.Contains(output, "2 PRs, 3 Steps") {
		t.Fatalf("plan apply output = %q", output)
	}
	if output := fixture.success("", "pr", "start", "PR-001"); output != "Started PR: PR-001 on feature/delivery\n" {
		t.Fatalf("pr start output = %q", output)
	}

	wantStepJSON := "{\n" +
		"  \"task_id\": \"FLOW-001\",\n" +
		"  \"pr_id\": \"PR-001\",\n" +
		"  \"step_id\": \"STEP-001\",\n" +
		"  \"status\": \"pending\",\n" +
		"  \"artifacts\": {\n" +
		"    \"task\": " + jsonString(taskPath) + ",\n" +
		"    \"research\": " + jsonString(researchPath) + ",\n" +
		"    \"plan\": " + jsonString(planPath) + ",\n" +
		"    \"review\": " + jsonString(reviewPath) + "\n" +
		"  }\n" +
		"}\n"
	if output := fixture.success("", "step", "get"); output != wantStepJSON {
		t.Fatalf("initial step get = %q, want %q", output, wantStepJSON)
	}

	for _, command := range [][]string{
		{"step", "start"},
		{"step", "submit"},
		{"step", "revise"},
		{"step", "submit"},
		{"step", "complete"},
		{"step", "skip", "--reason", "not required"},
		{"pr", "skip", "PR-002", "--reason", "deferred beyond MVP"},
	} {
		fixture.success("", command...)
	}
	completed := decodeCLIContext(t, fixture.success("", "context"))
	if completed.TaskID != "FLOW-001" || completed.Status != "completed" || completed.CurrentPR == nil || completed.CurrentPR.Status != "completed" {
		t.Fatalf("completed context = %#v", completed)
	}
	code, stdout, stderr := fixture.run("", "step", "get")
	if code != ExitNotFound || stdout != "" || !strings.Contains(stderr, "no active or pending Step") {
		t.Fatalf("step get after completion = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}

	if got := strings.TrimSpace(fixture.success("", "step", "add", "--pr", "PR-001", "--title", "Address final review")); got != "STEP-004" {
		t.Fatalf("corrective Step ID = %q", got)
	}
	appendCLIFile(t, planPath, "\n#### STEP-004: Address final review\n")
	reopened := decodeCLIContext(t, fixture.success("", "context"))
	if reopened.Status != "in_progress" || reopened.CurrentPR == nil || reopened.CurrentPR.Status != "in_progress" {
		t.Fatalf("reopened context = %#v", reopened)
	}
	for _, command := range [][]string{{"step", "start"}, {"step", "submit"}, {"step", "complete"}} {
		fixture.success("", command...)
	}
	recompleted := decodeCLIContext(t, fixture.success("", "context"))
	if recompleted.Status != "completed" || recompleted.CurrentPR == nil || recompleted.CurrentPR.Status != "completed" {
		t.Fatalf("recompleted context = %#v", recompleted)
	}

	fixture.success("", "new", "Follow-up Task", "--non-interactive")
	if output := fixture.success("", "task", "list"); !strings.Contains(output, "FLOW-001") || !strings.Contains(output, "* FLOW-002") {
		t.Fatalf("task list = %q", output)
	}
	branchContext := decodeCLIContext(t, fixture.success("", "context"))
	if branchContext.TaskID != "FLOW-001" || branchContext.CurrentPR == nil {
		t.Fatalf("branch-precedence context = %#v", branchContext)
	}

	fixture.setBranch("main")
	fallbackContext := decodeCLIContext(t, fixture.success("", "context"))
	if fallbackContext.TaskID != "FLOW-002" || fallbackContext.CurrentPR != nil {
		t.Fatalf("fallback context = %#v", fallbackContext)
	}
	fixture.success("", "task", "cancel")
	fixture.success("", "use", "FLOW-001")
	selectedContext := decodeCLIContext(t, fixture.success("", "context"))
	if selectedContext.TaskID != "FLOW-001" || selectedContext.CurrentPR != nil {
		t.Fatalf("selected fallback context = %#v", selectedContext)
	}
	if output := fixture.success("", "status"); !strings.Contains(output, "Status: Completed") || !strings.HasSuffix(output, "Vault: not a Git repository\n") {
		t.Fatalf("human status = %q", output)
	}

	projected := string(mustReadCLIFile(t, planPath))
	for _, expected := range []string{
		"STEP-002: Remove obsolete path — Skipped",
		"PR-002: Provider integration — Skipped",
		"STEP-004: Address final review — Completed",
	} {
		if !strings.Contains(projected, expected) {
			t.Fatalf("final plan does not contain %q: %q", expected, projected)
		}
	}
	for _, path := range []string{filepath.Join(fixture.repository, ".agent-task"), filepath.Join(fixture.repository, ".git", ".agent-task")} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("repository-local pointer exists at %s: %v", path, err)
		}
	}
}

func TestCLIRejectsCorruptStateWithoutRewritingIt(t *testing.T) {
	t.Parallel()
	fixture := newMVPCLIFixture(t)
	fixture.initializeAndCreateTask()

	manifestPath := filepath.Join(fixture.vault, "taskctl.yaml")
	projectPath := filepath.Join(fixture.vault, "projects", "acme_taskctl", "project.yaml")
	taskPath := filepath.Join(fixture.vault, "projects", "acme_taskctl", "FLOW-001", "task.yaml")
	originalManifest := mustReadCLIFile(t, manifestPath)
	originalProject := mustReadCLIFile(t, projectPath)
	originalTask := mustReadCLIFile(t, taskPath)

	assertRejectedUnchanged := func(path string, corrupt []byte, args ...string) {
		t.Helper()
		if err := os.WriteFile(path, corrupt, 0o644); err != nil {
			t.Fatal(err)
		}
		code, stdout, stderr := fixture.run("", args...)
		if code != ExitInvalidData || stdout != "" || stderr == "" {
			t.Fatalf("taskctl %v = %d, stdout = %q, stderr = %q", args, code, stdout, stderr)
		}
		if got := mustReadCLIFile(t, path); !bytes.Equal(got, corrupt) {
			t.Fatalf("taskctl %v rewrote corrupt %s: got %q, want %q", args, path, got, corrupt)
		}
	}

	assertRejectedUnchanged(manifestPath, []byte("schema_version: 1\nunknown: true\n"), "context")
	if err := os.WriteFile(manifestPath, originalManifest, 0o644); err != nil {
		t.Fatal(err)
	}
	assertRejectedUnchanged(manifestPath, []byte("schema_version: 99\n"), "context")
	if err := os.WriteFile(manifestPath, originalManifest, 0o644); err != nil {
		t.Fatal(err)
	}
	assertRejectedUnchanged(projectPath, append(append([]byte(nil), originalProject...), []byte("unknown: true\n")...), "task", "list")
	if err := os.WriteFile(projectPath, originalProject, 0o644); err != nil {
		t.Fatal(err)
	}
	assertRejectedUnchanged(taskPath, []byte("schema_version: [\n"), "context")
	if err := os.WriteFile(taskPath, originalTask, 0o644); err != nil {
		t.Fatal(err)
	}

	staleProject := bytes.Replace(originalProject, []byte("current_task: FLOW-001"), []byte("current_task: FLOW-999"), 1)
	assertRejectedUnchanged(projectPath, staleProject, "context")
	if err := os.WriteFile(projectPath, originalProject, 0o644); err != nil {
		t.Fatal(err)
	}

	planPath := strings.TrimSpace(fixture.success("", "artifact", "ensure", "plan"))
	planMarkdown := strings.Replace(
		string(mustReadCLIFile(t, planPath)),
		"Use `### PR-NNN: Title` and `#### STEP-NNN: Title` headings for registered work.",
		"### PR-001: Delivery\n\n#### STEP-001: Build\n",
		1,
	)
	if err := os.WriteFile(planPath, []byte(planMarkdown), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := fixture.run("{", "plan", "apply")
	if code != ExitUsage || stdout != "" || !strings.Contains(stderr, "invalid structured plan") {
		t.Fatalf("malformed plan JSON = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if got := string(mustReadCLIFile(t, planPath)); got != planMarkdown {
		t.Fatalf("malformed JSON rewrote plan.md: %q", got)
	}

	brokenPlan := strings.Replace(planMarkdown, "<!-- taskctl:progress:end -->", "", 1)
	if err := os.WriteFile(planPath, []byte(brokenPlan), 0o644); err != nil {
		t.Fatal(err)
	}
	beforeTask := mustReadCLIFile(t, taskPath)
	structured := `{"prs":[{"id":"PR-001","title":"Delivery","steps":[{"id":"STEP-001","title":"Build"}]}]}`
	code, stdout, stderr = fixture.run(structured, "plan", "apply")
	if code != ExitInvalidData || stdout != "" || !strings.Contains(stderr, "one start and one end marker") {
		t.Fatalf("invalid progress markers = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if got := mustReadCLIFile(t, taskPath); !bytes.Equal(got, beforeTask) {
		t.Fatalf("invalid markers rewrote task.yaml: got %q, want %q", got, beforeTask)
	}
	if got := string(mustReadCLIFile(t, planPath)); got != brokenPlan {
		t.Fatalf("invalid markers rewrote plan.md: %q", got)
	}
}

type cliContextFixture struct {
	TaskID    string `json:"task_id"`
	Status    string `json:"status"`
	CurrentPR *struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"current_pr"`
}

func decodeCLIContext(t *testing.T, output string) cliContextFixture {
	t.Helper()
	var result cliContextFixture
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("decode context JSON %q: %v", output, err)
	}
	return result
}

func jsonString(value string) string {
	return strconv.Quote(value)
}

func mustReadCLIFile(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return contents
}

func appendCLIFile(t *testing.T, path, contents string) {
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

func runCLIGit(t *testing.T, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
