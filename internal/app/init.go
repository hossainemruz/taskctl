package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hossainemruz/taskctl/internal/config"
	"github.com/hossainemruz/taskctl/internal/vault"
)

// InitDefaults contains existing machine-local values that init can use as
// interactive defaults.
type InitDefaults struct {
	ConfigPath string
	Found      bool
	Vault      string
	Viewer     config.Viewer
}

// InitInput contains all values required for a non-interactive initialization.
type InitInput struct {
	Vault  string
	Viewer config.Viewer
}

// InitResult describes the files affected by initialization.
type InitResult struct {
	Vault              string
	ConfigPath         string
	VaultCreated       bool
	TemplatesInstalled []string
}

// Initializer orchestrates local configuration and vault bootstrap.
type Initializer struct {
	environment config.Environment
}

func NewInitializer(environment config.Environment) *Initializer {
	return &Initializer{environment: environment}
}

// Defaults loads the existing local configuration, if one exists.
func (a *Initializer) Defaults(ctx context.Context) (InitDefaults, error) {
	if err := ctx.Err(); err != nil {
		return InitDefaults{}, WrapError(ErrorInternal, err, "load initialization defaults: %v", err)
	}
	store, err := a.configStore()
	if err != nil {
		return InitDefaults{}, WrapError(ErrorInvalidData, err, "resolve local configuration path: %v", err)
	}

	defaults := InitDefaults{ConfigPath: store.Path()}
	local, err := store.Load()
	if errors.Is(err, config.ErrNotFound) {
		return defaults, nil
	}
	if err != nil {
		kind := ErrorInternal
		if errors.Is(err, config.ErrInvalid) || errors.Is(err, config.ErrUnsupportedVersion) {
			kind = ErrorInvalidData
		}
		return InitDefaults{}, WrapError(kind, err, "load local configuration %q: %v", store.Path(), err)
	}

	defaults.Found = true
	defaults.Vault = local.Vault
	defaults.Viewer = local.Viewer
	return defaults, nil
}

// Init prepares a compatible vault before saving the machine-local pointer to
// it. A failed vault initialization therefore never replaces a working config.
func (a *Initializer) Init(ctx context.Context, input InitInput) (InitResult, error) {
	if err := ctx.Err(); err != nil {
		return InitResult{}, WrapError(ErrorInternal, err, "initialize taskctl: %v", err)
	}
	if strings.TrimSpace(input.Vault) == "" {
		return InitResult{}, NewError(ErrorUsage, "vault path is required")
	}
	if strings.TrimSpace(input.Viewer.Command) == "" {
		return InitResult{}, NewError(ErrorUsage, "viewer command is required")
	}

	vaultPath, err := normalizeVaultPath(input.Vault, a.environment)
	if err != nil {
		return InitResult{}, WrapError(ErrorUsage, err, "resolve vault path: %v", err)
	}
	store, err := a.configStore()
	if err != nil {
		return InitResult{}, WrapError(ErrorInvalidData, err, "resolve local configuration path: %v", err)
	}
	local := config.Config{
		SchemaVersion: config.SchemaVersion,
		Vault:         vaultPath,
		Viewer: config.Viewer{
			Command: strings.TrimSpace(input.Viewer.Command),
			Args:    append([]string(nil), input.Viewer.Args...),
		},
	}
	if err := local.Validate(); err != nil {
		return InitResult{}, WrapError(ErrorUsage, err, "invalid initialization settings: %v", err)
	}

	vaultResult, err := vault.Initialize(vaultPath)
	if err != nil {
		kind := ErrorInternal
		if errors.Is(err, vault.ErrInvalid) || errors.Is(err, vault.ErrIncompatible) || errors.Is(err, vault.ErrUnsupportedVersion) {
			kind = ErrorInvalidData
		}
		return InitResult{}, WrapError(kind, err, "initialize vault %q: %v", vaultPath, err)
	}

	if err := store.Save(local); err != nil {
		return InitResult{}, WrapError(ErrorInternal, err, "vault is ready, but saving local configuration %q failed: %v", store.Path(), err)
	}

	return InitResult{
		Vault:              vaultPath,
		ConfigPath:         store.Path(),
		VaultCreated:       vaultResult.Created,
		TemplatesInstalled: vaultResult.TemplatesInstalled,
	}, nil
}

func (a *Initializer) configStore() (*config.Store, error) {
	path, err := (config.PathResolver{Environment: a.environment}).Path()
	if err != nil {
		return nil, err
	}
	return config.NewStore(path), nil
}

func normalizeVaultPath(path string, environment config.Environment) (string, error) {
	path = strings.TrimSpace(path)
	if path == "~" || strings.HasPrefix(path, "~"+string(filepath.Separator)) {
		home, err := environment.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, strings.TrimPrefix(path, "~"+string(filepath.Separator)))
		}
	}
	if !filepath.IsAbs(path) {
		workingDirectory, err := environment.WorkingDirectory()
		if err != nil {
			return "", fmt.Errorf("resolve working directory: %w", err)
		}
		path = filepath.Join(workingDirectory, path)
	}
	return filepath.Clean(path), nil
}
