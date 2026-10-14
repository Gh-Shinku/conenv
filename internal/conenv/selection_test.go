package conenv

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func selectionTestRepository() *Resolved {
	return &Resolved{Profile: "personal", Components: []NamedComponent{
		{Name: "git", Component: Component{Files: []ManagedFile{{Source: "gitconfig", Target: "C:/gitconfig", Mode: "link"}}}},
		{Name: "ai-agents", Component: Component{Agents: map[string]Agent{
			"codex": {
				Name: "codex", Harness: "codex",
				Config: map[string]any{"model": "example"},
				Skills: []Skill{{Name: "project-bootstrap", Source: "project"}, {Name: "vibe-bootstrap", Source: "vibe"}},
			},
		}}},
	}}
}

func TestSelectionRoundTripAndApply(t *testing.T) {
	resolved := selectionTestRepository()
	spec, err := SelectionFromTargets(resolved, []string{"git", "ai-agents/agents/codex/skills/vibe-bootstrap"})
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
	if len(filtered.Components) != 2 {
		t.Fatalf("filtered profile = %#v", filtered.Components)
	}
	agent, ok := filtered.Components[1].Agents["codex"]
	if !ok {
		t.Fatalf("agent was dropped by the selection: %#v", filtered.Components[1])
	}
	if len(agent.Skills) != 1 || agent.Skills[0].Name != "vibe-bootstrap" {
		t.Fatalf("skill-only selection = %#v", agent.Skills)
	}
	if len(agent.Config) != 0 {
		t.Fatalf("skill-only selection retained agent resources: %#v", agent.Config)
	}
}

func TestSelectionSelectsWholeComponentAndAgent(t *testing.T) {
	resolved := selectionTestRepository()
	spec, err := SelectionFromTargets(resolved, []string{"ai-agents"})
	if err != nil {
		t.Fatal(err)
	}
	if selected := spec.Components["ai-agents"]; !selected.Agents["codex"].Resources || len(selected.Agents["codex"].Skills) != 2 {
		t.Fatalf("whole component selection = %#v", selected)
	}

	spec, err = SelectionFromTargets(resolved, []string{"ai-agents/agents/codex"})
	if err != nil {
		t.Fatal(err)
	}
	if selected := spec.Components["ai-agents"]; !selected.Agents["codex"].Resources || len(selected.Agents["codex"].Skills) != 2 {
		t.Fatalf("whole agent selection = %#v", selected)
	}

	spec, err = SelectionFromTargets(resolved, []string{"ai-agents/agents/codex/resources"})
	if err != nil {
		t.Fatal(err)
	}
	selected := spec.Components["ai-agents"]
	if !selected.Agents["codex"].Resources || len(selected.Agents["codex"].Skills) != 0 {
		t.Fatalf("resource-only agent selection = %#v", selected)
	}
}

func TestSelectionRejectsUnknownTargets(t *testing.T) {
	resolved := selectionTestRepository()
	for _, target := range []string{
		"missing",
		"ai-agents/skills/vibe-bootstrap",
		"ai-agents/agents/claude",
		"ai-agents/agents/codex/skills/missing",
		"ai-agents/agents/codex/mcpServers/x",
	} {
		if _, err := SelectionFromTargets(resolved, []string{target}); err == nil {
			t.Fatalf("expected target %q to fail", target)
		}
	}
}

func TestSelectionRejectsUnknownAndDuplicateSkills(t *testing.T) {
	resolved := selectionTestRepository()
	for _, spec := range []*SelectionSpec{
		{SchemaVersion: SelectionSchemaVersion, Profile: "other", Components: map[string]ComponentSelection{"ai-agents": {Agents: map[string]ComponentSelection{"codex": {Skills: []string{"project-bootstrap"}}}}}},
		{SchemaVersion: SelectionSchemaVersion, Profile: "personal", Components: map[string]ComponentSelection{"missing": {Resources: true}}},
		{SchemaVersion: SelectionSchemaVersion, Profile: "personal", Components: map[string]ComponentSelection{"ai-agents": {Agents: map[string]ComponentSelection{"codex": {Skills: []string{"project-bootstrap", "project-bootstrap"}}}}}},
		{SchemaVersion: SelectionSchemaVersion, Profile: "personal", Components: map[string]ComponentSelection{"ai-agents": {Agents: map[string]ComponentSelection{"codex": {Skills: []string{"missing"}}}}}},
		{SchemaVersion: SelectionSchemaVersion, Profile: "personal", Components: map[string]ComponentSelection{"ai-agents": {Agents: map[string]ComponentSelection{"claude": {Resources: true}}}}},
		{SchemaVersion: SelectionSchemaVersion, Profile: "personal", Components: map[string]ComponentSelection{"ai-agents": {}}},
	} {
		if _, err := ApplySelection(resolved, spec); err == nil {
			t.Fatalf("expected invalid selection to fail: %#v", spec)
		}
	}
}

func TestSelectionAgentResourcesSelectsConfigOnly(t *testing.T) {
	resolved := selectionTestRepository()
	spec := &SelectionSpec{
		SchemaVersion: SelectionSchemaVersion, Profile: "personal",
		Components: map[string]ComponentSelection{"ai-agents": {Agents: map[string]ComponentSelection{"codex": {Resources: true}}}},
	}
	filtered, err := ApplySelection(resolved, spec)
	if err != nil {
		t.Fatal(err)
	}
	agent := filtered.Components[0].Agents["codex"]
	if len(agent.Config) == 0 || len(agent.Skills) != 0 {
		t.Fatalf("resource-only selection = %#v", agent)
	}
}

func TestHasComponentResourcesIgnoresAgents(t *testing.T) {
	component := Component{Agents: map[string]Agent{"codex": {Name: "codex", Harness: "codex", Skills: []Skill{{Name: "one", Source: "one"}}}}}
	if HasComponentResources(component) {
		t.Fatal("agent declarations are not component resources")
	}
}

func TestNarrowedScopeSkipsUnselectedRuntimePrerequisites(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "selected"), []byte("selected"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := `schemaVersion: conenv/v2
defaultProfile: personal
secrets:
  unused:
    env: CONENV_UNSET_SELECTION_TEST
profiles:
  personal:
    components: [good, broken]
components:
  good:
    files:
      - source: selected
        target: '{{ .home }}/selected'
        mode: link
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
