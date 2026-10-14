package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"conenv/internal/conenv"
	"conenv/internal/tui"

	"github.com/charmbracelet/x/term"
)

type componentOption struct {
	componentIndex int
	name           string
	category       string
	description    string
}

type selectionTargetKind int

const (
	selectComponent selectionTargetKind = iota
	selectComponentResources
	selectSkill
)

type selectionTarget struct {
	component string
	kind      selectionTargetKind
	skill     string
}

type selectionCatalog struct {
	items   []tui.Item
	targets map[string]selectionTarget
}

func chooseComponents(input io.Reader, output io.Writer, resolved *conenv.Resolved) error {
	if canUseTUI(input, output) {
		result, err := runComponentTUI(input, output, resolved, false)
		if err != nil {
			if errors.Is(err, tui.ErrCancelled) {
				return errSelectionCancelled
			}
			return err
		}
		return filterComponents(resolved, result.SelectedIDs)
	}
	return chooseComponentsLineMode(input, output, resolved)
}

// chooseComponentsLineMode preserves interactive selection for redirected IO
// and tests, where a full-screen terminal application cannot run safely.
func chooseComponentsLineMode(input io.Reader, output io.Writer, resolved *conenv.Resolved) error {
	catalog := newSelectionCatalog(resolved)
	if len(catalog.items) == 0 {
		return errors.New("the selected profile has no components supported on this platform")
	}
	tree, err := tui.NewTree(catalog.items)
	if err != nil {
		return err
	}
	reader := bufio.NewReader(input)
	for {
		fmt.Fprintf(output, "\nSelect items for profile %q:\n", resolved.Profile)
		lastCategory := ""
		for index, item := range tree.Items() {
			depth := tree.Depth(item.ID)
			if depth == 0 && item.Category != lastCategory {
				fmt.Fprintf(output, "\n%s\n", strings.ToUpper(item.Category))
				lastCategory = item.Category
			}
			mark := " "
			switch tree.State(item.ID) {
			case tui.Selected:
				mark = "x"
			case tui.PartiallySelected:
				mark = "~"
			}
			description := ""
			if item.Description != "" {
				description = " — " + item.Description
			}
			indent := strings.Repeat("  ", depth+1)
			fmt.Fprintf(output, "%s[%s] %d. %s%s\n", indent, mark, index+1, item.Title, description)
		}
		fmt.Fprint(output, "\nToggle numbers/ranges (1,3-5), a=all, n=none, Enter=confirm, q=cancel: ")
		line, err := reader.ReadString('\n')
		if err != nil && len(line) == 0 {
			if errors.Is(err, io.EOF) {
				return errSelectionCancelled
			}
			return err
		}
		choice := strings.TrimSpace(strings.ToLower(line))
		switch choice {
		case "":
			if tree.SelectedCount() == 0 {
				fmt.Fprintln(output, "Select at least one item or enter q to cancel.")
				continue
			}
			if err := filterComponents(resolved, tree.SelectedIDs()); err != nil {
				return err
			}
			fmt.Fprintf(output, "Selected: %s\n", strings.Join(selectedTitles(tree), ", "))
			return nil
		case "q":
			return errSelectionCancelled
		case "a":
			if tree.SelectedCount() == tree.SelectableCount() {
				tree.Clear()
			} else {
				tree.SelectAll()
			}
			continue
		case "n":
			tree.Clear()
			continue
		}
		items := tree.Items()
		indices, err := parseSelection(choice, len(items))
		if err != nil {
			fmt.Fprintf(output, "Invalid selection: %v\n", err)
			continue
		}
		for _, index := range indices {
			tree.Toggle(items[index].ID)
		}
	}
}

func canUseTUI(input io.Reader, output io.Writer) bool {
	in, inputOK := input.(*os.File)
	out, outputOK := output.(*os.File)
	return inputOK && outputOK && term.IsTerminal(in.Fd()) && term.IsTerminal(out.Fd())
}

func canPrompt(input io.Reader) bool {
	file, ok := input.(*os.File)
	return ok && term.IsTerminal(file.Fd())
}

// runComponentTUI is the adapter between conenv's domain model and the
// presentation-only models exposed by internal/tui.
func runComponentTUI(input io.Reader, output io.Writer, resolved *conenv.Resolved, preview bool) (tui.Result, error) {
	catalog := newSelectionCatalog(resolved)
	config := tui.Config{
		Profile:      resolved.Profile,
		Items:        catalog.items,
		Preview:      preview,
		ConfirmLabel: "apply",
	}
	if preview {
		config.BuildPlan = func(selectedIDs []string) (tui.Plan, error) {
			selected, err := filteredResolved(resolved, selectedIDs)
			if err != nil {
				return tui.Plan{}, err
			}
			plan, err := conenv.BuildPlan(selected)
			if err != nil {
				return tui.Plan{}, err
			}
			return presentPlan(plan), nil
		}
	}
	return tui.Run(input, output, config)
}

func presentPlan(plan *conenv.Plan) tui.Plan {
	presented := tui.Plan{Profile: plan.Profile, Actions: make([]tui.Action, 0, len(plan.Actions))}
	for _, action := range plan.Actions {
		presented.Actions = append(presented.Actions, tui.Action{
			Kind:      action.Kind,
			Component: action.Component,
			Summary:   action.Summary,
		})
	}
	return presented
}

