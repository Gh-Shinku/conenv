package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"conenv/internal/assets"
	"conenv/internal/conenv"
)

var version = "dev"

type commonFlags struct {
	repo             string
	root             string
	profile          string
	json             bool
	selectComponents bool
}

var errSelectionCancelled = errors.New("component selection cancelled")

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	switch args[0] {
	case "validate":
		common, extra, err := parseCommon("validate", args[1:], false, false)
		if err != nil {
			return fail(err, 2)
		}
		if len(extra) != 0 {
			return fail(errors.New("validate does not accept positional arguments"), 2)
		}
		resolved, err := loadResolved(common)
		if err != nil {
			return fail(err, 2)
		}
		fmt.Printf("valid profile %q with %d component(s)\n", resolved.Profile, len(resolved.Components))
		return 0
	case "plan", "status":
		common, extra, err := parseCommon(args[0], args[1:], true, true)
		if err != nil {
			return fail(err, 2)
		}
		if len(extra) != 0 {
			return fail(fmt.Errorf("%s does not accept positional arguments", args[0]), 2)
		}
		resolved, err := loadResolved(common)
		if err != nil {
			return fail(err, 2)
		}
		if common.selectComponents {
			output := io.Writer(os.Stdout)
			if common.json {
				output = os.Stderr
			}
			if err := chooseComponents(os.Stdin, output, resolved); err != nil {
				return fail(err, 1)
			}
		}
		plan, err := conenv.BuildPlan(resolved)
		if err != nil {
			return fail(err, 1)
		}
		printPlan(plan, common.json)
		if args[0] == "status" && len(plan.Actions) > 0 {
			return 2
		}
		return 0
	case "apply":
		fs := flag.NewFlagSet("apply", flag.ContinueOnError)
		repo := fs.String("repo", "", "configuration repository")
		root := fs.String("root", "", "deprecated alias for --repo")
		profile := fs.String("profile", "", "profile name")
		yes := fs.Bool("yes", false, "apply without prompting")
		selectComponents := fs.Bool("select", false, "interactively select components")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		if fs.NArg() != 0 {
			return fail(errors.New("apply does not accept positional arguments"), 2)
		}
		common := commonFlags{repo: *repo, root: *root, profile: *profile}
		resolved, err := loadResolved(common)
		if err != nil {
			return fail(err, 2)
		}
		if *selectComponents {
			if err := chooseComponents(os.Stdin, os.Stdout, resolved); err != nil {
				return fail(err, 1)
			}
		}
		plan, err := conenv.BuildPlan(resolved)
		if err != nil {
			return fail(err, 1)
		}
		printPlan(plan, false)
		if len(plan.Actions) == 0 {
			return 0
		}
		if !*yes && !confirm("Apply these changes? [y/N] ") {
			return fail(errors.New("apply cancelled"), 1)
		}
		run, err := conenv.Apply(plan)
		if err != nil {
			if len(run.Packages) > 0 {
				fmt.Fprintf(os.Stderr, "packages retained after failure: %s\n", strings.Join(run.Packages, ", "))
			}
			return fail(err, 1)
		}
		fmt.Printf("applied run %s\n", run.ID)
		return 0
	case "doctor":
		common, extra, err := parseCommon("doctor", args[1:], true, true)
		if err != nil {
			return fail(err, 2)
		}
		if len(extra) != 0 {
			return fail(errors.New("doctor does not accept positional arguments"), 2)
		}
		resolved, err := loadResolved(common)
		if err != nil {
			return fail(err, 2)
		}
		if common.selectComponents {
			output := io.Writer(os.Stdout)
			if common.json {
				output = os.Stderr
			}
			if err := chooseComponents(os.Stdin, output, resolved); err != nil {
				return fail(err, 1)
			}
		}
		results := conenv.Doctor(resolved)
		unhealthy := false
		if common.json {
			_ = writeJSON(results)
		} else {
			for _, result := range results {
				label := "ok"
				if !result.Healthy {
					label = "fail"
					unhealthy = true
				}
				fmt.Printf("[%s] %s: %s\n", label, result.Name, strings.TrimSpace(result.Detail))
			}
		}
		for _, result := range results {
			unhealthy = unhealthy || !result.Healthy
		}
		if unhealthy {
			return 2
		}
		return 0
	case "restore":
		runID, yes, err := parseRestore(args[1:])
		if err != nil {
			return fail(errors.New("usage: conenv restore RUN_ID [--yes]"), 2)
		}
		if !yes && !confirm("Restore this run and replace current destinations? [y/N] ") {
			return fail(errors.New("restore cancelled"), 1)
		}
		if err := conenv.Restore(runID); err != nil {
			return fail(err, 1)
		}
		fmt.Printf("restored run %s\n", runID)
		return 0
	case "use":
		if len(args) != 2 {
			return fail(errors.New("usage: conenv use REPOSITORY"), 2)
		}
		selection, err := conenv.SaveRepository(args[1])
		if err != nil {
			return fail(err, 2)
		}
		fmt.Printf("default configuration repository: %s\n", selection.Path)
		return 0
	case "config":
		fs := flag.NewFlagSet("config", flag.ContinueOnError)
		asJSON := fs.Bool("json", false, "emit JSON")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
			return fail(errors.New("usage: conenv config [--json]"), 2)
		}
		selection, err := conenv.ResolveRepository("")
		if err != nil {
			return fail(err, 2)
		}
		if *asJSON {
			_ = writeJSON(selection)
		} else {
			configPath, _ := conenv.UserConfigPath()
			fmt.Printf("repository: %s\nsource: %s\nuser config: %s\n", selection.Path, selection.Source, configPath)
		}
		return 0
	case "schema":
		fs := flag.NewFlagSet("schema", flag.ContinueOnError)
		writePath := fs.String("write", "", "write the embedded schema to a file")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
			return fail(errors.New("usage: conenv schema [--write FILE]"), 2)
		}
		if *writePath == "" {
			_, _ = os.Stdout.Write(assets.ManifestSchema)
			return 0
		}
		absolute, err := filepath.Abs(*writePath)
		if err != nil {
			return fail(err, 1)
		}
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			return fail(err, 1)
		}
		if err := os.WriteFile(absolute, assets.ManifestSchema, 0o644); err != nil {
			return fail(err, 1)
		}
		fmt.Printf("wrote schema to %s\n", absolute)
		return 0
	case "version", "--version", "-version":
		fmt.Println(version)
		return 0
	case "help", "--help", "-h":
		usage()
		return 0
	default:
		usage()
		return fail(fmt.Errorf("unknown command %q", args[0]), 2)
	}
}

