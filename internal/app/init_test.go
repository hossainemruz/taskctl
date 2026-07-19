package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hossainemruz/taskctl/internal/config"
)

type initEnvironment struct {
	home    string
	working string
	xdg     string
}

func TestInitializerValidatesAllInputBeforeCreatingVault(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	environment := initEnvironment{
		home:    filepath.Join(root, "home"),
		working: root,
		xdg:     filepath.Join(root, "config"),
	}
	vaultPath := filepath.Join(root, "vault")
	_, err := NewInitializer(environment).Init(t.Context(), InitInput{
		Vault: vaultPath,
		Viewer: config.Viewer{
			Command: "typora",
			Args:    []string{"bad\x00argument"},
		},
	})
	if kind, ok := ErrorKindOf(err); !ok || kind != ErrorUsage {
		t.Fatalf("Init() error = %v, kind = %v, categorized = %v", err, kind, ok)
	}
	if _, statErr := os.Stat(vaultPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("vault was created before input validation: %v", statErr)
	}
}

func (e initEnvironment) GOOS() string { return "linux" }
func (e initEnvironment) LookupEnv(key string) (string, bool) {
	if key == "XDG_CONFIG_HOME" {
		return e.xdg, true
	}
	return "", false
}
func (e initEnvironment) UserHomeDir() (string, error)      { return e.home, nil }
func (e initEnvironment) WorkingDirectory() (string, error) { return e.working, nil }

func TestNormalizeVaultPath(t *testing.T) {
	t.Parallel()
	environment := initEnvironment{home: "/home/user", working: "/work/project"}
	tests := []struct {
		input string
		want  string
	}{
		{input: "vault", want: "/work/project/vault"},
		{input: "~/vault", want: "/home/user/vault"},
		{input: "/shared/../vault", want: "/vault"},
	}
	for _, test := range tests {
		got, err := normalizeVaultPath(test.input, environment)
		if err != nil {
			t.Fatalf("normalizeVaultPath(%q) error = %v", test.input, err)
		}
		if got != test.want {
			t.Fatalf("normalizeVaultPath(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestInitializerResolvesRelativeVaultAndSavesConfig(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	environment := initEnvironment{
		home:    filepath.Join(root, "home"),
		working: root,
		xdg:     filepath.Join(root, "config"),
	}
	initializer := NewInitializer(environment)
	result, err := initializer.Init(t.Context(), InitInput{
		Vault: "vault",
		Viewer: config.Viewer{
			Command: "typora",
			Args:    []string{},
		},
	})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	wantVault := filepath.Join(root, "vault")
	if result.Vault != wantVault {
		t.Fatalf("Init() vault = %q, want %q", result.Vault, wantVault)
	}
	local, err := config.NewStore(filepath.Join(environment.xdg, config.DirectoryName, config.FileName)).Load()
	if err != nil {
		t.Fatal(err)
	}
	if local.Vault != wantVault {
		t.Fatalf("saved vault = %q, want %q", local.Vault, wantVault)
	}
}
