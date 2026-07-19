package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const (
	DirectoryName = "taskctl"
	FileName      = "config.yaml"
)

// Environment isolates platform and process-global environment lookups.
type Environment interface {
	GOOS() string
	LookupEnv(string) (string, bool)
	UserHomeDir() (string, error)
	WorkingDirectory() (string, error)
}

// OSEnvironment reads the production process environment.
type OSEnvironment struct{}

func (OSEnvironment) GOOS() string                        { return runtime.GOOS }
func (OSEnvironment) LookupEnv(key string) (string, bool) { return os.LookupEnv(key) }
func (OSEnvironment) UserHomeDir() (string, error)        { return os.UserHomeDir() }
func (OSEnvironment) WorkingDirectory() (string, error)   { return os.Getwd() }

// PathResolver follows os.UserConfigDir conventions while allowing tests to
// supply Linux/XDG and macOS environments independently of the test host.
type PathResolver struct {
	Environment Environment
}

func (r PathResolver) Path() (string, error) {
	if r.Environment == nil {
		return "", fmt.Errorf("environment is required")
	}

	var base string
	switch r.Environment.GOOS() {
	case "darwin":
		home, err := r.Environment.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, "Library", "Application Support")
	case "linux":
		if configured, ok := r.Environment.LookupEnv("XDG_CONFIG_HOME"); ok && configured != "" {
			base = configured
		} else {
			home, err := r.Environment.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, ".config")
		}
	default:
		return "", fmt.Errorf("unsupported operating system %q", r.Environment.GOOS())
	}
	if !filepath.IsAbs(base) {
		return "", fmt.Errorf("user configuration directory %q is not absolute", base)
	}
	return filepath.Join(base, DirectoryName, FileName), nil
}
