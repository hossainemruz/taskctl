package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/hossainemruz/taskctl/internal/config"
	"github.com/hossainemruz/taskctl/internal/domain"
	"github.com/hossainemruz/taskctl/internal/gitcli"
	processutil "github.com/hossainemruz/taskctl/internal/process"
	"github.com/hossainemruz/taskctl/internal/vault"
)

type Workflow struct {
	environment config.Environment
	processes   processutil.Runner
	now         func() time.Time
}

func NewWorkflow(environment config.Environment, processes processutil.Runner) *Workflow {
	return &Workflow{environment: environment, processes: processes, now: time.Now}
}

// SetClock replaces the creation/transition clock. It is intended for tests and
// embedding applications that require deterministic timestamps.
func (w *Workflow) SetClock(clock func() time.Time) { w.now = clock }

type ProjectInput struct {
	Directory  string
	ProjectID  string
	Repository string
}

type NewTaskDefaults struct {
	ProjectRegistered bool
	ProjectID         string
	Repository        string
	TaskPrefix        domain.TaskPrefix
}

type NewTaskInput struct {
	ProjectInput
	Title      string
	TaskPrefix string
}

type NewTaskResult struct {
	Task      domain.Task
	Directory string
}

type TaskListItem struct {
	ID        domain.TaskID
	Title     string
	Status    domain.TaskStatus
	CreatedAt time.Time
	Current   bool
}

type ArtifactResult struct {
	Artifact vault.Artifact
	Path     string
	Created  bool
}

func (w *Workflow) NewDefaults(ctx context.Context, input ProjectInput) (NewTaskDefaults, error) {
	_, store, projects, err := w.runtime()
	if err != nil {
		return NewTaskDefaults{}, err
	}
	return w.newDefaults(ctx, input, store, projects)
}

func (w *Workflow) newDefaults(ctx context.Context, input ProjectInput, store *vault.Store, projects *ProjectService) (NewTaskDefaults, error) {
	repository, projectID, _, err := projects.registrationIdentity(ctx, RegisterProjectInput{
		Directory: input.Directory, ProjectID: input.ProjectID, Repository: input.Repository,
	})
	if err != nil {
		return NewTaskDefaults{}, err
	}
	registrations, err := store.ListProjects()
	if err != nil {
		return NewTaskDefaults{}, projects.vaultError("scan project registrations", err)
	}
	for _, existing := range registrations {
		if existing.ID == projectID {
			if existing.Repository != repository.Normalized {
				return NewTaskDefaults{}, WrapError(ErrorConflict, ErrRepositoryMismatch,
					"project ID %s is registered for a different repository", projectID)
			}
			return NewTaskDefaults{ProjectRegistered: true, ProjectID: existing.ID,
				Repository: existing.Repository, TaskPrefix: existing.TaskPrefix}, nil
		}
		if existing.Repository == repository.Normalized {
			return NewTaskDefaults{}, NewError(ErrorConflict,
				"repository %s is already registered as project %s", repository.Normalized, existing.ID)
		}
	}
	prefix, parseErr := domain.ParseTaskPrefix(suggestedTaskPrefix(repository.RepositoryName))
	if parseErr != nil {
		return NewTaskDefaults{}, WrapError(ErrorInternal, parseErr, "derive Task prefix: %v", parseErr)
	}
	return NewTaskDefaults{ProjectID: projectID, Repository: repository.Normalized, TaskPrefix: prefix}, nil
}

