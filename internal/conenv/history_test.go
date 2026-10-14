package conenv

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSavedSelectionRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	if spec, err := LoadDefaultSelection(); err != nil {
		t.Fatal(err)
	} else if spec != nil {
		t.Fatalf("expected no saved selection, got %#v", spec)
	}

	want := &SelectionSpec{
		SchemaVersion: SelectionSchemaVersion,
		Profile:       "base",
		Components:    map[string]ComponentSelection{"shell": {Resources: true}},
	}
	path, err := SaveDefaultSelection(want)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("saved selection path %s: %v", path, err)
	}
	got, err := LoadDefaultSelection()
	if err != nil {
		t.Fatal(err)
	}
	if got.Profile != want.Profile || !got.Components["shell"].Resources {
		t.Fatalf("round trip changed the selection: %#v", got)
	}

	if err := ClearDefaultSelection(); err != nil {
		t.Fatal(err)
	}
	if spec, err := LoadDefaultSelection(); err != nil || spec != nil {
		t.Fatalf("selection was not cleared: %#v, %v", spec, err)
	}
	// Clearing an already cleared selection is not an error.
	if err := ClearDefaultSelection(); err != nil {
		t.Fatal(err)
	}
}

func TestLatestRunID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	if _, err := LatestRunID(); err == nil {
		t.Fatal("expected no runs to be an error")
	}

	older := RunRecord{ID: "run-1", Profile: "base", CreatedAt: time.Now().UTC()}
	newer := RunRecord{ID: "run-2", Profile: "base", CreatedAt: time.Now().UTC()}
	if err := saveState(home, State{LastRun: newer.ID, Runs: []RunRecord{older, newer}}); err != nil {
		t.Fatal(err)
	}
	id, err := LatestRunID()
	if err != nil {
		t.Fatal(err)
	}
	if id != newer.ID {
		t.Fatalf("latest run = %q, want %q", id, newer.ID)
	}
}

// copyEntry backs up a managed directory that contains linked skills, so a
// directory symlink must be preserved instead of followed.
func TestCopyEntryPreservesNestedDirectorySymlink(t *testing.T) {
	root := t.TempDir()
	linked := filepath.Join(root, "linked")
	if err := os.MkdirAll(linked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linked, "SKILL.md"), []byte("skill"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(linked, filepath.Join(source, "skill")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	target := filepath.Join(root, "backup")
	if err := copyEntry(source, target); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(target, "skill"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("nested directory symlink became a %v entry", info.Mode())
	}
	assertFileContent(t, filepath.Join(linked, "SKILL.md"), "skill")
}
