package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hossainemruz/taskctl/internal/fsutil"
	"github.com/hossainemruz/taskctl/internal/templates"
	"go.yaml.in/yaml/v3"
)

const (
	SchemaVersion    = 1
	ManifestName     = "taskctl.yaml"
	TemplatesDirName = "templates"
	ProjectsDirName  = "projects"
)

var (
	ErrInvalid            = errors.New("invalid vault")
	ErrIncompatible       = errors.New("incompatible existing vault")
	ErrUnsupportedVersion = errors.New("unsupported vault schema version")
)

// Manifest is the synchronized root marker and version boundary for a vault.
type Manifest struct {
	SchemaVersion int `yaml:"schema_version"`
}

// InitResult reports whether the vault marker and templates were added.
type InitResult struct {
	Created            bool
	TemplatesInstalled []string
}

// InitializeOptions controls how a vault root is adopted.
type InitializeOptions struct {
	Force bool
}

// Initialize creates a dedicated vault or validates and repairs the layout of a
// compatible existing one. Existing template files are never overwritten.
func Initialize(root string) (InitResult, error) {
	return InitializeWithOptions(root, InitializeOptions{})
}

// InitializeWithOptions initializes a vault with explicit adoption behavior.
// Force permits adding the vault manifest to a non-empty directory, but does not
// bypass validation of an existing manifest or required vault paths.
func InitializeWithOptions(root string, options InitializeOptions) (InitResult, error) {
	if root == "" || !filepath.IsAbs(root) {
		return InitResult{}, fmt.Errorf("%w: root must be an absolute path", ErrInvalid)
	}
	root = filepath.Clean(root)

	created, err := prepareRoot(root, options.Force)
	if err != nil {
		return InitResult{}, err
	}
	if err := ensureDirectory(filepath.Join(root, TemplatesDirName), "templates"); err != nil {
		return InitResult{}, err
	}
	if err := ensureDirectory(filepath.Join(root, ProjectsDirName), "projects"); err != nil {
		return InitResult{}, err
	}

	installed, err := installTemplates(root)
	if err != nil {
		return InitResult{}, err
	}
	return InitResult{Created: created, TemplatesInstalled: installed}, nil
}

func ensureDirectory(path, label string) error {
	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("%w: %s path %q is not a directory", ErrInvalid, label, path)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect %s directory: %w", label, err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("create %s directory: %w", label, err)
	}
	return nil
}

func prepareRoot(root string, force bool) (bool, error) {
	info, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(root, 0o755); err != nil {
			return false, fmt.Errorf("create vault directory: %w", err)
		}
		return true, writeManifest(root)
	}
	if err != nil {
		return false, fmt.Errorf("inspect vault directory: %w", err)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("%w: %q is not a directory", ErrInvalid, root)
	}

	manifestPath := filepath.Join(root, ManifestName)
	if _, err := os.Stat(manifestPath); errors.Is(err, os.ErrNotExist) {
		entries, readErr := os.ReadDir(root)
		if readErr != nil {
			return false, fmt.Errorf("inspect existing vault: %w", readErr)
		}
		if len(entries) != 0 && !force {
			return false, fmt.Errorf(
				"%w: %q is non-empty and has no %s; rerun init with --force to adopt it",
				ErrIncompatible, root, ManifestName,
			)
		}
		return true, writeManifest(root)
	} else if err != nil {
		return false, fmt.Errorf("inspect vault manifest: %w", err)
	}

	if err := validateManifest(manifestPath); err != nil {
		return false, err
	}
	return false, nil
}

func writeManifest(root string) error {
	contents, err := yaml.Marshal(Manifest{SchemaVersion: SchemaVersion})
	if err != nil {
		return fmt.Errorf("encode vault manifest: %w", err)
	}
	if err := fsutil.AtomicWriteFile(filepath.Join(root, ManifestName), contents, 0o644); err != nil {
		return fmt.Errorf("write vault manifest: %w", err)
	}
	return nil
}

func validateManifest(path string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read vault manifest: %w", err)
	}
	var manifest Manifest
	if err := decodeYAML(contents, &manifest, ManifestName); err != nil {
		return err
	}
	if manifest.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: got %d, want %d", ErrUnsupportedVersion, manifest.SchemaVersion, SchemaVersion)
	}
	return nil
}

func installTemplates(root string) ([]string, error) {
	var installed []string
	for _, name := range templates.DefaultNames() {
		destination := filepath.Join(root, TemplatesDirName, name)
		info, err := os.Stat(destination)
		if err == nil {
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("%w: template %q is not a regular file", ErrInvalid, destination)
			}
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("inspect template %q: %w", destination, err)
		}

		contents, err := templates.ReadDefault(name)
		if err != nil {
			return nil, fmt.Errorf("read embedded template %q: %w", name, err)
		}
		if err := fsutil.AtomicWriteFile(destination, contents, 0o644); err != nil {
			return nil, fmt.Errorf("install template %q: %w", name, err)
		}
		installed = append(installed, name)
	}
	return installed, nil
}