func (w *Workflow) NewTask(ctx context.Context, input NewTaskInput) (NewTaskResult, error) {
	_, store, projects, err := w.runtime()
	if err != nil {
		return NewTaskResult{}, err
	}
	defaults, err := w.newDefaults(ctx, input.ProjectInput, store, projects)
	if err != nil {
		return NewTaskResult{}, err
	}
	prefixText := strings.TrimSpace(input.TaskPrefix)
	if prefixText == "" {
		prefixText = string(defaults.TaskPrefix)
	}
	prefix, err := domain.ParseTaskPrefix(prefixText)
	if err != nil {
		return NewTaskResult{}, WrapError(ErrorUsage, err, "invalid Task prefix %q: %v", prefixText, err)
	}
	if defaults.ProjectRegistered && prefix != defaults.TaskPrefix {
		return NewTaskResult{}, NewError(ErrorConflict, "project %s already uses Task prefix %s", defaults.ProjectID, defaults.TaskPrefix)
	}
	var tasks []domain.Task
	if defaults.ProjectRegistered {
		tasks, err = store.ListTasks(defaults.ProjectID)
		if err != nil {
			return NewTaskResult{}, projects.vaultError("scan Tasks for project "+defaults.ProjectID, err)
		}
	}
	ids := make([]domain.TaskID, len(tasks))
	for index := range tasks {
		ids[index] = tasks[index].ID
	}
	id, err := domain.NextTaskID(prefix, ids)
	if err != nil {
		return NewTaskResult{}, WrapError(ErrorInvalidData, err, "allocate Task ID: %v", err)
	}
	createdAt := w.currentTime()
	task, err := domain.NewTask(id, input.Title, defaults.ProjectID, createdAt)
	if err != nil {
		return NewTaskResult{}, WrapError(ErrorUsage, err, "invalid Task: %v", err)
	}
	markdown, err := store.RenderArtifact(vault.ArtifactTask, taskTemplateData(task))
	if err != nil {
		return NewTaskResult{}, projects.vaultError("render task.md", err)
	}
	registration, err := projects.RegisterProject(ctx, RegisterProjectInput{
		Directory: input.Directory, ProjectID: input.ProjectID, Repository: input.Repository, TaskPrefix: string(prefix),
	})
	if err != nil {
		return NewTaskResult{}, err
	}
	if err := store.CreateTaskWithMarkdown(task, markdown); err != nil {
		kind := vaultWriteErrorKind(err)
		return NewTaskResult{}, WrapError(kind, err, "create Task %s: %v", id, err)
	}
	registration.Project.CurrentTask = id
	if err := store.SaveProject(registration.Project); err != nil {
		return NewTaskResult{}, WrapError(vaultWriteErrorKind(err), err,
			"Task %s was created, but setting it current failed: %v", id, err)
	}
	directory, err := store.TaskDirectory(task.ProjectID, task.ID)
	if err != nil {
		return NewTaskResult{}, projects.vaultError("resolve new Task directory", err)
	}
	return NewTaskResult{Task: task, Directory: directory}, nil
}

func (w *Workflow) UseTask(ctx context.Context, input ProjectInput, taskID string) (domain.Task, error) {
	_, store, projects, err := w.runtime()
	if err != nil {
		return domain.Task{}, err
	}
	project, err := projects.resolveProject(ctx, resolveInput(input))
	if err != nil {
		return domain.Task{}, err
	}
	id, err := parseTaskIDForProject(taskID, project)
	if err != nil {
		return domain.Task{}, err
	}
	task, err := store.LoadTask(project.ID, id)
	if err != nil {
		return domain.Task{}, taskLoadError(projects, id, err)
	}
	project.CurrentTask = task.ID
	if err := store.SaveProject(project); err != nil {
		return domain.Task{}, WrapError(vaultWriteErrorKind(err), err, "set current Task %s: %v", id, err)
	}
	return task, nil
}

func (w *Workflow) ListTasks(ctx context.Context, input ProjectInput) ([]TaskListItem, error) {
	_, store, projects, err := w.runtime()
	if err != nil {
		return nil, err
	}
	project, err := projects.resolveProject(ctx, resolveInput(input))
	if err != nil {
		return nil, err
	}
	tasks, err := store.ListTasks(project.ID)
	if err != nil {
		return nil, projects.vaultError("scan Tasks for project "+project.ID, err)
	}
	result := make([]TaskListItem, len(tasks))
	for index, task := range tasks {
		result[index] = TaskListItem{ID: task.ID, Title: task.Title, Status: task.Status(),
			CreatedAt: task.CreatedAt, Current: task.ID == project.CurrentTask}
	}
	return result, nil
}