func selectionFromIDs(resolved *conenv.Resolved, selectedIDs []string) (*conenv.SelectionSpec, error) {
	catalog := newSelectionCatalog(resolved)
	targets := make([]string, 0, len(selectedIDs))
	for _, id := range selectedIDs {
		target, ok := catalog.targets[id]
		if !ok {
			return nil, fmt.Errorf("unknown TUI selection ID %q", id)
		}
		switch target.kind {
		case selectComponent:
			targets = append(targets, target.component)
		case selectComponentResources:
			targets = append(targets, target.component+"/resources")
		case selectSkill:
			targets = append(targets, target.component+"/skills/"+target.skill)
		}
	}
	return conenv.SelectionFromTargets(resolved, targets)
}

func filteredResolved(resolved *conenv.Resolved, selectedIDs []string) (*conenv.Resolved, error) {
	spec, err := selectionFromIDs(resolved, selectedIDs)
	if err != nil {
		return nil, err
	}
	return conenv.ApplySelection(resolved, spec)
}

func filterComponents(resolved *conenv.Resolved, selectedIDs []string) error {
	filtered, err := filteredResolved(resolved, selectedIDs)
	if err != nil {
		return err
	}
	resolved.Components = filtered.Components
	return nil
}

func failSelection(err error) int {
	if errors.Is(err, tui.ErrCancelled) {
		return fail(errSelectionCancelled, 1)
	}
	return fail(err, 1)
}

func componentOptions(resolved *conenv.Resolved) []componentOption {
	options := make([]componentOption, 0, len(resolved.Components))
	for _, category := range []string{"program", "ai"} {
		for componentIndex, component := range resolved.Components {
			componentCategory := component.Category
			if componentCategory == "" {
				componentCategory = "program"
				if component.Codex != nil && len(component.Files) == 0 && len(component.Packages) == 0 {
					componentCategory = "ai"
				}
			}
			if componentCategory == category {
				options = append(options, componentOption{componentIndex: componentIndex, name: component.Name, category: category, description: component.Description})
			}
		}
	}
	return options
}

func newSelectionCatalog(resolved *conenv.Resolved) selectionCatalog {
	catalog := selectionCatalog{targets: make(map[string]selectionTarget)}
	for _, option := range componentOptions(resolved) {
		component := resolved.Components[option.componentIndex]
		rootID := "component:" + component.Name
		skills := []conenv.Skill(nil)
		if component.Codex != nil {
			skills = component.Codex.Skills
		}
		catalog.items = append(catalog.items, tui.Item{
			ID: rootID, Title: option.name, Category: option.category, Description: option.description,
		})
		if len(skills) == 0 {
			catalog.targets[rootID] = selectionTarget{component: component.Name, kind: selectComponent}
			continue
		}
		if hasComponentResources(component.Component) {
			baseID := rootID + ":resources"
			catalog.items = append(catalog.items, tui.Item{
				ID: baseID, ParentID: rootID, Title: "Component resources", Description: "Packages, files, configuration, and checks",
			})
			catalog.targets[baseID] = selectionTarget{component: component.Name, kind: selectComponentResources}
		}
		for _, skill := range skills {
			skillID := fmt.Sprintf("%s:skill:%s", rootID, skill.Name)
			catalog.items = append(catalog.items, tui.Item{
				ID: skillID, ParentID: rootID, Title: skill.Name, Description: "Codex skill",
			})
			catalog.targets[skillID] = selectionTarget{component: component.Name, kind: selectSkill, skill: skill.Name}
		}
	}
	return catalog
}

func hasComponentResources(component conenv.Component) bool {
	return conenv.HasComponentResources(component)
}

func selectedTitles(tree *tui.Tree) []string {
	selected := make(map[string]bool)
	for _, id := range tree.SelectedIDs() {
		selected[id] = true
	}
	var titles []string
	for _, item := range tree.Items() {
		if selected[item.ID] {
			titles = append(titles, item.Title)
		}
	}
	return titles
}

func parseSelection(value string, maximum int) ([]int, error) {
	seen := map[int]bool{}
	var indices []int
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, errors.New("empty item")
		}
		start, end := 0, 0
		if strings.Contains(part, "-") {
			bounds := strings.Split(part, "-")
			if len(bounds) != 2 {
				return nil, fmt.Errorf("invalid range %q", part)
			}
			var err error
			start, err = strconv.Atoi(bounds[0])
			if err != nil {
				return nil, fmt.Errorf("invalid number %q", bounds[0])
			}
			end, err = strconv.Atoi(bounds[1])
			if err != nil {
				return nil, fmt.Errorf("invalid number %q", bounds[1])
			}
		} else {
			var err error
			start, err = strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid number %q", part)
			}
			end = start
		}
		if start < 1 || end > maximum || start > end {
			return nil, fmt.Errorf("selection %q is outside 1-%d", part, maximum)
		}
		for index := start - 1; index < end; index++ {
			if !seen[index] {
				indices = append(indices, index)
				seen[index] = true
			}
		}
	}
	return indices, nil
}
