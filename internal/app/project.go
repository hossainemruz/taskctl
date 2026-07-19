package app

import (
	"context"
	"errors"
	"strings"

	"github.com/hossainemruz/taskctl/internal/domain"
	"github.com/hossainemruz/taskctl/internal/gitcli"
	"github.com/hossainemruz/taskctl/internal/vault"
)

var (
	ErrProjectNotRegistered = errors.New("project is not registered")
	ErrRepositoryMismatch   = errors.New("repository identity does not match project registration")
	ErrNoCurrentTask        = errors.New("project has no current Task")
	ErrStaleCurrentTask     = errors.New("project current Task does not exist")
	ErrAmbiguousBranch      = errors.New("branch is associated with multiple PRs")
)

// ProjectService provides project registration and current-context facts to
// later application workflows without exposing vault paths or Git syntax.
type ProjectService struct {
	store *vault.Store
	git   *gitcli.Client
}

func NewProjectService(store *vault.Store, git *gitcli.Client) *ProjectService {
	return &ProjectService{store: store, git: git}
}

type RegisterProjectInput struct {
	Directory  string
	ProjectID  string
	Repository string
	TaskPrefix string
}

type RegisterProjectResult struct {
	Project vault.Project
	Created bool
}

// RegisterProject creates synchronized project metadata. ProjectID and
// Repository are an all-or-nothing explicit identity pair for repositories
// without a usable portable origin. An existing matching registration is
// returned unchanged.
func (s *ProjectService) RegisterProject(ctx context.Context, input RegisterProjectInput) (RegisterProjectResult, error) {
	if err := s.ready(); err != nil {
		return RegisterProjectResult{}, err
	}
	repository, projectID, explicit, err := s.registrationIdentity(ctx, input)
	if err != nil {
		return RegisterProjectResult{}, err
	}

	prefixText := strings.TrimSpace(input.TaskPrefix)
	if prefixText == "" {
		prefixText = suggestedTaskPrefix(repository.RepositoryName)
	}
	prefix, err := domain.ParseTaskPrefix(prefixText)
	if err != nil {
		return RegisterProjectResult{}, WrapError(ErrorUsage, err, "invalid Task prefix %q: %v", prefixText, err)
	}

	projects, err := s.store.ListProjects()
	if err != nil {
		return RegisterProjectResult{}, s.vaultError("scan project registrations", err)
	}
	for _, existing := range projects {
		if existing.ID == projectID {
			if existing.Repository != repository.Normalized {
				return RegisterProjectResult{}, WrapError(ErrorConflict, ErrRepositoryMismatch,
					"project ID %s is registered for a different repository", projectID)
			}
			if input.TaskPrefix != "" && existing.TaskPrefix != prefix {
				return RegisterProjectResult{}, NewError(ErrorConflict,
					"project %s already uses Task prefix %s", projectID, existing.TaskPrefix)
			}
			return RegisterProjectResult{Project: existing}, nil
		}
		if existing.Repository == repository.Normalized {
			return RegisterProjectResult{}, NewError(ErrorConflict,
				"repository %s is already registered as project %s", repository.Normalized, existing.ID)
		}
	}

	project := vault.Project{
		SchemaVersion: vault.SchemaVersion,
		ID:            projectID,
		Repository:    repository.Normalized,
		TaskPrefix:    prefix,
	}
	if err := project.Validate(); err != nil {
		kind := ErrorInvalidData
		if explicit {
			kind = ErrorUsage
		}
		return RegisterProjectResult{}, WrapError(kind, err, "invalid project registration: %v", err)
	}
	if err := s.store.CreateProject(project); err != nil {
		kind := ErrorInternal
		if errors.Is(err, vault.ErrAlreadyExists) {
			kind = ErrorConflict
		} else if errors.Is(err, vault.ErrInvalid) || errors.Is(err, vault.ErrUnsupportedVersion) {
			kind = ErrorInvalidData
		}
		return RegisterProjectResult{}, WrapError(kind, err, "create project registration: %v", err)
	}
	return RegisterProjectResult{Project: project, Created: true}, nil
}