func (w *Workflow) CancelTask(ctx context.Context, input ProjectInput, taskID string) (domain.Task, error) {
	_, store, projects, err := w.runtime()
	if err != nil {
		return domain.Task{}, err
	}
	var task domain.Task
	if strings.TrimSpace(taskID) == "" {
		resolved, resolveErr := projects.ResolveContext(ctx, resolveInput(input))
		if resolveErr != nil {
			return domain.Task{}, resolveErr
		}
		task = resolved.Task
	} else {
		project, resolveErr := projects.resolveProject(ctx, resolveInput(input))
		if resolveErr != nil {
			return domain.Task{}, resolveErr
		}
		id, parseErr := parseTaskIDForProject(taskID, project)
		if parseErr != nil {
			return domain.Task{}, parseErr
		}
		task, err = store.LoadTask(project.ID, id)
		if err != nil {
			return domain.Task{}, taskLoadError(projects, id, err)
		}
	}
	if err := task.Cancel(w.currentTime()); err != nil {
		return domain.Task{}, WrapError(ErrorConflict, err, "%v", err)
	}
	if err := store.SaveTask(task); err != nil {
		return domain.Task{}, WrapError(vaultWriteErrorKind(err), err, "save cancelled Task %s: %v", task.ID, err)
	}
	return task, nil
}

func (w *Workflow) EnsureArtifact(ctx context.Context, input ProjectInput, name string) (ArtifactResult, error) {
	artifact, err := parseEnsureArtifact(name)
	if err != nil {
		return ArtifactResult{}, err
	}
	_, store, projects, err := w.runtime()
	if err != nil {
		return ArtifactResult{}, err
	}
	resolved, err := projects.ResolveContext(ctx, resolveInput(input))
	if err != nil {
		return ArtifactResult{}, err
	}
	path, pathErr := store.ArtifactPath(resolved.Project.ID, resolved.Task.ID, artifact)
	if pathErr == nil {
		return ArtifactResult{Artifact: artifact, Path: path}, nil
	}
	if !errors.Is(pathErr, vault.ErrNotFound) {
		return ArtifactResult{}, projects.vaultError("inspect artifact", pathErr)
	}
	contents, err := store.RenderArtifact(artifact, taskTemplateData(resolved.Task))
	if err != nil {
		return ArtifactResult{}, projects.vaultError("render "+string(artifact)+".md", err)
	}
	path, created, err := store.EnsureArtifact(resolved.Project.ID, resolved.Task.ID, artifact, contents)
	if err != nil {
		return ArtifactResult{}, projects.vaultError("create "+string(artifact)+".md", err)
	}
	return ArtifactResult{Artifact: artifact, Path: path, Created: created}, nil
}

func (w *Workflow) ArtifactPath(ctx context.Context, input ProjectInput, name string) (string, error) {
	artifact, err := parseArtifact(name)
	if err != nil {
		return "", err
	}
	_, store, projects, err := w.runtime()
	if err != nil {
		return "", err
	}
	resolved, err := projects.ResolveContext(ctx, resolveInput(input))
	if err != nil {
		return "", err
	}
	path, err := store.ArtifactPath(resolved.Project.ID, resolved.Task.ID, artifact)
	if err != nil {
		return "", projects.vaultError("resolve "+string(artifact)+" artifact path", err)
	}
	return path, nil
}

func (w *Workflow) ViewArtifacts(ctx context.Context, input ProjectInput) (string, error) {
	local, store, projects, err := w.runtime()
	if err != nil {
		return "", err
	}
	resolved, err := projects.ResolveContext(ctx, resolveInput(input))
	if err != nil {
		return "", err
	}
	directory, err := store.TaskDirectory(resolved.Project.ID, resolved.Task.ID)
	if err != nil {
		return "", projects.vaultError("resolve Task directory", err)
	}
	arguments := append([]string(nil), local.Viewer.Args...)
	arguments = append(arguments, directory)
	if err := w.processes.Start(processutil.Command{Name: local.Viewer.Command, Args: arguments}); err != nil {
		return "", WrapError(ErrorExternalCommand, err, "start viewer %q: %v", local.Viewer.Command, err)
	}
	return directory, nil
}

