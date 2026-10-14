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

func TestChooseComponentsGroupsAndFilters(t *testing.T) {
	resolved := &conenv.Resolved{Profile: "personal", Components: []conenv.NamedComponent{
		{Name: "skills", Component: conenv.Component{Category: "ai", Description: "Shared skills", Codex: &conenv.Codex{}}},
		{Name: "shell", Component: conenv.Component{Category: "program", Description: "Shell config"}},
		{Name: "vim", Component: conenv.Component{Category: "program"}},
	}}
	var output bytes.Buffer
	if err := chooseComponents(strings.NewReader("1,3\n\n"), &output, resolved); err != nil {
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
		{Name: "skills"},
		{Name: "shell"},
		{Name: "vim"},
	}}
	filtered := filteredResolved(resolved, []string{"vim", "skills"})
	if got := []string{filtered.Components[0].Name, filtered.Components[1].Name}; !reflect.DeepEqual(got, []string{"skills", "vim"}) {
		t.Fatalf("filtered order = %v", got)
	}
	if len(resolved.Components) != 3 {
		t.Fatal("filtering a copy should not mutate the resolved profile")
	}
}
