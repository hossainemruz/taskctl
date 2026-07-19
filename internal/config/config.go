package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/hossainemruz/taskctl/internal/fsutil"
	"go.yaml.in/yaml/v3"
)

const SchemaVersion = 1

var (
	ErrNotFound           = errors.New("local configuration not found")
	ErrInvalid            = errors.New("invalid local configuration")
	ErrUnsupportedVersion = errors.New("unsupported local configuration schema version")
)

// Viewer is an executable and its argument vector, never a shell command.
type Viewer struct {
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
}

// Config is the versioned, machine-local taskctl configuration.
type Config struct {
	SchemaVersion int    `yaml:"schema_version"`
	Vault         string `yaml:"vault"`
	Viewer        Viewer `yaml:"viewer"`
}

func (c Config) Validate() error {
	if c.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: got %d, want %d", ErrUnsupportedVersion, c.SchemaVersion, SchemaVersion)
	}
	if c.Vault == "" || !filepath.IsAbs(c.Vault) {
		return fmt.Errorf("%w: vault must be an absolute path", ErrInvalid)
	}
	if strings.ContainsRune(c.Vault, '\x00') {
		return fmt.Errorf("%w: vault path contains a NUL byte", ErrInvalid)
	}
	if strings.TrimSpace(c.Viewer.Command) == "" {
		return fmt.Errorf("%w: viewer command is required", ErrInvalid)
	}
	if c.Viewer.Command != strings.TrimSpace(c.Viewer.Command) {
		return fmt.Errorf("%w: viewer command must not have surrounding whitespace", ErrInvalid)
	}
	if strings.ContainsRune(c.Viewer.Command, '\x00') {
		return fmt.Errorf("%w: viewer command contains a NUL byte", ErrInvalid)
	}
	for index, argument := range c.Viewer.Args {
		if strings.ContainsRune(argument, '\x00') {
			return fmt.Errorf("%w: viewer argument %d contains a NUL byte", ErrInvalid, index+1)
		}
	}
	return nil
}

// Store loads and saves one local config file.
type Store struct {
	path string
}

func NewStore(path string) *Store {
	return &Store{path: filepath.Clean(path)}
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) Load() (Config, error) {
	contents, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, ErrNotFound
	}
	if err != nil {
		return Config{}, err
	}

	var result Config
	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)
	if err := decoder.Decode(&result); err != nil {
		return Config{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Config{}, fmt.Errorf("%w: multiple YAML documents", ErrInvalid)
		}
		return Config{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := result.Validate(); err != nil {
		return Config{}, err
	}
	return result, nil
}

func (s *Store) Save(value Config) error {
	if err := value.Validate(); err != nil {
		return err
	}
	contents, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode local configuration: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create local configuration directory: %w", err)
	}
	if err := fsutil.AtomicWriteFile(s.path, contents, 0o600); err != nil {
		return fmt.Errorf("write local configuration: %w", err)
	}
	return nil
}
