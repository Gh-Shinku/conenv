package tui

import (
	"reflect"
	"testing"
)

func TestTreeDerivesParentStateAndTogglesDescendants(t *testing.T) {
	tree, err := NewTree([]Item{
		{ID: "skills", Title: "skills"},
		{ID: "skills/one", ParentID: "skills", Title: "one"},
		{ID: "skills/two", ParentID: "skills", Title: "two"},
	})
	if err != nil {
		t.Fatal(err)
	}
	tree.Toggle("skills/one")
	if state := tree.State("skills"); state != PartiallySelected {
		t.Fatalf("parent state = %v", state)
	}
	if got := tree.SelectedIDs(); !reflect.DeepEqual(got, []string{"skills/two"}) {
		t.Fatalf("selected IDs = %v", got)
	}

	tree.Toggle("skills")
	if state := tree.State("skills"); state != Selected {
		t.Fatalf("parent state after select = %v", state)
	}
	tree.Toggle("skills")
	if state := tree.State("skills"); state != Unselected {
		t.Fatalf("parent state after clear = %v", state)
	}
}

func TestTreeFilterIncludesAncestorsAndDescendants(t *testing.T) {
	tree, err := NewTree([]Item{
		{ID: "shell", Title: "shell"},
		{ID: "skills", Title: "codex skills"},
		{ID: "skills/one", ParentID: "skills", Title: "project bootstrap"},
		{ID: "skills/two", ParentID: "skills", Title: "vibe bootstrap"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := tree.FilteredIndices("vibe"); !reflect.DeepEqual(got, []int{1, 3}) {
		t.Fatalf("child filter = %v", got)
	}
	if got := tree.FilteredIndices("codex"); !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Fatalf("parent filter = %v", got)
	}
}
