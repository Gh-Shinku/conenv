package conenv

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSelectionRoundTripAndApply(t *testing.T) {
	resolved := &Resolved{Profile: "personal", Components: []NamedComponent{
		{Name: "git", Component: Component{Files: []ManagedFile{{Source: "gitconfig", Target: "C:/gitconfig", Mode: "link"}}}},
		{Name: "codex", Component: Component{Codex: &Codex{
			Config: map[string]any{"model": "example"},
			Skills: []Skill{{Name: "project-bootstrap", Source: "project"}, {Name: "vibe-bootstrap", Source: "vibe"}},
		}}},
	}}
	spec, err := SelectionFromTargets(resolved, []string{"git", "codex/skills/vibe-bootstrap"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "selection.yaml")
	if err := SaveSelection(path, spec); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSelection(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, spec) {
		t.Fatalf("loaded selection = %#v, want %#v", loaded, spec)
	}
	filtered, err := ApplySelection(resolved, loaded)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Components) != 2 || len(filtered.Components[1].Codex.Skills) != 1 || filtered.Components[1].Codex.Skills[0].Name != "vibe-bootstrap" {
		t.Fatalf("filtered profile = %#v", filtered.Components)
	}
	if len(filtered.Components[1].Codex.Config) != 0 {
		t.Fatalf("skill-only selection retained component resources: %#v", filtered.Components[1].Codex.Config)
	}
}

func TestSelectionRejectsUnknownAndDuplicateSkills(t *testing.T) {
	resolved := &Resolved{Profile: "personal", Components: []NamedComponent{{
		Name: "codex", Component: Component{Codex: &Codex{Skills: []Skill{{Name: "one", Source: "one"}}}},
	}}}
	for _, spec := range []*SelectionSpec{
		{SchemaVersion: SelectionSchemaVersion, Profile: "other", Components: map[string]ComponentSelection{"codex": {Skills: []string{"one"}}}},
		{SchemaVersion: SelectionSchemaVersion, Profile: "personal", Components: map[string]ComponentSelection{"missing": {Resources: true}}},
		{SchemaVersion: SelectionSchemaVersion, Profile: "personal", Components: map[string]ComponentSelection{"codex": {Skills: []string{"one", "one"}}}},
		{SchemaVersion: SelectionSchemaVersion, Profile: "personal", Components: map[string]ComponentSelection{"codex": {Skills: []string{"missing"}}}},
	} {
		if _, err := ApplySelection(resolved, spec); err == nil {
			t.Fatalf("expected invalid selection to fail: %#v", spec)
		}
	}
}

func TestNarrowedScopeSkipsUnselectedRuntimePrerequisites(t *testing.T) {
	root := t.TempDir()
	manifest := `schemaVersion: conenv/v1
defaultProfile: personal
secrets:
  unused:
    env: CONENV_UNSET_SELECTION_TEST
profiles:
  personal:
    components: [good, broken]
components:
  good: {}
  broken:
    files:
      - source: missing-file
        target: '{{ .home }}/missing-file'
        mode: link
`
	if err := os.WriteFile(filepath.Join(root, "conenv.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, ""); err == nil {
		t.Fatal("full profile validation should fail")
	}
	resolved, err := LoadProfile(root, "")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := SelectionFromTargets(resolved, []string{"good"})
	if err != nil {
		t.Fatal(err)
	}
	selected, err := ApplySelection(resolved, spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateScope(selected); err != nil {
		t.Fatalf("unselected prerequisites blocked validation: %v", err)
	}
}

func TestLoadSelectionRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selection.yaml")
	data := "schemaVersion: conenv/selection/v1\nprofile: personal\ncomponents:\n  git:\n    resources: true\n    surprise: true\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSelection(path); err == nil || !strings.Contains(err.Error(), "surprise") {
		t.Fatalf("expected unknown-field error, got %v", err)
	}
}
