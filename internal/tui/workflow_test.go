package tui

import (
	"reflect"
	"strings"
	"testing"
)

func testComponents() []Component {
	return []Component{
		{ID: "shell", Category: "program", Title: "shell", Description: "Shell config"},
		{ID: "vim", Category: "program", Title: "vim"},
		{ID: "skills", Category: "ai", Title: "skills", Description: "Shared skills"},
	}
}

func TestSelectorDefaultsToAllAndSupportsFiltering(t *testing.T) {
	selector := newSelector(testComponents())
	if got := selector.selectedIDs(); !reflect.DeepEqual(got, []string{"shell", "vim", "skills"}) {
		t.Fatalf("initial selection = %v", got)
	}

	selector.handleKey("/")
	selector.handleKey("s")
	selector.handleKey("k")
	selector.handleKey("i")
	selector.handleKey("l")
	selector.handleKey("l")
	selector.handleKey("s")
	selector.handleKey("enter")
	selector.handleKey("space")
	if got := selector.selectedIDs(); !reflect.DeepEqual(got, []string{"shell", "vim"}) {
		t.Fatalf("filtered toggle selection = %v", got)
	}
}

func TestWorkflowSelectPreviewBackAndConfirm(t *testing.T) {
	var builtWith []string
	model := newWorkflow(Config{
		Profile:    "personal",
		Components: testComponents(),
		Preview:    true,
		BuildPlan: func(selected []string) (Plan, error) {
			builtWith = append([]string(nil), selected...)
			return Plan{Profile: "personal", Actions: []Action{{Kind: "render", Component: "shell", Summary: "render profile"}}}, nil
		},
	})

	model, _ = model.updateKey("space")
	model, _ = model.updateKey("enter")
	if model.screen != previewScreen || !reflect.DeepEqual(builtWith, []string{"vim", "skills"}) {
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

func TestSelectorRequiresOneComponent(t *testing.T) {
	selector := newSelector(testComponents())
	selector.handleKey("n")
	if event := selector.handleKey("enter"); event != selectorNone {
		t.Fatalf("empty selection event = %v", event)
	}
	if selector.errorText == "" {
		t.Fatal("expected validation message")
	}
}

func TestControlCCancelsWhileFiltering(t *testing.T) {
	model := newWorkflow(Config{Profile: "personal", Components: testComponents()})
	model, _ = model.updateKey("/")
	model, _ = model.updateKey("ctrl+c")
	if !model.cancelled {
		t.Fatal("ctrl+c should cancel even while the filter is active")
	}
}
