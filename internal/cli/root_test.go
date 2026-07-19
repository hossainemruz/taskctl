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
