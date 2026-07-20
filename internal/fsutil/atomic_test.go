package fsutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtomicWriteFileReplacesContentAndMode(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "state.yaml")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteFile(path, []byte("new"), 0o644); err != nil {
		t.Fatalf("AtomicWriteFile() error = %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "new" {
		t.Fatalf("contents = %q, want new", contents)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %#o, want 0644", info.Mode().Perm())
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.yaml" {
		t.Fatalf("directory entries = %v, want only state.yaml", entries)
	}
}

func TestAtomicWriteFileCreateFailureCreatesNothing(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	missingParentPath := filepath.Join(directory, "missing", "state.yaml")
	err := AtomicWriteFile(missingParentPath, []byte("replacement"), 0o644)
	if err == nil || !strings.Contains(err.Error(), "create temporary file") {
		t.Fatalf("AtomicWriteFile() error = %v, want temporary-file creation failure", err)
	}
	if _, statErr := os.Stat(filepath.Dir(missingParentPath)); !os.IsNotExist(statErr) {
		t.Fatalf("create failure left parent state: %v", statErr)
	}
}

func TestAtomicWriteFileRenameFailureCleansTemporaryFile(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	destination := filepath.Join(directory, "state.yaml")
	if err := os.Mkdir(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(destination, "keep")
	if err := os.WriteFile(sentinel, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := AtomicWriteFile(destination, []byte("replacement"), 0o644)
	if err == nil || !strings.Contains(err.Error(), "replace destination") {
		t.Fatalf("AtomicWriteFile() error = %v, want rename failure", err)
	}
	contents, readErr := os.ReadFile(sentinel)
	if readErr != nil || string(contents) != "original" {
		t.Fatalf("destination after failure = %q, error = %v", contents, readErr)
	}
	entries, readDirErr := os.ReadDir(directory)
	if readDirErr != nil {
		t.Fatal(readDirErr)
	}
	if len(entries) != 1 || entries[0].Name() != "state.yaml" || !entries[0].IsDir() {
		t.Fatalf("directory entries after failure = %v, want only original destination", entries)
	}
}
