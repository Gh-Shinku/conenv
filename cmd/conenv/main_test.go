package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"conenv/internal/conenv"
)

func TestParseSelection(t *testing.T) {
	indices, err := parseSelection("1,3-5,3", 5)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{0, 2, 3, 4}
	if !reflect.DeepEqual(indices, want) {
		t.Fatalf("got %v, want %v", indices, want)
	}
	if _, err := parseSelection("2-6", 5); err == nil {
		t.Fatal("expected out-of-range selection to fail")
	}
}

func TestParseRestoreOptions(t *testing.T) {
	options, err := parseRestore([]string{"run-1", "--dry-run", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if options.runID != "run-1" || !options.dryRun || !options.json || options.yes {
		t.Fatalf("unexpected restore options: %#v", options)
	}
	if _, err := parseRestore([]string{"--dry-run"}); err == nil {
		t.Fatal("expected missing run ID to fail")
	}
}

func TestChooseComponentsGroupsAndFilters(t *testing.T) {
	resolved := &conenv.Resolved{Profile: "personal", Components: []conenv.NamedComponent{
		{Name: "skills", Component: conenv.Component{Category: "ai", Description: "Shared skills", Codex: &conenv.Codex{}}},
		{Name: "shell", Component: conenv.Component{Category: "program", Description: "Shell config"}},
		{Name: "vim", Component: conenv.Component{Category: "program"}},
	}}
	var output bytes.Buffer
	if err := chooseComponents(strings.NewReader("n\n1,3\n\n"), &output, resolved); err != nil {
		t.Fatal(err)
	}
	if len(resolved.Components) != 2 || resolved.Components[0].Name != "skills" || resolved.Components[1].Name != "shell" {
		t.Fatalf("unexpected components: %#v", resolved.Components)
	}
	text := output.String()
	if !strings.Contains(text, "PROGRAM") || !strings.Contains(text, "AI") || !strings.Contains(text, "[x]") {
		t.Fatalf("unexpected menu output: %s", text)
	}
}

func TestFilteredResolvedPreservesManifestOrder(t *testing.T) {
	resolved := &conenv.Resolved{Profile: "personal", Components: []conenv.NamedComponent{
		{Name: "skills", Component: conenv.Component{Codex: &conenv.Codex{Skills: []conenv.Skill{
			{Name: "project-bootstrap", Source: "project"},
			{Name: "vibe-bootstrap", Source: "vibe"},
		}}}},
		{Name: "shell"},
		{Name: "vim"},
	}}
	catalog := newSelectionCatalog(resolved)
	var selected []string
	for _, item := range catalog.items {
		if item.Title == "vibe-bootstrap" || item.Title == "shell" {
			selected = append(selected, item.ID)
		}
	}
	filtered, err := filteredResolved(resolved, selected)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{filtered.Components[0].Name, filtered.Components[1].Name}; !reflect.DeepEqual(got, []string{"skills", "shell"}) {
		t.Fatalf("filtered order = %v", got)
	}
	if got := filtered.Components[0].Codex.Skills; len(got) != 1 || got[0].Name != "vibe-bootstrap" {
		t.Fatalf("filtered skills = %#v", got)
	}
	if len(resolved.Components) != 3 {
		t.Fatal("filtering a copy should not mutate the resolved profile")
	}
}

func TestChooseComponentsAllowsIndividualSkill(t *testing.T) {
	resolved := &conenv.Resolved{Profile: "personal", Components: []conenv.NamedComponent{
		{Name: "codex-skills", Component: conenv.Component{Category: "ai", Codex: &conenv.Codex{Skills: []conenv.Skill{
			{Name: "project-bootstrap", Source: "project"},
			{Name: "vibe-bootstrap", Source: "vibe"},
		}}}},
	}}
	var output bytes.Buffer
	if err := chooseComponents(strings.NewReader("2\n\n"), &output, resolved); err != nil {
		t.Fatal(err)
	}
	if got := resolved.Components[0].Codex.Skills; len(got) != 1 || got[0].Name != "vibe-bootstrap" {
		t.Fatalf("selected skills = %#v", got)
	}
	if !strings.Contains(output.String(), "[~] 1. codex-skills") {
		t.Fatalf("parent did not render a partial state: %s", output.String())
	}
}

func TestFilteredResolvedSeparatesResourcesFromSkills(t *testing.T) {
	component := conenv.NamedComponent{Name: "mixed", Component: conenv.Component{
		Packages: []conenv.Package{{Manager: "winget", ID: "Example.Tool"}},
		Checks:   []conenv.Check{{Name: "tool", Type: "command", Command: "tool"}},
		Codex: &conenv.Codex{
			Config: map[string]any{"model": "example"},
			MCPServers: map[string]conenv.MCP{
				"docs": {Type: "http", URL: "https://example.com"},
			},
			Skills: []conenv.Skill{{Name: "one", Source: "one"}, {Name: "two", Source: "two"}},
		},
	}}
	resolved := &conenv.Resolved{Profile: "personal", Components: []conenv.NamedComponent{component}}
	catalog := newSelectionCatalog(resolved)
	idsByTitle := map[string]string{}
	for _, item := range catalog.items {
		idsByTitle[item.Title] = item.ID
	}

	skillFiltered, err := filteredResolved(resolved, []string{idsByTitle["two"]})
	if err != nil {
		t.Fatal(err)
	}
	skillOnly := skillFiltered.Components[0]
	if len(skillOnly.Packages) != 0 || len(skillOnly.Checks) != 0 || len(skillOnly.Codex.Config) != 0 || len(skillOnly.Codex.MCPServers) != 0 {
		t.Fatalf("skill-only selection retained component resources: %#v", skillOnly)
	}
	if len(skillOnly.Codex.Skills) != 1 || skillOnly.Codex.Skills[0].Name != "two" {
		t.Fatalf("skill-only selection = %#v", skillOnly.Codex.Skills)
	}

	resourcesFiltered, err := filteredResolved(resolved, []string{idsByTitle["Component resources"]})
	if err != nil {
		t.Fatal(err)
	}
	resourcesOnly := resourcesFiltered.Components[0]
	if len(resourcesOnly.Packages) != 1 || len(resourcesOnly.Checks) != 1 || len(resourcesOnly.Codex.Config) != 1 || len(resourcesOnly.Codex.MCPServers) != 1 {
		t.Fatalf("resource selection lost component resources: %#v", resourcesOnly)
	}
	if len(resourcesOnly.Codex.Skills) != 0 {
		t.Fatalf("resource selection retained skills: %#v", resourcesOnly.Codex.Skills)
	}
}

func TestTUISelectionMatchesDeclarativeSelection(t *testing.T) {
	resolved := &conenv.Resolved{Profile: "personal", Components: []conenv.NamedComponent{{
		Name: "codex", Component: conenv.Component{Codex: &conenv.Codex{
			Config: map[string]any{"model": "example"},
			Skills: []conenv.Skill{{Name: "one", Source: "one"}, {Name: "two", Source: "two"}},
		}},
	}}}
	catalog := newSelectionCatalog(resolved)
	var selectedIDs []string
	for _, item := range catalog.items {
		if item.Title == "Component resources" || item.Title == "two" {
			selectedIDs = append(selectedIDs, item.ID)
		}
	}
	fromTUI, err := filteredResolved(resolved, selectedIDs)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := conenv.SelectionFromTargets(resolved, []string{"codex/resources", "codex/skills/two"})
	if err != nil {
		t.Fatal(err)
	}
	fromFile, err := conenv.ApplySelection(resolved, spec)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fromTUI.Components, fromFile.Components) {
		t.Fatalf("TUI selection %#v differs from declarative selection %#v", fromTUI.Components, fromFile.Components)
	}
}