func parseRestore(args []string) (string, bool, error) {
	var runID string
	yes := false
	for _, arg := range args {
		switch arg {
		case "--yes":
			yes = true
		default:
			if strings.HasPrefix(arg, "-") || runID != "" {
				return "", false, errors.New("invalid restore arguments")
			}
			runID = arg
		}
	}
	if runID == "" {
		return "", false, errors.New("run ID is required")
	}
	return runID, yes, nil
}

func parseCommon(name string, args []string, jsonFlag, selectFlag bool) (commonFlags, []string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	common := commonFlags{}
	fs.StringVar(&common.repo, "repo", "", "configuration repository")
	fs.StringVar(&common.root, "root", "", "deprecated alias for --repo")
	fs.StringVar(&common.profile, "profile", "", "profile name")
	if jsonFlag {
		fs.BoolVar(&common.json, "json", false, "emit JSON")
	}
	if selectFlag {
		fs.BoolVar(&common.selectComponents, "select", false, "interactively select components")
	}
	if err := fs.Parse(args); err != nil {
		return common, nil, err
	}
	return common, fs.Args(), nil
}

type componentOption struct {
	name        string
	category    string
	description string
}

func chooseComponents(input io.Reader, output io.Writer, resolved *conenv.Resolved) error {
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

func loadResolved(common commonFlags) (*conenv.Resolved, error) {
	if common.repo != "" && common.root != "" {
		return nil, errors.New("use only one of --repo and --root")
	}
	explicit := common.repo
	if explicit == "" {
		explicit = common.root
	}
	selection, err := conenv.ResolveRepository(explicit)
	if err != nil {
		return nil, err
	}
	return conenv.Load(selection.Path, common.profile)
}

func printPlan(plan *conenv.Plan, asJSON bool) {
	if asJSON {
		_ = writeJSON(plan)
		return
	}
	if len(plan.Actions) == 0 {
		fmt.Printf("profile %q is in sync\n", plan.Profile)
		return
	}
	fmt.Printf("profile %q: %d action(s)\n", plan.Profile, len(plan.Actions))
	for _, action := range plan.Actions {
		fmt.Printf("- [%s] %s\n", action.Kind, action.Summary)
	}
}

func writeJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func confirm(prompt string) bool {
	fmt.Print(prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}

func fail(err error, code int) int {
	fmt.Fprintln(os.Stderr, "conenv:", err)
	return code
}

func usage() {
	fmt.Fprintln(os.Stderr, `conenv manages a declarative development environment.

Usage:
  conenv validate [--repo DIR] [--profile NAME]
  conenv plan     [--repo DIR] [--profile NAME] [--select] [--json]
  conenv apply    [--repo DIR] [--profile NAME] [--select] [--yes]
  conenv status   [--repo DIR] [--profile NAME] [--select] [--json]
  conenv doctor   [--repo DIR] [--profile NAME] [--select] [--json]
  conenv restore  RUN_ID [--yes]
  conenv use      REPOSITORY
  conenv config   [--json]
  conenv schema   [--write FILE]
  conenv version`)
}
