package vault

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hossainemruz/taskctl/internal/templates"
)

func TestInitializeFreshVault(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "vault")
	result, err := Initialize(root)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if !result.Created {
		t.Fatal("Initialize() Created = false, want true")
	}
	if !reflect.DeepEqual(result.TemplatesInstalled, templates.DefaultNames()) {
		t.Fatalf("installed templates = %v, want %v", result.TemplatesInstalled, templates.DefaultNames())
	}

	manifest, err := os.ReadFile(filepath.Join(root, ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	if string(manifest) != "schema_version: 1\n" {
		t.Fatalf("manifest = %q", manifest)
	}
	for _, directory := range []string{TemplatesDirName, ProjectsDirName} {
		info, err := os.Stat(filepath.Join(root, directory))
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", directory)
		}
	}
	for _, name := range templates.DefaultNames() {
		got, err := os.ReadFile(filepath.Join(root, TemplatesDirName, name))
		if err != nil {
			t.Fatal(err)
		}
		want, err := templates.ReadDefault(name)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("installed %s differs from embedded default", name)
		}
		if strings.HasPrefix(string(got), "---") {
			t.Fatalf("%s contains YAML frontmatter", name)
		}
	}
}

func TestInitializePreservesCustomizedTemplate(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "vault")
	if _, err := Initialize(root); err != nil {
		t.Fatal(err)
	}
	customPath := filepath.Join(root, TemplatesDirName, "plan.md.tmpl")
	const custom = "# My custom plan\n"
	if err := os.WriteFile(customPath, []byte(custom), 0o640); err != nil {
		t.Fatal(err)
	}

	result, err := Initialize(root)
	if err != nil {
		t.Fatalf("second Initialize() error = %v", err)
	}
	if result.Created {
		t.Fatal("second Initialize() Created = true, want false")
	}
	if len(result.TemplatesInstalled) != 0 {
		t.Fatalf("second Initialize() installed = %v, want none", result.TemplatesInstalled)
	}
	got, err := os.ReadFile(customPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != custom {
		t.Fatalf("custom template = %q, want %q", got, custom)
	}
}

func TestInitializeEmptyExistingDirectory(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "vault")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := Initialize(root)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if !result.Created {
		t.Fatal("Initialize() Created = false, want true")
	}
}

func TestInitializeRejectsIncompatibleDirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	keep := filepath.Join(root, "keep.txt")
	if err := os.WriteFile(keep, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Initialize(root)
	if !errors.Is(err, ErrIncompatible) {
		t.Fatalf("Initialize() error = %v, want ErrIncompatible", err)
	}
	if _, err := os.Stat(filepath.Join(root, ManifestName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manifest was created in incompatible directory: %v", err)
	}
	got, err := os.ReadFile(keep)
	if err != nil || string(got) != "keep" {
		t.Fatalf("existing file changed: contents=%q error=%v", got, err)
	}
}

func TestInitializeStrictManifestValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		manifest string
		wantIs   error
	}{
		{name: "unknown field", manifest: "schema_version: 1\nother: true\n", wantIs: ErrInvalid},
		{name: "newer schema", manifest: "schema_version: 2\n", wantIs: ErrUnsupportedVersion},
		{name: "multiple documents", manifest: "schema_version: 1\n---\nschema_version: 1\n", wantIs: ErrInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, ManifestName), []byte(test.manifest), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Initialize(root)
			if !errors.Is(err, test.wantIs) {
				t.Fatalf("Initialize() error = %v, want errors.Is(_, %v)", err, test.wantIs)
			}
		})
	}
}

func TestInitializeRejectsFileAtRequiredDirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ManifestName), []byte("schema_version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, TemplatesDirName), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Initialize(root)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Initialize() error = %v, want ErrInvalid", err)
	}
}