func (s *ProjectService) registrationIdentity(ctx context.Context, input RegisterProjectInput) (gitcli.Repository, string, bool, error) {
	hasProjectID := strings.TrimSpace(input.ProjectID) != ""
	hasRepository := strings.TrimSpace(input.Repository) != ""
	if hasProjectID != hasRepository {
		return gitcli.Repository{}, "", true, NewError(ErrorUsage,
			"explicit project ID and repository identity must be provided together")
	}
	if !hasProjectID {
		repository, err := s.git.Repository(ctx, input.Directory)
		if err != nil {
			return gitcli.Repository{}, "", false, s.gitIdentityError(err)
		}
		return repository, repository.ProjectID, false, nil
	}

	repository, err := gitcli.NormalizeRepository(input.Repository)
	if err != nil {
		return gitcli.Repository{}, "", true, WrapError(ErrorUsage, err, "invalid explicit repository identity: %v", err)
	}
	projectID := strings.TrimSpace(input.ProjectID)
	if err := vault.ValidateProjectID(projectID); err != nil {
		return gitcli.Repository{}, "", true, WrapError(ErrorUsage, err, "invalid explicit project ID: %v", err)
	}
	if input.Directory != "" {
		detected, detectErr := s.git.Repository(ctx, input.Directory)
		switch {
		case detectErr == nil && detected.Normalized != repository.Normalized:
			return gitcli.Repository{}, "", true, WrapError(ErrorConflict, ErrRepositoryMismatch,
				"explicit repository identity does not match the current Git origin")
		case detectErr == nil:
		case errors.Is(detectErr, gitcli.ErrNoOrigin),
			errors.Is(detectErr, gitcli.ErrUnsupportedRemote),
			errors.Is(detectErr, gitcli.ErrInvalidRemote):
			// Explicit identity is specifically the fallback for an unusable origin.
		default:
			return gitcli.Repository{}, "", true, s.gitIdentityError(detectErr)
		}
	}
	return repository, projectID, true, nil
}

func suggestedTaskPrefix(repositoryName string) string {
	var result strings.Builder
	for _, character := range strings.ToUpper(repositoryName) {
		if (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') {
			result.WriteRune(character)
		}
	}
	value := result.String()
	if value == "" || value[0] < 'A' || value[0] > 'Z' {
		return "TASK"
	}
	return value
}

type ResolveContextInput struct {
	Directory  string
	ProjectID  string
	Repository string
}

type ContextSelection string

const (
	SelectionBranch  ContextSelection = "branch"
	SelectionCurrent ContextSelection = "current_task"
)

type ResolvedContext struct {
	Project   vault.Project
	Task      domain.Task
	CurrentPR *domain.PR
	Branch    string
	Selection ContextSelection
}

// ResolveContext gives an exact branch association precedence over the
// synchronized current_task fallback. It never guesses when registrations or
// branch associations are ambiguous.
func (s *ProjectService) ResolveContext(ctx context.Context, input ResolveContextInput) (ResolvedContext, error) {
	if err := s.ready(); err != nil {
		return ResolvedContext{}, err
	}
	project, err := s.resolveProject(ctx, input)
	if err != nil {
		return ResolvedContext{}, err
	}

	branch, err := s.git.CurrentBranch(ctx, input.Directory)
	if errors.Is(err, gitcli.ErrDetachedHEAD) {
		branch = ""
	} else if err != nil {
		return ResolvedContext{}, WrapError(ErrorExternalCommand, err, "resolve current Git branch: %v", err)
	}

	tasks, err := s.store.ListTasks(project.ID)
	if err != nil {
		return ResolvedContext{}, s.vaultError("scan Tasks for project "+project.ID, err)
	}
	if branch != "" {
		type match struct {
			task domain.Task
			pr   domain.PR
		}
		matches := make([]match, 0, 1)
		for _, task := range tasks {
			for _, pr := range task.PRs {
				if pr.Branch == branch {
					matches = append(matches, match{task: task, pr: pr})
				}
			}
		}
		if len(matches) > 1 {
			return ResolvedContext{}, WrapError(ErrorInvalidData, ErrAmbiguousBranch,
				"branch %q is associated with %d PRs in project %s", branch, len(matches), project.ID)
		}
		if len(matches) == 1 {
			currentPR := matches[0].pr
			return ResolvedContext{
				Project:   project,
				Task:      matches[0].task,
				CurrentPR: &currentPR,
				Branch:    branch,
				Selection: SelectionBranch,
			}, nil
		}
	}

	if project.CurrentTask == "" {
		return ResolvedContext{}, WrapError(ErrorMissingContext, ErrNoCurrentTask,
			"project %s has no current Task; run taskctl new or taskctl use", project.ID)
	}
	for _, task := range tasks {
		if task.ID == project.CurrentTask {
			return ResolvedContext{
				Project:   project,
				Task:      task,
				Branch:    branch,
				Selection: SelectionCurrent,
			}, nil
		}
	}
	return ResolvedContext{}, WrapError(ErrorInvalidData, ErrStaleCurrentTask,
		"project %s selects missing Task %s; run taskctl use with an existing Task", project.ID, project.CurrentTask)
}

