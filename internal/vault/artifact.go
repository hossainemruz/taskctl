package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/hossainemruz/taskctl/internal/domain"
	"github.com/hossainemruz/taskctl/internal/fsutil"
)

type Artifact string

const (
	ArtifactTask     Artifact = "task"
	ArtifactResearch Artifact = "research"
	ArtifactPlan     Artifact = "plan"
	ArtifactReview   Artifact = "review"
)

var ErrInvalidArtifact = errors.New("invalid artifact")

type TemplateData struct {
	TaskID    domain.TaskID
	Title     string
	ProjectID string
	CreatedAt string
}

func (a Artifact) Validate() error {
	switch a {
	case ArtifactTask, ArtifactResearch, ArtifactPlan, ArtifactReview:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrInvalidArtifact, a)
	}
}

func (a Artifact) fileName() string     { return string(a) + ".md" }
func (a Artifact) templateName() string { return a.fileName() + ".tmpl" }

// RenderArtifact executes a user-owned vault template entirely in memory.
func (s *Store) RenderArtifact(artifact Artifact, data TemplateData) ([]byte, error) {
	if err := artifact.Validate(); err != nil {
		return nil, err
	}
	path := filepath.Join(s.root, TemplatesDirName, artifact.templateName())
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: template %s", ErrNotFound, artifact.templateName())
	}
	if err != nil {
		return nil, fmt.Errorf("inspect template %s: %w", artifact.templateName(), err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: template %s is not a regular file", ErrCorrupt, artifact.templateName())
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read template %s: %w", artifact.templateName(), err)
	}
	parsed, err := template.New(artifact.templateName()).Option("missingkey=error").Parse(string(contents))
	if err != nil {
		return nil, fmt.Errorf("%w: parse template %s: %v", ErrInvalid, artifact.templateName(), err)
	}
	var rendered strings.Builder
	if err := parsed.Execute(&rendered, data); err != nil {
		return nil, fmt.Errorf("%w: render template %s: %v", ErrInvalid, artifact.templateName(), err)
	}
	return []byte(rendered.String()), nil
}

// CreateTaskWithMarkdown creates the canonical manifest and task.md as one
// directory-level operation. Any failure removes the newly-created directory.
func (s *Store) CreateTaskWithMarkdown(task domain.Task, markdown []byte) (returnErr error) {
	project, err := s.validateTaskForProject(task)
	if err != nil {
		return err
	}
	directory := filepath.Join(s.root, ProjectsDirName, project.ID, string(task.ID))
	if err := os.Mkdir(directory, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%w: task %s", ErrAlreadyExists, task.ID)
		}
		return fmt.Errorf("create task directory: %w", err)
	}
	defer func() {
		if returnErr != nil {
			_ = os.Remove(filepath.Join(directory, ArtifactTask.fileName()))
			_ = os.Remove(filepath.Join(directory, TaskManifestName))
			_ = os.Remove(directory)
		}
	}()
	if err := s.writeTask(task); err != nil {
		return err
	}
	if err := fsutil.AtomicWriteFile(filepath.Join(directory, ArtifactTask.fileName()), markdown, 0o644); err != nil {
		return fmt.Errorf("write task.md: %w", err)
	}
	return nil
}

// EnsureArtifact creates a missing optional artifact without replacing existing
// user content. The returned boolean reports whether a file was created.
func (s *Store) EnsureArtifact(projectID string, taskID domain.TaskID, artifact Artifact, contents []byte) (string, bool, error) {
	if artifact == ArtifactTask {
		return "", false, fmt.Errorf("%w: task.md is created only with a Task", ErrInvalidArtifact)
	}
	if err := artifact.Validate(); err != nil {
		return "", false, err
	}
	directory, err := s.TaskDirectory(projectID, taskID)
	if err != nil {
		return "", false, err
	}
	path := filepath.Join(directory, artifact.fileName())
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", false, fmt.Errorf("%w: artifact %s is not a regular file", ErrCorrupt, artifact)
		}
		return path, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", false, fmt.Errorf("inspect artifact %s: %w", artifact, err)
	}
	if err := fsutil.AtomicWriteFile(path, contents, 0o644); err != nil {
		return "", false, fmt.Errorf("write %s.md: %w", artifact, err)
	}
	return path, true, nil
}

func (s *Store) TaskDirectory(projectID string, taskID domain.TaskID) (string, error) {
	if _, err := s.LoadTask(projectID, taskID); err != nil {
		return "", err
	}
	return filepath.Join(s.root, ProjectsDirName, projectID, string(taskID)), nil
}

func (s *Store) ArtifactPath(projectID string, taskID domain.TaskID, artifact Artifact) (string, error) {
	if err := artifact.Validate(); err != nil {
		return "", err
	}
	directory, err := s.TaskDirectory(projectID, taskID)
	if err != nil {
		return "", err
	}
	path := filepath.Join(directory, artifact.fileName())
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%w: artifact %s for Task %s", ErrNotFound, artifact, taskID)
	}
	if err != nil {
		return "", fmt.Errorf("inspect artifact %s: %w", artifact, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: artifact %s is not a regular file", ErrCorrupt, artifact)
	}
	return path, nil
}
