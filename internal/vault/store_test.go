package vault

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hossainemruz/taskctl/internal/domain"
)

var storeTestTime = time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	root := filepath.Join(t.TempDir(), "vault")
	if _, err := Initialize(root); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func storeTestProject() Project {
	return Project{
		SchemaVersion: SchemaVersion,
		ID:            "org_taskctl",
		Repository:    "github.com/org/taskctl",
		TaskPrefix:    "TASKCTL",
	}
}

func storeTestTask(id domain.TaskID) domain.Task {
	return domain.Task{
		SchemaVersion: domain.SchemaVersion,
		ID:            id,
		Title:         "Implement storage",
		ProjectID:     "org_taskctl",
		CreatedAt:     storeTestTime,
		PRs:           []domain.PR{},
	}
}

func TestStoreRoundTripsRootProjectAndTask(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	if _, err := store.LoadManifest(); err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}
	if err := store.SaveManifest(Manifest{SchemaVersion: SchemaVersion}); err != nil {
		t.Fatalf("SaveManifest() error = %v", err)
	}

	project := storeTestProject()
	if err := store.CreateProject(project); err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	loadedProject, err := store.LoadProject(project.ID)
	if err != nil {
		t.Fatalf("LoadProject() error = %v", err)
	}
	if !reflect.DeepEqual(loadedProject, project) {
		t.Fatalf("LoadProject() = %#v, want %#v", loadedProject, project)
	}

	task := storeTestTask("TASKCTL-001")
	if err := store.CreateTask(task); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	loadedTask, err := store.LoadTask(project.ID, task.ID)
	if err != nil {
		t.Fatalf("LoadTask() error = %v", err)
	}
	if !reflect.DeepEqual(loadedTask, task) {
		t.Fatalf("LoadTask() = %#v, want %#v", loadedTask, task)
	}

	project.CurrentTask = task.ID
	if err := store.SaveProject(project); err != nil {
		t.Fatalf("SaveProject() error = %v", err)
	}
	task.Title = "Updated title"
	if err := store.SaveTask(task); err != nil {
		t.Fatalf("SaveTask() error = %v", err)
	}

	projectYAML, err := os.ReadFile(filepath.Join(store.Root(), ProjectsDirName, project.ID, ProjectManifestName))
	if err != nil {
		t.Fatal(err)
	}
	wantProjectYAML := "schema_version: 1\nid: org_taskctl\nrepository: github.com/org/taskctl\ntask_prefix: TASKCTL\ncurrent_task: TASKCTL-001\n"
	if string(projectYAML) != wantProjectYAML {
		t.Fatalf("project YAML = %q, want %q", projectYAML, wantProjectYAML)
	}
	entries, err := os.ReadDir(filepath.Join(store.Root(), ProjectsDirName, project.ID, string(task.ID)))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != TaskManifestName {
		t.Fatalf("Task directory entries = %v", entries)
	}
}

func TestStoreStrictYAMLAndSchemaValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		manifest   string
		load       func(*Store) error
		want       error
		projectDir bool
	}{
		{
			name:       "project unknown field",
			manifest:   "schema_version: 1\nid: org_taskctl\nrepository: github.com/org/taskctl\ntask_prefix: TASKCTL\ncurrent_task: \"\"\nextra: true\n",
			load:       func(store *Store) error { _, err := store.LoadProject("org_taskctl"); return err },
			want:       ErrInvalid,
			projectDir: true,
		},
		{
			name:       "project unsupported schema",
			manifest:   "schema_version: 2\nid: org_taskctl\nrepository: github.com/org/taskctl\ntask_prefix: TASKCTL\ncurrent_task: \"\"\n",
			load:       func(store *Store) error { _, err := store.LoadProject("org_taskctl"); return err },
			want:       ErrUnsupportedVersion,
			projectDir: true,
		},
		{
			name:     "task unknown field",
			manifest: "schema_version: 1\nid: TASKCTL-001\ntitle: Storage\nproject_id: org_taskctl\ncreated_at: 2026-07-19T12:00:00Z\ncancelled_at: null\nprs: []\nextra: true\n",
			load:     func(store *Store) error { _, err := store.LoadTask("org_taskctl", "TASKCTL-001"); return err },
			want:     ErrInvalid,
		},
		{
			name:     "task unsupported schema",
			manifest: "schema_version: 9\nid: TASKCTL-001\ntitle: Storage\nproject_id: org_taskctl\ncreated_at: 2026-07-19T12:00:00Z\ncancelled_at: null\nprs: []\n",
			load:     func(store *Store) error { _, err := store.LoadTask("org_taskctl", "TASKCTL-001"); return err },
			want:     ErrUnsupportedVersion,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store := newTestStore(t)
			project := storeTestProject()
			if test.projectDir {
				directory := filepath.Join(store.Root(), ProjectsDirName, project.ID)
				if err := os.Mkdir(directory, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, ProjectManifestName), []byte(test.manifest), 0o644); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := store.CreateProject(project); err != nil {
					t.Fatal(err)
				}
				directory := filepath.Join(store.Root(), ProjectsDirName, project.ID, "TASKCTL-001")
				if err := os.Mkdir(directory, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, TaskManifestName), []byte(test.manifest), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := test.load(store); !errors.Is(err, test.want) {
				t.Fatalf("load error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestListTasksIsDirectDeterministicAndIgnoresTemporaryFiles(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	project := storeTestProject()
	if err := store.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	for _, id := range []domain.TaskID{"TASKCTL-1000", "TASKCTL-002", "TASKCTL-001"} {
		if err := store.CreateTask(storeTestTask(id)); err != nil {
			t.Fatal(err)
		}
	}
	projectDirectory := filepath.Join(store.Root(), ProjectsDirName, project.ID)
	if err := os.WriteFile(filepath.Join(projectDirectory, ".taskctl-interrupted"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A nested manifest must not be discovered as a fourth Task.
	nested := filepath.Join(projectDirectory, "TASKCTL-001", "nested", "TASKCTL-003")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, TaskManifestName), []byte("invalid"), 0o644); err != nil {
		t.Fatal(err)
	}

	tasks, err := store.ListTasks(project.ID)
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	got := make([]domain.TaskID, len(tasks))
	for index := range tasks {
		got[index] = tasks[index].ID
	}
	want := []domain.TaskID{"TASKCTL-001", "TASKCTL-002", "TASKCTL-1000"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListTasks() IDs = %v, want %v", got, want)
	}
}

func TestListTasksReportsDuplicateAndCorruptDirectories(t *testing.T) {
	t.Parallel()
	t.Run("duplicate manifest ID", func(t *testing.T) {
		store := newTestStore(t)
		project := storeTestProject()
		if err := store.CreateProject(project); err != nil {
			t.Fatal(err)
		}
		for _, directoryName := range []string{"TASKCTL-001", "TASKCTL-002"} {
			directory := filepath.Join(store.Root(), ProjectsDirName, project.ID, directoryName)
			if err := os.Mkdir(directory, 0o755); err != nil {
				t.Fatal(err)
			}
			contents := "schema_version: 1\nid: TASKCTL-001\ntitle: Storage\nproject_id: org_taskctl\ncreated_at: 2026-07-19T12:00:00Z\ncancelled_at: null\nprs: []\n"
			if err := os.WriteFile(filepath.Join(directory, TaskManifestName), []byte(contents), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		_, err := store.ListTasks(project.ID)
		if !errors.Is(err, ErrCorrupt) || !errors.Is(err, ErrDuplicate) {
			t.Fatalf("ListTasks() error = %v, want corrupt duplicate", err)
		}
	})

	t.Run("malformed direct directory", func(t *testing.T) {
		store := newTestStore(t)
		project := storeTestProject()
		if err := store.CreateProject(project); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(store.Root(), ProjectsDirName, project.ID, "not-a-task"), 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := store.ListTasks(project.ID)
		if !errors.Is(err, ErrCorrupt) {
			t.Fatalf("ListTasks() error = %v, want ErrCorrupt", err)
		}
	})
}

func TestStoreRejectsMissingCollidingAndUnsafePaths(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	project := storeTestProject()
	if err := store.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateProject(project); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("second CreateProject() error = %v", err)
	}
	if err := store.CreateTask(storeTestTask("TASKCTL-001")); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateTask(storeTestTask("TASKCTL-001")); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("second CreateTask() error = %v", err)
	}
	if _, err := store.LoadProject("../escape"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("LoadProject(unsafe) error = %v", err)
	}
	if _, err := store.LoadTask(project.ID, "TASKCTL-999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("LoadTask(missing) error = %v", err)
	}
	if _, err := NewStore("relative"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("NewStore(relative) error = %v", err)
	}
	outside := filepath.Join(filepath.Dir(store.Root()), "escape")
	if _, err := os.Stat(outside); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsafe load touched outside path: %v", err)
	}
}

func TestStoreRejectsSymbolicLinkEntries(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	project := storeTestProject()
	if err := store.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	link := filepath.Join(store.Root(), ProjectsDirName, project.ID, "TASKCTL-001")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}
	if _, err := store.ListTasks(project.ID); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("ListTasks() error = %v, want ErrCorrupt", err)
	}
	if err := store.SaveTask(storeTestTask("TASKCTL-001")); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("SaveTask() error = %v, want ErrCorrupt", err)
	}
	if _, err := os.Stat(filepath.Join(outside, TaskManifestName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("SaveTask() followed symbolic link: %v", err)
	}
}

func TestFindProjectByRepositoryRejectsAmbiguity(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	first := storeTestProject()
	second := first
	second.ID = "other_taskctl"
	for _, project := range []Project{first, second} {
		if err := store.CreateProject(project); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.FindProjectByRepository(first.Repository); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("FindProjectByRepository() error = %v, want ErrAmbiguous", err)
	}
}

func TestProjectValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*Project)
		want   error
	}{
		{name: "schema", mutate: func(project *Project) { project.SchemaVersion = 2 }, want: ErrUnsupportedVersion},
		{name: "unsafe ID", mutate: func(project *Project) { project.ID = "../repo" }, want: ErrInvalid},
		{name: "URL repository", mutate: func(project *Project) { project.Repository = "https://example.com/org/repo" }, want: ErrInvalid},
		{name: "prefix", mutate: func(project *Project) { project.TaskPrefix = "bad" }, want: ErrInvalid},
		{name: "current prefix", mutate: func(project *Project) { project.CurrentTask = "OTHER-001" }, want: ErrInvalid},
	}
	for _, test := range tests {
		project := storeTestProject()
		test.mutate(&project)
		if err := project.Validate(); !errors.Is(err, test.want) {
			t.Errorf("%s Validate() error = %v, want %v", test.name, err, test.want)
		}
	}
	if err := ValidateRepository("github.com/org/repo.git"); err == nil || !strings.Contains(err.Error(), ".git") {
		t.Fatalf("ValidateRepository(.git) error = %v", err)
	}
}
