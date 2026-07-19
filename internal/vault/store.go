package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hossainemruz/taskctl/internal/domain"
	"github.com/hossainemruz/taskctl/internal/fsutil"
	"go.yaml.in/yaml/v3"
)

const (
	ProjectManifestName = "project.yaml"
	TaskManifestName    = "task.yaml"
)

var (
	ErrNotFound      = errors.New("vault object not found")
	ErrAlreadyExists = errors.New("vault object already exists")
	ErrCorrupt       = errors.New("corrupt vault state")
	ErrDuplicate     = errors.New("duplicate vault identity")
	ErrAmbiguous     = errors.New("ambiguous vault state")
)

// Project is synchronized project metadata. Task and PR status are deliberately
// absent; those values are derived from Task manifests.
type Project struct {
	SchemaVersion int               `yaml:"schema_version"`
	ID            string            `yaml:"id"`
	Repository    string            `yaml:"repository"`
	TaskPrefix    domain.TaskPrefix `yaml:"task_prefix"`
	CurrentTask   domain.TaskID     `yaml:"current_task"`
}

func (p Project) Validate() error {
	if p.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: project schema version %d, want %d", ErrUnsupportedVersion, p.SchemaVersion, SchemaVersion)
	}
	if err := ValidateProjectID(p.ID); err != nil {
		return fmt.Errorf("%w: project id: %v", ErrInvalid, err)
	}
	if err := ValidateRepository(p.Repository); err != nil {
		return fmt.Errorf("%w: repository: %v", ErrInvalid, err)
	}
	prefix, err := domain.ParseTaskPrefix(string(p.TaskPrefix))
	if err != nil {
		return fmt.Errorf("%w: task_prefix: %v", ErrInvalid, err)
	}
	if p.CurrentTask != "" {
		currentPrefix, _, err := domain.ParseTaskID(string(p.CurrentTask))
		if err != nil {
			return fmt.Errorf("%w: current_task: %v", ErrInvalid, err)
		}
		if currentPrefix != prefix {
			return fmt.Errorf("%w: current_task %s does not use task_prefix %s", ErrInvalid, p.CurrentTask, prefix)
		}
	}
	return nil
}

// ValidateProjectID rejects any value that could escape its vault directory.
func ValidateProjectID(id string) error {
	return validateSegment(id, "project ID")
}

// ValidateRepository validates the canonical host/ownership/repository shape
// stored in project.yaml. Remote URL parsing and normalization belongs to
// gitcli; the store independently rejects unsafe or non-canonical path forms.
func ValidateRepository(repository string) error {
	if repository == "" || repository != strings.TrimSpace(repository) {
		return fmt.Errorf("must be nonempty and have no surrounding whitespace")
	}
	if strings.ContainsAny(repository, "\\\x00\r\n?#") || strings.Contains(repository, "://") {
		return fmt.Errorf("must be a normalized host/path identity")
	}
	parts := strings.Split(repository, "/")
	if len(parts) < 3 {
		return fmt.Errorf("must contain a host, ownership path, and repository")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || part != strings.TrimSpace(part) {
			return fmt.Errorf("contains an invalid path segment")
		}
	}
	if strings.HasSuffix(strings.ToLower(parts[len(parts)-1]), ".git") {
		return fmt.Errorf("must not include a .git suffix")
	}
	return nil
}

func validateSegment(value, label string) error {
	if value == "" || value == "." || value == ".." {
		return fmt.Errorf("%s must be a nonempty path-safe identifier", label)
	}
	if value != filepath.Base(value) || strings.ContainsAny(value, "/\\\x00") {
		return fmt.Errorf("%s contains an unsafe path character", label)
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '_' || character == '-' || character == '.' {
			continue
		}
		return fmt.Errorf("%s contains a non-portable character", label)
	}
	return nil
}

// Store owns all vault path construction and strict YAML persistence.
type Store struct {
	root string
}

func NewStore(root string) (*Store, error) {
	if root == "" || !filepath.IsAbs(root) || strings.ContainsRune(root, '\x00') {
		return nil, fmt.Errorf("%w: vault root must be an absolute path", ErrInvalid)
	}
	return &Store{root: filepath.Clean(root)}, nil
}

func (s *Store) Root() string { return s.root }

func (s *Store) LoadManifest() (Manifest, error) {
	var manifest Manifest
	if err := loadYAML(filepath.Join(s.root, ManifestName), ManifestName, &manifest); err != nil {
		return Manifest{}, err
	}
	if manifest.SchemaVersion != SchemaVersion {
		return Manifest{}, fmt.Errorf("%w: vault schema version %d, want %d", ErrUnsupportedVersion, manifest.SchemaVersion, SchemaVersion)
	}
	return manifest, nil
}

func (s *Store) SaveManifest(manifest Manifest) error {
	if manifest.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: vault schema version %d, want %d", ErrUnsupportedVersion, manifest.SchemaVersion, SchemaVersion)
	}
	return saveYAML(filepath.Join(s.root, ManifestName), ManifestName, manifest)
}

