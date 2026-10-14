package tui

import (
	"reflect"
	"strings"
	"testing"
)

func testItems() []Item {
	return []Item{
		{ID: "shell", Category: "program", Title: "shell", Description: "Shell config"},
		{ID: "vim", Category: "program", Title: "vim"},
		{ID: "skills", Category: "ai", Title: "skills", Description: "Shared skills"},
		{ID: "skills/project", ParentID: "skills", Title: "project-bootstrap"},
		{ID: "skills/creator", ParentID: "skills", Title: "skill-creator"},
	}
}

func TestSelectorDefaultsToAllAndSupportsFiltering(t *testing.T) {
	selector, err := newSelector(testItems())
	if err != nil {
		t.Fatal(err)
	}
	if got := selector.selectedIDs(); !reflect.DeepEqual(got, []string{"shell", "vim", "skills/project", "skills/creator"}) {
		t.Fatalf("initial selection = %v", got)
	}

	selector.handleKey("/")
	for _, key := range []string{"s", "k", "i", "l", "l", "s"} {
		selector.handleKey(key)
	}
	selector.handleKey("enter")
	selector.handleKey("space")
	if got := selector.selectedIDs(); !reflect.DeepEqual(got, []string{"shell", "vim"}) {
		t.Fatalf("filtered group toggle selection = %v", got)
	}
}

func TestWorkflowSelectPreviewBackAndConfirm(t *testing.T) {
	var builtWith []string
	model, err := newWorkflow(Config{
		Profile: "personal",
		Items:   testItems(),
		Preview: true,
		BuildPlan: func(selected []string) (Plan, error) {
			builtWith = append([]string(nil), selected...)
			return Plan{Profile: "personal", Actions: []Action{{Kind: "render", Component: "shell", Summary: "render profile"}}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	model, _ = model.updateKey("space")
	model, _ = model.updateKey("enter")
	wantSelection := []string{"vim", "skills/project", "skills/creator"}
	if model.screen != previewScreen || !reflect.DeepEqual(builtWith, wantSelection) {
		t.Fatalf("preview state = %v, built with %v", model.screen, builtWith)
	}
	if !strings.Contains(model.preview.view(), "render profile") {
		t.Fatalf("preview did not render action: %s", model.preview.view())
	}

	model, _ = model.updateKey("esc")
	if model.screen != selectScreen {
		t.Fatal("escape should return to selection")
	}
	model, _ = model.updateKey("enter")
	model, _ = model.updateKey("enter")
	if !model.result.Confirmed {
		t.Fatal("enter on preview should confirm")
	}
}

func TestSelectorRequiresOneItem(t *testing.T) {
	selector, err := newSelector(testItems())
	if err != nil {
		t.Fatal(err)
	}
	selector.handleKey("n")
	if event := selector.handleKey("enter"); event != selectorNone {
		t.Fatalf("empty selection event = %v", event)
	}
	if selector.errorText == "" {
		t.Fatal("expected validation message")
	}
}

func TestControlCCancelsWhileFiltering(t *testing.T) {
	model, err := newWorkflow(Config{Profile: "personal", Items: testItems()})
	if err != nil {
		t.Fatal(err)
	}
	model, _ = model.updateKey("/")
	model, _ = model.updateKey("ctrl+c")
	if !model.cancelled {
		t.Fatal("ctrl+c should cancel even while the filter is active")
	}
}