func (s *ProjectService) resolveProject(ctx context.Context, input ResolveContextInput) (vault.Project, error) {
	hasProjectID := strings.TrimSpace(input.ProjectID) != ""
	hasRepository := strings.TrimSpace(input.Repository) != ""
	if hasProjectID != hasRepository {
		return vault.Project{}, NewError(ErrorUsage,
			"explicit project ID and repository identity must be provided together")
	}
	if hasProjectID {
		repository, err := gitcli.NormalizeRepository(input.Repository)
		if err != nil {
			return vault.Project{}, WrapError(ErrorUsage, err, "invalid explicit repository identity: %v", err)
		}
		projectID := strings.TrimSpace(input.ProjectID)
		if err := vault.ValidateProjectID(projectID); err != nil {
			return vault.Project{}, WrapError(ErrorUsage, err, "invalid explicit project ID: %v", err)
		}
		project, err := s.store.LoadProject(projectID)
		if err != nil {
			if errors.Is(err, vault.ErrNotFound) {
				return vault.Project{}, WrapError(ErrorMissingContext, ErrProjectNotRegistered,
					"project %s is not registered", projectID)
			}
			return vault.Project{}, s.vaultError("load project "+projectID, err)
		}
		if project.Repository != repository.Normalized {
			return vault.Project{}, WrapError(ErrorConflict, ErrRepositoryMismatch,
				"project %s is registered for a different repository", projectID)
		}
		if input.Directory != "" {
			detected, detectErr := s.git.Repository(ctx, input.Directory)
			switch {
			case detectErr == nil && detected.Normalized != repository.Normalized:
				return vault.Project{}, WrapError(ErrorConflict, ErrRepositoryMismatch,
					"current Git origin does not match project %s", projectID)
			case detectErr == nil:
			case errors.Is(detectErr, gitcli.ErrNoOrigin),
				errors.Is(detectErr, gitcli.ErrUnsupportedRemote),
				errors.Is(detectErr, gitcli.ErrInvalidRemote):
			default:
				return vault.Project{}, s.gitIdentityError(detectErr)
			}
		}
		return project, nil
	}

	repository, err := s.git.Repository(ctx, input.Directory)
	if err != nil {
		return vault.Project{}, s.gitIdentityError(err)
	}
	project, err := s.store.FindProjectByRepository(repository.Normalized)
	if err != nil {
		switch {
		case errors.Is(err, vault.ErrNotFound):
			return vault.Project{}, WrapError(ErrorMissingContext, ErrProjectNotRegistered,
				"repository %s is not registered; run taskctl new", repository.Normalized)
		case errors.Is(err, vault.ErrAmbiguous):
			return vault.Project{}, WrapError(ErrorInvalidData, err,
				"repository %s has ambiguous project registrations", repository.Normalized)
		default:
			return vault.Project{}, s.vaultError("resolve project registration", err)
		}
	}
	return project, nil
}

func (s *ProjectService) ready() error {
	if s == nil || s.store == nil || s.git == nil {
		return NewError(ErrorInternal, "project service is not configured")
	}
	return nil
}

func (s *ProjectService) gitIdentityError(err error) error {
	switch {
	case errors.Is(err, gitcli.ErrNoOrigin),
		errors.Is(err, gitcli.ErrUnsupportedRemote),
		errors.Is(err, gitcli.ErrInvalidRemote):
		return WrapError(ErrorMissingContext, err,
			"current repository has no usable portable origin; provide explicit project and repository identities")
	default:
		return WrapError(ErrorExternalCommand, err, "inspect current Git repository: %v", err)
	}
}

func (s *ProjectService) vaultError(operation string, err error) error {
	kind := ErrorInternal
	if errors.Is(err, vault.ErrInvalid) || errors.Is(err, vault.ErrCorrupt) ||
		errors.Is(err, vault.ErrUnsupportedVersion) || errors.Is(err, vault.ErrAmbiguous) ||
		errors.Is(err, vault.ErrDuplicate) {
		kind = ErrorInvalidData
	} else if errors.Is(err, vault.ErrNotFound) {
		kind = ErrorNotFound
	}
	return WrapError(kind, err, "%s: %v", operation, err)
}
