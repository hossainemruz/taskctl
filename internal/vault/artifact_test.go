package vault

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArtifactRenderCreateEnsureAndLookup(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	project := storeTestProject()
	if err := store.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	task := storeTestTask("TASKCTL-001")
	data := TemplateData{TaskID: task.ID, Title: task.Title, ProjectID: task.ProjectID, CreatedAt: task.CreatedAt.Format(time.RFC3339)}
	markdown, err := store.RenderArtifact(ArtifactTask, data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(markdown), string(task.ID)) || strings.HasPrefix(string(markdown), "---") {
		t.Fatalf("rendered task.md = %q", markdown)
	}
	if err := store.CreateTaskWithMarkdown(task, markdown); err != nil {
		t.Fatal(err)
	}
	taskPath, err := store.ArtifactPath(project.ID, task.ID, ArtifactTask)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(taskPath) {
		t.Fatalf("ArtifactPath() = %q, want absolute", taskPath)
	}

	research, err := store.RenderArtifact(ArtifactResearch, data)
	if err != nil {
		t.Fatal(err)
	}
	researchPath, created, err := store.EnsureArtifact(project.ID, task.ID, ArtifactResearch, research)
	if err != nil || !created {
		t.Fatalf("EnsureArtifact() = %q, %t, %v", researchPath, created, err)
	}
	const customized = "# User content\n"
	if err := os.WriteFile(researchPath, []byte(customized), 0o644); err != nil {
		t.Fatal(err)
	}
	_, created, err = store.EnsureArtifact(project.ID, task.ID, ArtifactResearch, []byte("replacement"))
	if err != nil || created {
		t.Fatalf("second EnsureArtifact() created = %t, error = %v", created, err)
	}
	contents, err := os.ReadFile(researchPath)
	if err != nil || string(contents) != customized {
		t.Fatalf("preserved content = %q, error = %v", contents, err)
	}
	if _, err := store.ArtifactPath(project.ID, task.ID, ArtifactPlan); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing plan path error = %v", err)
	}
	if _, _, err := store.EnsureArtifact(project.ID, task.ID, ArtifactTask, nil); !errors.Is(err, ErrInvalidArtifact) {
		t.Fatalf("ensure task error = %v", err)
	}
}

func TestRenderArtifactRejectsMalformedAndSymlinkTemplates(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	templatePath := filepath.Join(store.Root(), TemplatesDirName, "research.md.tmpl")
	if err := os.WriteFile(templatePath, []byte("{{ .Missing }}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RenderArtifact(ArtifactResearch, TemplateData{}); err == nil {
		t.Fatal("RenderArtifact() accepted missing template field")
	}
	if err := os.Remove(templatePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(store.Root(), TemplatesDirName, "task.md.tmpl"), templatePath); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	if _, err := store.RenderArtifact(ArtifactResearch, TemplateData{}); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("symlink template error = %v", err)
	}
}
