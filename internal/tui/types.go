package tui

import "errors"

var ErrCancelled = errors.New("interactive workflow cancelled")

// Item is one node in the selector tree. ParentID is empty for a root item.
// It deliberately contains no conenv domain types so the TUI can evolve
// independently.
type Item struct {
	ID          string
	ParentID    string
	Category    string
	Title       string
	Description string
}

// Action is the presentation model for one planned change.
type Action struct {
	Kind      string
	Component string
	Summary   string
}

// Plan is the presentation model rendered by the preview screen.
type Plan struct {
	Profile string
	Actions []Action
}

type PlanBuilder func(selectedIDs []string) (Plan, error)

type Config struct {
	Profile      string
	Items        []Item
	Preview      bool
	BuildPlan    PlanBuilder
	ConfirmLabel string
}

type Result struct {
	SelectedIDs []string
	Plan        Plan
	Confirmed   bool
}
