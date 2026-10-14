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
	name        string
	category    string
	description string
}

func chooseComponents(input io.Reader, output io.Writer, resolved *conenv.Resolved) error {
	if canUseTUI(input, output) {
		result, err := runComponentTUI(input, output, resolved, false)
		if err != nil {
			return err
		}
		filterComponents(resolved, result.SelectedIDs)
		return nil
	}
	return chooseComponentsLineMode(input, output, resolved)
}

// chooseComponentsLineMode preserves interactive selection for redirected IO
// and tests, where a full-screen terminal application cannot run safely.
func chooseComponentsLineMode(input io.Reader, output io.Writer, resolved *conenv.Resolved) error {
	options := componentOptions(resolved)
	if len(options) == 0 {
		return errors.New("the selected profile has no components supported on this platform")
	}
	selected := map[string]bool{}
	reader := bufio.NewReader(input)
	for {
		fmt.Fprintf(output, "\nSelect components for profile %q:\n", resolved.Profile)
		lastCategory := ""
		for index, option := range options {
			if option.category != lastCategory {
				fmt.Fprintf(output, "\n%s\n", strings.ToUpper(option.category))
				lastCategory = option.category
			}
			mark := " "
			if selected[option.name] {
				mark = "x"
			}
			description := ""
			if option.description != "" {
				description = " — " + option.description
			}
			fmt.Fprintf(output, "  [%s] %d. %s%s\n", mark, index+1, option.name, description)
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
			if len(selected) == 0 {
				fmt.Fprintln(output, "Select at least one component or enter q to cancel.")
				continue
			}
			filtered := resolved.Components[:0]
			var names []string
			for _, component := range resolved.Components {
				if selected[component.Name] {
					filtered = append(filtered, component)
					names = append(names, component.Name)
				}
			}
			resolved.Components = filtered
			fmt.Fprintf(output, "Selected: %s\n", strings.Join(names, ", "))
			return nil
		case "q":
			return errSelectionCancelled
		case "a":
			selectAll := len(selected) != len(options)
			clear(selected)
			if selectAll {
				for _, option := range options {
					selected[option.name] = true
				}
			}
			continue
		case "n":
			clear(selected)
			continue
		}
		indices, err := parseSelection(choice, len(options))
		if err != nil {
			fmt.Fprintf(output, "Invalid selection: %v\n", err)
			continue
		}
		for _, index := range indices {
			name := options[index].name
			selected[name] = !selected[name]
			if !selected[name] {
				delete(selected, name)
			}
		}
	}
}

func canUseTUI(input io.Reader, output io.Writer) bool {
	in, inputOK := input.(*os.File)
	out, outputOK := output.(*os.File)
	return inputOK && outputOK && term.IsTerminal(in.Fd()) && term.IsTerminal(out.Fd())
}

// runComponentTUI is the adapter between conenv's domain model and the
// presentation-only models exposed by internal/tui.
func runComponentTUI(input io.Reader, output io.Writer, resolved *conenv.Resolved, preview bool) (tui.Result, error) {
	options := componentOptions(resolved)
	components := make([]tui.Component, 0, len(options))
	for _, option := range options {
		components = append(components, tui.Component{
			ID:          option.name,
			Title:       option.name,
			Category:    option.category,
			Description: option.description,
		})
	}
	config := tui.Config{
		Profile:      resolved.Profile,
		Components:   components,
		Preview:      preview,
		ConfirmLabel: "apply",
	}
	if preview {
		config.BuildPlan = func(selectedIDs []string) (tui.Plan, error) {
			selected := filteredResolved(resolved, selectedIDs)
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

func filteredResolved(resolved *conenv.Resolved, selectedIDs []string) *conenv.Resolved {
	copyResolved := *resolved
	selected := make(map[string]bool, len(selectedIDs))
	for _, id := range selectedIDs {
		selected[id] = true
	}
	copyResolved.Components = make([]conenv.NamedComponent, 0, len(selectedIDs))
	for _, component := range resolved.Components {
		if selected[component.Name] {
			copyResolved.Components = append(copyResolved.Components, component)
		}
	}
	return &copyResolved
}

func filterComponents(resolved *conenv.Resolved, selectedIDs []string) {
	resolved.Components = filteredResolved(resolved, selectedIDs).Components
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
		for _, component := range resolved.Components {
			componentCategory := component.Category
			if componentCategory == "" {
				componentCategory = "program"
				if component.Codex != nil && len(component.Files) == 0 && len(component.Packages) == 0 {
					componentCategory = "ai"
				}
			}
			if componentCategory == category {
				options = append(options, componentOption{name: component.Name, category: category, description: component.Description})
			}
		}
	}
	return options
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
