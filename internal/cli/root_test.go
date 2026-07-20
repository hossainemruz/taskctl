package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hossainemruz/taskctl/internal/app"
	"github.com/hossainemruz/taskctl/internal/config"
	"github.com/hossainemruz/taskctl/internal/process"
)

type stubInitializer struct {
	defaults    app.InitDefaults
	defaultsErr error
	result      app.InitResult
	initErr     error
	input       app.InitInput
}

func (s *stubInitializer) Defaults(context.Context) (app.InitDefaults, error) {
	return s.defaults, s.defaultsErr
}

func (s *stubInitializer) Init(_ context.Context, input app.InitInput) (app.InitResult, error) {
	s.input = input
	return s.result, s.initErr
}

type cliEnvironment struct {
	home    string
	working string
	xdg     string
}

func (e cliEnvironment) GOOS() string { return "linux" }
func (e cliEnvironment) LookupEnv(key string) (string, bool) {
	if key == "XDG_CONFIG_HOME" && e.xdg != "" {
		return e.xdg, true
	}
	return "", false
}
func (e cliEnvironment) UserHomeDir() (string, error)      { return e.home, nil }
func (e cliEnvironment) WorkingDirectory() (string, error) { return e.working, nil }

type panicRunner struct{}

func (panicRunner) Run(context.Context, process.Command) (process.Result, error) {
	panic("init invoked an external process")
}
func (panicRunner) Start(process.Command) error {
	panic("init invoked an external process")
}

type workflowRunner struct {
	commands      []process.Command
	started       []process.Command
	branch        string
	vaultDirty    string
	vaultCounts   string
	fetchErr      error
	noUpstream    bool
	notRepository bool
}

func (r *workflowRunner) Run(_ context.Context, command process.Command) (process.Result, error) {
	r.commands = append(r.commands, command)
	if len(command.Args) == 0 {
		return process.Result{}, nil
	}
	switch command.Args[0] {
	case "config":
		return process.Result{Stdout: []byte("git@github.com:org/taskctl.git\n")}, nil
	case "symbolic-ref":
		branch := r.branch
		if branch == "" {
			branch = "main\n"
		}
		return process.Result{Stdout: []byte(branch)}, nil
	case "fetch":
		return process.Result{}, r.fetchErr
	case "status":
		return process.Result{Stdout: []byte(r.vaultDirty)}, nil
	case "rev-list":
		counts := r.vaultCounts
		if counts == "" {
			counts = "0\t0\n"
		}
		return process.Result{Stdout: []byte(counts)}, nil
	case "rev-parse":
		if len(command.Args) > 1 && command.Args[1] == "--is-inside-work-tree" {
			if r.notRepository {
				return process.Result{}, testGitExit(128)
			}
			return process.Result{Stdout: []byte("true\n")}, nil
		}
		if r.noUpstream {
			return process.Result{}, testGitExit(1)
		}
		return process.Result{Stdout: []byte("abc123\n")}, nil
	}
	return process.Result{}, nil
}

func (r *workflowRunner) Start(command process.Command) error {
	r.started = append(r.started, command)
	return nil
}

func testGitExit(code int) error {
	return &process.CommandError{Name: "git", ExitCode: code, Cause: fmt.Errorf("exit status %d", code)}
}

func TestHelpAndVersion(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		args       []string
		wantOutput string
	}{
		{name: "help", args: []string{"--help"}, wantOutput: "init        Configure this machine"},
		{name: "version flag", args: []string{"--version"}, wantOutput: "taskctl test-version\n"},
		{name: "version command", args: []string{"version"}, wantOutput: "taskctl test-version\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := Execute(context.Background(), Dependencies{
				Stdout:      &stdout,
				Stderr:      &stderr,
				Initializer: &stubInitializer{},
				Version:     "test-version",
			}, test.args)
			if code != ExitSuccess {
				t.Fatalf("Execute() = %d, stderr = %q", code, stderr.String())
			}
			if !strings.Contains(stdout.String(), test.wantOutput) {
				t.Fatalf("stdout = %q, want it to contain %q", stdout.String(), test.wantOutput)
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}