func (s *Store) LoadProject(projectID string) (Project, error) {
	if err := ValidateProjectID(projectID); err != nil {
		return Project{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return s.loadProjectPath(projectID)
}

func (s *Store) loadProjectPath(projectID string) (Project, error) {
	var project Project
	directory := filepath.Join(s.root, ProjectsDirName, projectID)
	if err := inspectDirectory(directory, "project "+projectID); err != nil {
		return Project{}, err
	}
	path := filepath.Join(directory, ProjectManifestName)
	if err := loadYAML(path, ProjectManifestName, &project); err != nil {
		return Project{}, err
	}
	if err := project.Validate(); err != nil {
		return Project{}, fmt.Errorf("validate %s for project %s: %w", ProjectManifestName, projectID, err)
	}
	if project.ID != projectID {
		return Project{}, fmt.Errorf("%w: project directory %s contains project id %s", ErrCorrupt, projectID, project.ID)
	}
	return project, nil
}

// CreateProject creates a registration without overwriting an existing project.
func (s *Store) CreateProject(project Project) (returnErr error) {
	if err := project.Validate(); err != nil {
		return err
	}
	directory := filepath.Join(s.root, ProjectsDirName, project.ID)
	if err := os.Mkdir(directory, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%w: project %s", ErrAlreadyExists, project.ID)
		}
		return fmt.Errorf("create project directory: %w", err)
	}
	defer func() {
		if returnErr != nil {
			_ = os.Remove(directory)
		}
	}()
	return s.writeProject(project)
}

func (s *Store) SaveProject(project Project) error {
	if err := project.Validate(); err != nil {
		return err
	}
	directory := filepath.Join(s.root, ProjectsDirName, project.ID)
	if err := inspectDirectory(directory, "project "+project.ID); err != nil {
		return err
	}
	return s.writeProject(project)
}

func (s *Store) writeProject(project Project) error {
	path := filepath.Join(s.root, ProjectsDirName, project.ID, ProjectManifestName)
	return saveYAML(path, ProjectManifestName, project)
}

// ListProjects scans only direct project directories and returns them in ID
// order. Non-directory files (including abandoned atomic temp files) are ignored.
func (s *Store) ListProjects() ([]Project, error) {
	directory := filepath.Join(s.root, ProjectsDirName)
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: projects directory", ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("scan projects: %w", err)
	}
	projects := make([]Project, 0, len(entries))
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: project entry %q is a symbolic link", ErrCorrupt, entry.Name())
		}
		if !entry.IsDir() {
			continue
		}
		if err := ValidateProjectID(entry.Name()); err != nil {
			return nil, fmt.Errorf("%w: invalid project directory %q: %v", ErrCorrupt, entry.Name(), err)
		}
		project, err := s.loadProjectPath(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("%w: load project %s: %w", ErrCorrupt, entry.Name(), err)
		}
		projects = append(projects, project)
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].ID < projects[j].ID })
	return projects, nil
}

// FindProjectByRepository requires an exact full normalized identity match.
func (s *Store) FindProjectByRepository(repository string) (Project, error) {
	if err := ValidateRepository(repository); err != nil {
		return Project{}, fmt.Errorf("%w: repository: %v", ErrInvalid, err)
	}
	projects, err := s.ListProjects()
	if err != nil {
		return Project{}, err
	}
	var matches []Project
	for _, project := range projects {
		if project.Repository == repository {
			matches = append(matches, project)
		}
	}
	if len(matches) == 0 {
		return Project{}, fmt.Errorf("%w: repository %s", ErrNotFound, repository)
	}
	if len(matches) > 1 {
		return Project{}, fmt.Errorf("%w: repository %s is registered by %d projects", ErrAmbiguous, repository, len(matches))
	}
	return matches[0], nil
}

