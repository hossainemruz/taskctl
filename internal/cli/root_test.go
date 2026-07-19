package cli

import (
	"bytes"
	"context"
	"errors"
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
	commands []process.Command
	started  []process.Command
}

func (r *workflowRunner) Run(_ context.Context, command process.Command) (process.Result, error) {
	r.commands = append(r.commands, command)
	if len(command.Args) > 0 && command.Args[0] == "config" {
		return process.Result{Stdout: []byte("git@github.com:org/taskctl.git\n")}, nil
	}
	return process.Result{Stdout: []byte("main\n")}, nil
}

func (r *workflowRunner) Start(command process.Command) error {
	r.started = append(r.started, command)
	return nil
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