func TestOperationalErrorDoesNotPrintUsage(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	service := &stubInitializer{initErr: app.NewError(app.ErrorInvalidData, "incompatible vault")}
	code := Execute(context.Background(), Dependencies{
		Stdout:      &stdout,
		Stderr:      &stderr,
		Initializer: service,
	}, []string{"init", "--vault", "/tmp/vault", "--viewer", "typora", "--non-interactive"})
	if code != ExitInvalidData {
		t.Fatalf("Execute() = %d, want %d", code, ExitInvalidData)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); got != "Error: incompatible vault\n" {
		t.Fatalf("stderr = %q", got)
	}
	if strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("operational error printed usage: %q", stderr.String())
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	code := Execute(context.Background(), Dependencies{
		Stderr:      &stderr,
		Initializer: &stubInitializer{},
	}, []string{"unknown"})
	if code != ExitUsage {
		t.Fatalf("Execute() = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestInitInteractiveFallbacks(t *testing.T) {
	t.Parallel()
	service := &stubInitializer{result: app.InitResult{
		Vault:              "/tmp/vault",
		ConfigPath:         "/tmp/config.yaml",
		VaultCreated:       true,
		TemplatesInstalled: []string{"one", "two"},
	}}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Execute(context.Background(), Dependencies{
		Stdin:       strings.NewReader("/tmp/vault\ncode\n"),
		Stdout:      &stdout,
		Stderr:      &stderr,
		Initializer: service,
	}, []string{"init"})
	if code != ExitSuccess {
		t.Fatalf("Execute() = %d, stderr = %q", code, stderr.String())
	}
	if service.input.Vault != "/tmp/vault" || service.input.Viewer.Command != "code" {
		t.Fatalf("Init input = %#v", service.input)
	}
	if got := stderr.String(); got != "Vault path: Viewer executable [typora]: " {
		t.Fatalf("prompts = %q", got)
	}
	if !strings.Contains(stdout.String(), "Templates installed: 2") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestInitNonInteractiveRequiresValues(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	code := Execute(context.Background(), Dependencies{
		Stderr:      &stderr,
		Initializer: &stubInitializer{},
	}, []string{"init", "--non-interactive"})
	if code != ExitUsage {
		t.Fatalf("Execute() = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr.String(), "--vault is required") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestInitPassesForceOption(t *testing.T) {
	t.Parallel()
	service := &stubInitializer{result: app.InitResult{
		Vault:      "/tmp/vault",
		ConfigPath: "/tmp/config.yaml",
	}}
	var stderr bytes.Buffer
	code := Execute(context.Background(), Dependencies{
		Stderr:      &stderr,
		Initializer: service,
	}, []string{"init", "--vault", "/tmp/vault", "--viewer", "typora", "--non-interactive", "--force"})
	if code != ExitSuccess {
		t.Fatalf("Execute() = %d, stderr = %q", code, stderr.String())
	}
	if !service.input.Force {
		t.Fatalf("Init input = %#v, want Force true", service.input)
	}
}

func TestInitEndToEndAndIdempotent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	environment := cliEnvironment{
		home:    filepath.Join(root, "home"),
		working: root,
		xdg:     filepath.Join(root, "config"),
	}
	vaultPath := filepath.Join(root, "vault")

	run := func() (int, string, string) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		code := Execute(context.Background(), Dependencies{
			Stdout:      &stdout,
			Stderr:      &stderr,
			Environment: environment,
			Processes:   panicRunner{},
		}, []string{"init", "--vault", vaultPath, "--viewer", "open", "--viewer-arg=-a", "--viewer-arg=Typora", "--non-interactive"})
		return code, stdout.String(), stderr.String()
	}

	code, stdout, stderr := run()
	if code != ExitSuccess {
		t.Fatalf("first init = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "Vault created: "+vaultPath) || !strings.Contains(stdout, "Templates installed: 4") {
		t.Fatalf("first stdout = %q", stdout)
	}

	configPath := filepath.Join(environment.xdg, config.DirectoryName, config.FileName)
	local, err := config.NewStore(configPath).Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if local.Vault != vaultPath || local.Viewer.Command != "open" || strings.Join(local.Viewer.Args, ",") != "-a,Typora" {
		t.Fatalf("local config = %#v", local)
	}

	customPath := filepath.Join(vaultPath, "templates", "task.md.tmpl")
	const custom = "# Customized\n"
	if err := os.WriteFile(customPath, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = run()
	if code != ExitSuccess {
		t.Fatalf("second init = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "Vault ready: "+vaultPath) || !strings.Contains(stdout, "Templates installed: 0") {
		t.Fatalf("second stdout = %q", stdout)
	}
	got, err := os.ReadFile(customPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != custom {
		t.Fatalf("customized template = %q, want %q", got, custom)
	}
}

func TestInitRejectsInvalidExistingLocalConfig(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	environment := cliEnvironment{home: root, working: root, xdg: filepath.Join(root, "config")}
	configPath := filepath.Join(environment.xdg, config.DirectoryName, config.FileName)
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("schema_version: 99\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	code := Execute(context.Background(), Dependencies{
		Stderr:      &stderr,
		Environment: environment,
	}, []string{"init", "--vault", filepath.Join(root, "vault"), "--viewer", "typora", "--non-interactive"})
	if code != ExitInvalidData {
		t.Fatalf("Execute() = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "unsupported local configuration schema version") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, "vault")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("vault was touched before invalid config was reported: %v", err)
	}
}

func TestTaskArtifactCLIWorkflow(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	repository := filepath.Join(root, "repo with spaces")
	if err := os.Mkdir(repository, 0o755); err != nil {
		t.Fatal(err)
	}
	environment := cliEnvironment{home: filepath.Join(root, "home"), working: repository, xdg: filepath.Join(root, "config")}
	vaultPath := filepath.Join(root, "vault with spaces")
	runner := &workflowRunner{}
	run := func(args ...string) (int, string, string) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		code := Execute(context.Background(), Dependencies{Stdout: &stdout, Stderr: &stderr, Environment: environment, Processes: runner}, args)
		return code, stdout.String(), stderr.String()
	}
	assertSuccess := func(args ...string) string {
		t.Helper()
		code, stdout, stderr := run(args...)
		if code != ExitSuccess || stderr != "" {
			t.Fatalf("taskctl %v = %d, stdout = %q, stderr = %q", args, code, stdout, stderr)
		}
		return stdout
	}

	assertSuccess("init", "--vault", vaultPath, "--viewer", "open", "--viewer-arg=-a", "--viewer-arg=Typora", "--non-interactive")
	stdout := assertSuccess("new", "First task", "--prefix", "TASKCTL", "--non-interactive")
	if !strings.Contains(stdout, "Created TASKCTL-001") {
		t.Fatalf("new stdout = %q", stdout)
	}
	researchPath := strings.TrimSpace(assertSuccess("artifact", "ensure", "research"))
	if !filepath.IsAbs(researchPath) {
		t.Fatalf("ensure path = %q", researchPath)
	}
	const customized = "# Keep me\n"
	if err := os.WriteFile(researchPath, []byte(customized), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(assertSuccess("artifact", "ensure", "research")); got != researchPath {
		t.Fatalf("second ensure path = %q, want %q", got, researchPath)
	}
	if contents, _ := os.ReadFile(researchPath); string(contents) != customized {
		t.Fatalf("ensure overwrote customized artifact = %q", contents)
	}
	if got := strings.TrimSpace(assertSuccess("path", "research")); got != researchPath {
		t.Fatalf("path stdout = %q, want %q", got, researchPath)
	}
	assertSuccess("new", "Second task", "--non-interactive")
	if list := assertSuccess("task", "list"); !strings.Contains(list, "TASKCTL-001") || !strings.Contains(list, "* TASKCTL-002") {
		t.Fatalf("task list = %q", list)
	}
	assertSuccess("use", "TASKCTL-001")
	assertSuccess("task", "cancel")
	assertSuccess("artifact", "view")
	if len(runner.started) != 1 {
		t.Fatalf("viewer starts = %#v", runner.started)
	}
	viewer := runner.started[0]
	wantDirectory := filepath.Dir(researchPath)
	if viewer.Name != "open" || strings.Join(viewer.Args, "|") != "-a|Typora|"+wantDirectory {
		t.Fatalf("viewer command = %#v", viewer)
	}
	if _, err := os.Stat(filepath.Join(repository, ".agent-task")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("repository-local pointer exists: %v", err)
	}
}

func TestPlanningCLIWorkflow(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	repository := filepath.Join(root, "repo")
	if err := os.Mkdir(repository, 0o755); err != nil {
		t.Fatal(err)
	}
	environment := cliEnvironment{home: filepath.Join(root, "home"), working: repository, xdg: filepath.Join(root, "config")}
	vaultPath := filepath.Join(root, "vault")
	runner := &workflowRunner{}
	run := func(stdin string, args ...string) (int, string, string) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		code := Execute(context.Background(), Dependencies{Stdin: strings.NewReader(stdin), Stdout: &stdout, Stderr: &stderr,
			Environment: environment, Processes: runner}, args)
		return code, stdout.String(), stderr.String()
	}
	assertSuccess := func(stdin string, args ...string) string {
		t.Helper()
		code, stdout, stderr := run(stdin, args...)
		if code != ExitSuccess || stderr != "" {
			t.Fatalf("taskctl %v = %d, stdout = %q, stderr = %q", args, code, stdout, stderr)
		}
		return stdout
	}

	assertSuccess("", "init", "--vault", vaultPath, "--viewer", "typora", "--non-interactive")
	assertSuccess("", "new", "Plan task", "--prefix", "PLAN", "--non-interactive")
	planPath := strings.TrimSpace(assertSuccess("", "artifact", "ensure", "plan"))
	contents, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	contents = []byte(strings.Replace(string(contents), "Use `### PR-NNN: Title`", "### PR-001: Storage\n\n#### STEP-001: Schema\n\nUse `### PR-NNN: Title`", 1))
	if err := os.WriteFile(planPath, contents, 0o644); err != nil {
		t.Fatal(err)
	}
	input := `{"prs":[{"id":"PR-001","title":"Storage","steps":[{"id":"STEP-001","title":"Schema"}]}]}`
	inputPath := filepath.Join(root, "plan.json")
	if err := os.WriteFile(inputPath, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}
	if stdout := assertSuccess("", "plan", "apply", "--file", inputPath); !strings.Contains(stdout, "1 PRs, 1 Steps") {
		t.Fatalf("plan apply stdout = %q", stdout)
	}
	assertSuccess(input, "plan", "apply")
	if output := assertSuccess("", "pr", "list", "--json"); !strings.Contains(output, `"id": "PR-001"`) || !strings.Contains(output, `"total": 1`) {
		t.Fatalf("pr list JSON = %q", output)
	}
	if output := strings.TrimSpace(assertSuccess("", "step", "list", "--json")); !strings.HasPrefix(output, "[") || !strings.Contains(output, `"pr_id": "PR-001"`) {
		t.Fatalf("step list JSON = %q", output)
	}
	if got := strings.TrimSpace(assertSuccess("", "step", "add", "--pr", "PR-001", "--title", "Persistence")); got != "STEP-002" {
		t.Fatalf("step add stdout = %q", got)
	}
	if got := strings.TrimSpace(assertSuccess("", "pr", "add", "--title", "CLI")); got != "PR-002" {
		t.Fatalf("pr add stdout = %q", got)
	}
	if got := strings.TrimSpace(assertSuccess("", "step", "add", "--pr", "PR-002", "--title", "Commands")); got != "STEP-003" {
		t.Fatalf("step add second PR stdout = %q", got)
	}
	assertSuccess("", "pr", "skip", "PR-002", "--reason", "deferred")
	projected, err := os.ReadFile(planPath)
	if err != nil || !strings.Contains(string(projected), "PR-002: CLI — Skipped") {
		t.Fatalf("projected plan = %q, error = %v", projected, err)
	}

	code, _, stderr := run("", "step", "add", "--pr", "PR-001")
	if code != ExitUsage || !strings.Contains(stderr, "--title is required") {
		t.Fatalf("missing title = %d, stderr = %q", code, stderr)
	}
}

func TestExecutionCLIWorkflowAndStepGetJSON(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	repository := filepath.Join(root, "repo")
	if err := os.Mkdir(repository, 0o755); err != nil {
		t.Fatal(err)
	}
	environment := cliEnvironment{home: filepath.Join(root, "home"), working: repository, xdg: filepath.Join(root, "config")}
	vaultPath := filepath.Join(root, "vault")
	runner := &workflowRunner{}
	run := func(stdin string, args ...string) (int, string, string) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		code := Execute(context.Background(), Dependencies{Stdin: strings.NewReader(stdin), Stdout: &stdout, Stderr: &stderr,
			Environment: environment, Processes: runner}, args)
		return code, stdout.String(), stderr.String()
	}
	assertSuccess := func(stdin string, args ...string) string {
		t.Helper()
		code, stdout, stderr := run(stdin, args...)
		if code != ExitSuccess || stderr != "" {
			t.Fatalf("taskctl %v = %d, stdout = %q, stderr = %q", args, code, stdout, stderr)
		}
		return stdout
	}

	assertSuccess("", "init", "--vault", vaultPath, "--viewer", "typora", "--non-interactive")
	assertSuccess("", "new", "Execution task", "--prefix", "EXEC", "--non-interactive")
	planPath := strings.TrimSpace(assertSuccess("", "artifact", "ensure", "plan"))
	contents, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	contents = []byte(strings.Replace(string(contents), "Use `### PR-NNN: Title`",
		"### PR-001: Implementation\n\n#### STEP-001: Build lifecycle\n\nUse `### PR-NNN: Title`", 1))
	if err := os.WriteFile(planPath, contents, 0o644); err != nil {
		t.Fatal(err)
	}
	structured := `{"prs":[{"id":"PR-001","title":"Implementation","steps":[{"id":"STEP-001","title":"Build lifecycle"}]}]}`
	assertSuccess(structured, "plan", "apply")

	runner.branch = "feature/team/execution\n"
	if output := assertSuccess("", "pr", "start", "PR-001"); output != "Started PR: PR-001 on feature/team/execution\n" {
		t.Fatalf("pr start stdout = %q", output)
	}
	taskPath := filepath.Join(filepath.Dir(planPath), "task.md")
	wantJSON := fmt.Sprintf("{\n  \"task_id\": \"EXEC-001\",\n  \"pr_id\": \"PR-001\",\n  \"step_id\": \"STEP-001\",\n  \"status\": \"pending\",\n  \"artifacts\": {\n    \"task\": %q,\n    \"plan\": %q\n  }\n}\n", taskPath, planPath)
	if output := assertSuccess("", "step", "get"); output != wantJSON {
		t.Fatalf("step get JSON = %q, want %q", output, wantJSON)
	}

	code, stdout, stderr := run("", "step", "complete")
	if code != ExitConflict || stdout != "" || !strings.Contains(stderr, "user acceptance requires ready for review") {
		t.Fatalf("early complete = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	for _, command := range [][]string{
		{"step", "start"},
		{"step", "submit"},
		{"step", "revise"},
		{"step", "submit"},
		{"step", "complete"},
	} {
		assertSuccess("", command...)
	}
	if got := strings.TrimSpace(assertSuccess("", "step", "add", "--pr", "PR-001", "--title", "Address final review")); got != "STEP-002" {
		t.Fatalf("corrective step ID = %q", got)
	}
	file, err := os.OpenFile(planPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("\n#### STEP-002: Address final review\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if output := assertSuccess("", "step", "get"); !strings.Contains(output, `"step_id": "STEP-002"`) || !strings.Contains(output, `"status": "pending"`) {
		t.Fatalf("corrective step get = %q", output)
	}
	for _, command := range [][]string{{"step", "start"}, {"step", "submit"}, {"step", "complete"}} {
		assertSuccess("", command...)
	}
	projected, err := os.ReadFile(planPath)
	if err != nil || !strings.Contains(string(projected), "STEP-002: Address final review — Completed") {
		t.Fatalf("final projection = %q, %v", projected, err)
	}

	code, _, stderr = run("", "step", "reopen")
	if code != ExitConflict || !strings.Contains(stderr, "multiple Steps") {
		t.Fatalf("ambiguous reopen = %d, stderr = %q", code, stderr)
	}
	assertSuccess("", "step", "reopen", "STEP-002")
	assertSuccess("", "step", "skip", "--reason", "superseded")
}

func TestContextTaskStatusAndVaultStatusCLI(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	repository := filepath.Join(root, "repo")
	if err := os.Mkdir(repository, 0o755); err != nil {
		t.Fatal(err)
	}
	environment := cliEnvironment{home: filepath.Join(root, "home"), working: repository, xdg: filepath.Join(root, "config")}
	vaultPath := filepath.Join(root, "vault")
	runner := &workflowRunner{}
	run := func(stdin string, args ...string) (int, string, string) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		code := Execute(context.Background(), Dependencies{Stdin: strings.NewReader(stdin), Stdout: &stdout, Stderr: &stderr,
			Environment: environment, Processes: runner}, args)
		return code, stdout.String(), stderr.String()
	}
	assertSuccess := func(stdin string, args ...string) string {
		t.Helper()
		code, stdout, stderr := run(stdin, args...)
		if code != ExitSuccess || stderr != "" {
			t.Fatalf("taskctl %v = %d, stdout = %q, stderr = %q", args, code, stdout, stderr)
		}
		return stdout
	}

	assertSuccess("", "init", "--vault", vaultPath, "--viewer", "typora", "--non-interactive")
	assertSuccess("", "new", "Status task", "--prefix", "STAT", "--non-interactive")
	researchPath := strings.TrimSpace(assertSuccess("", "artifact", "ensure", "research"))
	planPath := strings.TrimSpace(assertSuccess("", "artifact", "ensure", "plan"))
	contents, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	contents = []byte(strings.Replace(string(contents), "Use `### PR-NNN: Title`",
		"### PR-001: Delivery\n\n#### STEP-001: Implement\n\nUse `### PR-NNN: Title`", 1))
	if err := os.WriteFile(planPath, contents, 0o644); err != nil {
		t.Fatal(err)
	}
	structured := `{"prs":[{"id":"PR-001","title":"Delivery","steps":[{"id":"STEP-001","title":"Implement"}]}]}`
	assertSuccess(structured, "plan", "apply")
	runner.branch = "feature/status\n"
	assertSuccess("", "pr", "start", "PR-001")
	assertSuccess("", "step", "start")
	assertSuccess("", "step", "submit")

	if got := countGitCommand(runner.commands, "fetch"); got != 0 {
		t.Fatalf("commands before status fetched %d times", got)
	}
	taskPath := filepath.Join(filepath.Dir(planPath), "task.md")
	wantContext := fmt.Sprintf("{\n  \"project_id\": \"org_taskctl\",\n  \"task_id\": \"STAT-001\",\n  \"status\": \"in_progress\",\n  \"progress\": {\n    \"completed\": 0,\n    \"skipped\": 0,\n    \"total\": 1\n  },\n  \"current_pr\": {\n    \"id\": \"PR-001\",\n    \"status\": \"in_progress\",\n    \"progress\": {\n      \"completed\": 0,\n      \"skipped\": 0,\n      \"total\": 1\n    },\n    \"active_step\": {\n      \"id\": \"STEP-001\",\n      \"status\": \"ready_for_review\"\n    }\n  },\n  \"artifacts\": {\n    \"task\": %q,\n    \"research\": %q,\n    \"plan\": %q\n  }\n}\n", taskPath, researchPath, planPath)
	if output := assertSuccess("", "context"); output != wantContext {
		t.Fatalf("context JSON = %q, want %q", output, wantContext)
	}
	if got := countGitCommand(runner.commands, "fetch"); got != 0 {
		t.Fatalf("context fetched vault %d times", got)
	}

	runner.branch = "main\n"
	if fallback := assertSuccess("", "context"); strings.Contains(fallback, `"current_pr"`) || !strings.Contains(fallback, `"task_id": "STAT-001"`) {
		t.Fatalf("fallback context = %q", fallback)
	}
	runner.branch = "feature/status\n"
	wantStatus := fmt.Sprintf("Task: STAT-001 — Status task\nProject: org_taskctl\nStatus: In Progress\nProgress: 0/1 done (0 completed, 0 skipped)\nCurrent PR: PR-001\nActive Step: STEP-001\n\nPRs:\n* PR-001: Delivery — In Progress\n  Progress: 0/1 done (0 completed, 0 skipped)\n  Branch: feature/status\n  > STEP-001: Implement — Ready for Review\n\nArtifacts:\n  task: %s\n  research: %s\n  plan: %s\n\nVault: clean · ahead 0 · behind 0\n", taskPath, researchPath, planPath)
	if output := assertSuccess("", "status"); output != wantStatus {
		t.Fatalf("status output = %q, want %q", output, wantStatus)
	}
	if got := countGitCommand(runner.commands, "fetch"); got != 1 {
		t.Fatalf("status fetch count = %d", got)
	}
	if output := assertSuccess("", "vault", "status"); output != "Vault: clean · ahead 0 · behind 0\n" {
		t.Fatalf("vault status = %q", output)
	}
	if got := countGitCommand(runner.commands, "fetch"); got != 2 {
		t.Fatalf("vault status fetch count = %d", got)
	}

	runner.fetchErr = testGitExit(128)
	runner.vaultDirty = " M projects/task.yaml\n?? local.md\n"
	if output := assertSuccess("", "status"); !strings.HasSuffix(output, "Vault: 2 uncommitted files · remote status unavailable\n") {
		t.Fatalf("status with fetch warning = %q", output)
	}
	runner.noUpstream = true
	if output := assertSuccess("", "vault", "status"); output != "Vault: 2 uncommitted files · no upstream\n" {
		t.Fatalf("no-upstream vault status = %q", output)
	}
	runner.noUpstream = false
	runner.notRepository = true
	if output := assertSuccess("", "vault", "status"); output != "Vault: not a Git repository\n" {
		t.Fatalf("non-repository vault status = %q", output)
	}
}

func countGitCommand(commands []process.Command, name string) int {
	count := 0
	for _, command := range commands {
		if len(command.Args) > 0 && command.Args[0] == name {
			count++
		}
	}
	return count
}