func (s *Store) LoadTask(projectID string, taskID domain.TaskID) (domain.Task, error) {
	if err := ValidateProjectID(projectID); err != nil {
		return domain.Task{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if _, _, err := domain.ParseTaskID(string(taskID)); err != nil {
		return domain.Task{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	task, err := s.loadTaskPath(projectID, string(taskID))
	if err != nil {
		return domain.Task{}, err
	}
	if task.ID != taskID || task.ProjectID != projectID {
		return domain.Task{}, fmt.Errorf("%w: task path %s/%s contains task %s for project %s", ErrCorrupt, projectID, taskID, task.ID, task.ProjectID)
	}
	return task, nil
}

func (s *Store) loadTaskPath(projectID, directoryName string) (domain.Task, error) {
	var task domain.Task
	directory := filepath.Join(s.root, ProjectsDirName, projectID, directoryName)
	if err := inspectDirectory(directory, "task "+directoryName); err != nil {
		return domain.Task{}, err
	}
	path := filepath.Join(directory, TaskManifestName)
	if err := loadYAML(path, TaskManifestName, &task); err != nil {
		return domain.Task{}, err
	}
	if task.SchemaVersion != domain.SchemaVersion {
		return domain.Task{}, fmt.Errorf("%w: task schema version %d, want %d", ErrUnsupportedVersion, task.SchemaVersion, domain.SchemaVersion)
	}
	if err := task.Validate(); err != nil {
		return domain.Task{}, fmt.Errorf("%w: validate %s: %v", ErrInvalid, TaskManifestName, err)
	}
	return task, nil
}

func (s *Store) SaveTask(task domain.Task) error {
	project, err := s.validateTaskForProject(task)
	if err != nil {
		return err
	}
	directory := filepath.Join(s.root, ProjectsDirName, project.ID, string(task.ID))
	if err := inspectDirectory(directory, "task "+string(task.ID)); err != nil {
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		if err := os.Mkdir(directory, 0o755); err != nil {
			return fmt.Errorf("create task directory: %w", err)
		}
	}
	return s.writeTask(task)
}

// CreateTask provides the collision-safe primitive used by Task creation.
func (s *Store) CreateTask(task domain.Task) (returnErr error) {
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
			_ = os.Remove(directory)
		}
	}()
	return s.writeTask(task)
}

func (s *Store) validateTaskForProject(task domain.Task) (Project, error) {
	if task.SchemaVersion != domain.SchemaVersion {
		return Project{}, fmt.Errorf("%w: task schema version %d, want %d", ErrUnsupportedVersion, task.SchemaVersion, domain.SchemaVersion)
	}
	if err := task.Validate(); err != nil {
		return Project{}, fmt.Errorf("%w: invalid task: %v", ErrInvalid, err)
	}
	project, err := s.LoadProject(task.ProjectID)
	if err != nil {
		return Project{}, err
	}
	prefix, _, _ := domain.ParseTaskID(string(task.ID))
	if prefix != project.TaskPrefix {
		return Project{}, fmt.Errorf("%w: task %s does not use project prefix %s", ErrInvalid, task.ID, project.TaskPrefix)
	}
	return project, nil
}

func (s *Store) writeTask(task domain.Task) error {
	path := filepath.Join(s.root, ProjectsDirName, task.ProjectID, string(task.ID), TaskManifestName)
	return saveYAML(path, TaskManifestName, task)
}

// ListTasks scans only direct Task directories under projectID. A malformed
// directory, mismatched manifest, duplicate manifest ID, or unsupported schema
// is reported as corruption rather than silently skipped.
func (s *Store) ListTasks(projectID string) ([]domain.Task, error) {
	if err := ValidateProjectID(projectID); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	project, err := s.LoadProject(projectID)
	if err != nil {
		return nil, err
	}
	directory := filepath.Join(s.root, ProjectsDirName, projectID)
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("scan tasks for project %s: %w", projectID, err)
	}

	tasks := make([]domain.Task, 0, len(entries))
	seen := make(map[domain.TaskID]string)
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: Task entry %q is a symbolic link", ErrCorrupt, entry.Name())
		}
		if !entry.IsDir() {
			continue
		}
		directoryID := domain.TaskID(entry.Name())
		prefix, _, parseErr := domain.ParseTaskID(entry.Name())
		if parseErr != nil || prefix != project.TaskPrefix {
			return nil, fmt.Errorf("%w: invalid Task directory %q for prefix %s", ErrCorrupt, entry.Name(), project.TaskPrefix)
		}
		task, loadErr := s.loadTaskPath(projectID, entry.Name())
		if loadErr != nil {
			return nil, fmt.Errorf("%w: load task directory %s: %w", ErrCorrupt, entry.Name(), loadErr)
		}
		if previous, exists := seen[task.ID]; exists {
			return nil, fmt.Errorf("%w: %w: task id %s appears in directories %s and %s", ErrCorrupt, ErrDuplicate, task.ID, previous, entry.Name())
		}
		seen[task.ID] = entry.Name()
		if task.ID != directoryID || task.ProjectID != projectID {
			return nil, fmt.Errorf("%w: task directory %s contains task %s for project %s", ErrCorrupt, entry.Name(), task.ID, task.ProjectID)
		}
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool {
		_, left, _ := domain.ParseTaskID(string(tasks[i].ID))
		_, right, _ := domain.ParseTaskID(string(tasks[j].ID))
		return left < right
	})
	return tasks, nil
}

func loadYAML(path, label string, destination any) error {
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrNotFound, path)
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", label, err)
	}
	return decodeYAML(contents, destination, label)
}

func saveYAML(path, label string, value any) error {
	contents, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode %s: %w", label, err)
	}
	if err := fsutil.AtomicWriteFile(path, contents, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", label, err)
	}
	return nil
}

func inspectDirectory(path, label string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrNotFound, label)
	}
	if err != nil {
		return fmt.Errorf("inspect %s: %w", label, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%w: %s is not a direct directory", ErrCorrupt, label)
	}
	return nil
}
