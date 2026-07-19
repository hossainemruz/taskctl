package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type fakeEnvironment struct {
	goos    string
	home    string
	working string
	values  map[string]string
}

func (e fakeEnvironment) GOOS() string { return e.goos }
func (e fakeEnvironment) LookupEnv(key string) (string, bool) {
	value, ok := e.values[key]
	return value, ok
}
func (e fakeEnvironment) UserHomeDir() (string, error)      { return e.home, nil }
func (e fakeEnvironment) WorkingDirectory() (string, error) { return e.working, nil }

func TestPathResolver(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		environment fakeEnvironment
		want        string
	}{
		{
			name:        "linux xdg",
			environment: fakeEnvironment{goos: "linux", home: "/home/user", values: map[string]string{"XDG_CONFIG_HOME": "/var/config"}},
			want:        "/var/config/taskctl/config.yaml",
		},
		{
			name:        "linux home fallback",
			environment: fakeEnvironment{goos: "linux", home: "/home/user"},
			want:        "/home/user/.config/taskctl/config.yaml",
		},
		{
			name:        "macOS application support",
			environment: fakeEnvironment{goos: "darwin", home: "/Users/user", values: map[string]string{"XDG_CONFIG_HOME": "/ignored"}},
			want:        "/Users/user/Library/Application Support/taskctl/config.yaml",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := (PathResolver{Environment: test.environment}).Path()
			if err != nil {
				t.Fatalf("Path() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("Path() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPathResolverRejectsRelativeConfigDirectory(t *testing.T) {
	t.Parallel()
	environment := fakeEnvironment{
		goos:   "linux",
		home:   "/home/user",
		values: map[string]string{"XDG_CONFIG_HOME": "relative"},
	}
	if _, err := (PathResolver{Environment: environment}).Path(); err == nil {
		t.Fatal("Path() error = nil, want relative-path error")
	}
}

func TestStoreRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	store := NewStore(path)
	want := Config{
		SchemaVersion: SchemaVersion,
		Vault:         filepath.Join(t.TempDir(), "vault"),
		Viewer: Viewer{
			Command: "open",
			Args:    []string{"-a", "Markdown Viewer"},
		},
	}

	if err := store.Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if gotMode := info.Mode().Perm(); gotMode != 0o600 {
		t.Fatalf("config mode = %#o, want 0600", gotMode)
	}
}

func TestStoreStrictDecoding(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		contents string
		wantIs   error
	}{
		{
			name:     "unknown field",
			contents: "schema_version: 1\nvault: /tmp/vault\nviewer:\n  command: typora\n  args: []\nextra: true\n",
			wantIs:   ErrInvalid,
		},
		{
			name:     "unsupported version",
			contents: "schema_version: 2\nvault: /tmp/vault\nviewer:\n  command: typora\n  args: []\n",
			wantIs:   ErrUnsupportedVersion,
		},
		{
			name:     "malformed",
			contents: "schema_version: [\n",
			wantIs:   ErrInvalid,
		},
		{
			name:     "multiple documents",
			contents: "schema_version: 1\nvault: /tmp/vault\nviewer:\n  command: typora\n  args: []\n---\nother: document\n",
			wantIs:   ErrInvalid,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(test.contents), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := NewStore(path).Load()
			if !errors.Is(err, test.wantIs) {
				t.Fatalf("Load() error = %v, want errors.Is(_, %v)", err, test.wantIs)
			}
		})
	}
}

func TestStoreMissing(t *testing.T) {
	t.Parallel()
	_, err := NewStore(filepath.Join(t.TempDir(), "missing.yaml")).Load()
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load() error = %v, want ErrNotFound", err)
	}
}