func (w *Workflow) runtime() (config.Config, *vault.Store, *ProjectService, error) {
	if w == nil || w.environment == nil || w.processes == nil {
		return config.Config{}, nil, nil, NewError(ErrorInternal, "Task workflow is not configured")
	}
	path, err := (config.PathResolver{Environment: w.environment}).Path()
	if err != nil {
		return config.Config{}, nil, nil, WrapError(ErrorInvalidData, err, "resolve local configuration path: %v", err)
	}
	local, err := config.NewStore(path).Load()
	if errors.Is(err, config.ErrNotFound) {
		return config.Config{}, nil, nil, WrapError(ErrorMissingContext, err, "taskctl is not initialized; run taskctl init")
	}
	if err != nil {
		kind := ErrorInternal
		if errors.Is(err, config.ErrInvalid) || errors.Is(err, config.ErrUnsupportedVersion) {
			kind = ErrorInvalidData
		}
		return config.Config{}, nil, nil, WrapError(kind, err, "load local configuration %q: %v", path, err)
	}
	store, err := vault.NewStore(local.Vault)
	if err != nil {
		return config.Config{}, nil, nil, WrapError(ErrorInvalidData, err, "open configured vault: %v", err)
	}
	if _, err := store.LoadManifest(); err != nil {
		kind := vaultWriteErrorKind(err)
		if errors.Is(err, vault.ErrNotFound) {
			kind = ErrorMissingContext
		}
		return config.Config{}, nil, nil, WrapError(kind, err, "open configured vault %q: %v", local.Vault, err)
	}
	projects := NewProjectService(store, gitcli.NewClient(w.processes))
	return local, store, projects, nil
}

func (w *Workflow) currentTime() time.Time {
	clock := w.now
	if clock == nil {
		clock = time.Now
	}
	return clock().UTC()
}

func resolveInput(input ProjectInput) ResolveContextInput {
	return ResolveContextInput{Directory: input.Directory, ProjectID: input.ProjectID, Repository: input.Repository}
}

func parseTaskIDForProject(value string, project vault.Project) (domain.TaskID, error) {
	id := domain.TaskID(strings.TrimSpace(value))
	prefix, _, err := domain.ParseTaskID(string(id))
	if err != nil {
		return "", WrapError(ErrorUsage, err, "invalid Task ID %q: %v", value, err)
	}
	if prefix != project.TaskPrefix {
		return "", NewError(ErrorNotFound, "Task %s does not belong to project %s", id, project.ID)
	}
	return id, nil
}

func taskLoadError(projects *ProjectService, id domain.TaskID, err error) error {
	if errors.Is(err, vault.ErrNotFound) {
		return WrapError(ErrorNotFound, err, "Task %s not found", id)
	}
	return projects.vaultError("load Task "+string(id), err)
}

func taskTemplateData(task domain.Task) vault.TemplateData {
	return vault.TemplateData{TaskID: task.ID, Title: task.Title, ProjectID: task.ProjectID,
		CreatedAt: task.CreatedAt.Format(time.RFC3339)}
}

func parseArtifact(name string) (vault.Artifact, error) {
	artifact := vault.Artifact(strings.TrimSpace(name))
	if err := artifact.Validate(); err != nil {
		return "", WrapError(ErrorUsage, err, "unknown artifact %q; want task, research, plan, or review", name)
	}
	return artifact, nil
}

func parseEnsureArtifact(name string) (vault.Artifact, error) {
	artifact, err := parseArtifact(name)
	if err != nil {
		return "", err
	}
	if artifact == vault.ArtifactTask {
		return "", NewError(ErrorUsage, "task.md is created only by taskctl new")
	}
	return artifact, nil
}

func vaultWriteErrorKind(err error) ErrorKind {
	switch {
	case errors.Is(err, vault.ErrAlreadyExists):
		return ErrorConflict
	case errors.Is(err, vault.ErrInvalid), errors.Is(err, vault.ErrCorrupt),
		errors.Is(err, vault.ErrUnsupportedVersion), errors.Is(err, vault.ErrAmbiguous),
		errors.Is(err, vault.ErrDuplicate), errors.Is(err, vault.ErrInvalidArtifact):
		return ErrorInvalidData
	case errors.Is(err, vault.ErrNotFound):
		return ErrorNotFound
	default:
		return ErrorInternal
	}
}
