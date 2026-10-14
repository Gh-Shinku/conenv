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
	"runtime"
	"sort"
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
	selection        string
	only             stringList
	deferRuntime     bool
}

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }

func (values *stringList) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("selection target must not be empty")
	}
	*values = append(*values, value)
	return nil
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
	if len(args) > 1 && (args[len(args)-1] == "--help" || args[len(args)-1] == "-h") {
		usage()
		return 0
	}
	switch args[0] {
	case "validate":
		common, extra, err := parseCommon("validate", args[1:], true, false)
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
		result := validationResult{
			Valid: true, Repository: resolved.Root, Profile: resolved.Profile,
			Scope: resolvedComponentNames(resolved), Platform: runtime.GOOS + "/" + runtime.GOARCH,
			Skipped: resolved.Skipped,
		}
		if common.json {
			if err := writeJSON(result); err != nil {
				return fail(err, 1)
			}
		} else {
			fmt.Printf("valid profile %q with %d component(s)\n", resolved.Profile, len(resolved.Components))
		}
		return 0
	case "plan":
		common, extra, err := parseCommon("plan", args[1:], true, true)
		if err != nil {
			return fail(err, 2)
		}
		if len(extra) != 0 {
			return fail(errors.New("plan does not accept positional arguments"), 2)
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
			if err := conenv.ValidateScope(resolved); err != nil {
				return fail(err, 2)
			}
		}
		plan, err := conenv.BuildPlan(resolved)
		if err != nil {
			return fail(err, 1)
		}
		if err := printPlan(plan, common.json); err != nil {
			return fail(err, 1)
		}
		return 0
	case "status":
		common, extra, err := parseCommon("status", args[1:], true, true)
		if err != nil {
			return fail(err, 2)
		}
		if len(extra) != 0 {
			return fail(errors.New("status does not accept positional arguments"), 2)
		}
		common.deferRuntime = true
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
		report := conenv.InspectStatus(resolved)
		if common.json {
			if err := writeJSON(report); err != nil {
				return fail(err, 1)
			}
		} else {
			printStatus(report)
		}
		if !report.InSync {
			return 3
		}
		return 0
	case "apply":
		fs := flag.NewFlagSet("apply", flag.ContinueOnError)
		repo := fs.String("repo", "", "configuration repository")
		root := fs.String("root", "", "deprecated alias for --repo")
		profile := fs.String("profile", "", "profile name")
		yes := fs.Bool("yes", false, "apply without prompting")
		asJSON := fs.Bool("json", false, "emit JSON")
		selectComponents := fs.Bool("select", false, "interactively select components")
		selection := fs.String("selection", "", "load an exact selection from YAML")
		var only stringList
		fs.Var(&only, "only", "select a component or nested resource (repeatable)")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		if fs.NArg() != 0 {
			return fail(errors.New("apply does not accept positional arguments"), 2)
		}
		common := commonFlags{repo: *repo, root: *root, profile: *profile, selectComponents: *selectComponents, selection: *selection, only: only}
		if err := validateScopeFlags(common); err != nil {
			return fail(err, 2)
		}
		resolved, err := loadResolved(common)
		if err != nil {
			return fail(err, 2)
		}
		tuiConfirmed := false
		if common.selectComponents {
			selectionOutput := io.Writer(os.Stdout)
			if *asJSON {
				selectionOutput = os.Stderr
			}
			if canUseTUI(os.Stdin, selectionOutput) {
				result, err := runComponentTUI(os.Stdin, selectionOutput, resolved, !*yes)
				if err != nil {
					return failSelection(err)
				}
				if err := filterComponents(resolved, result.SelectedIDs); err != nil {
					return failSelection(err)
				}
				tuiConfirmed = result.Confirmed && !*yes
			} else if err := chooseComponentsLineMode(os.Stdin, selectionOutput, resolved); err != nil {
				return failSelection(err)
			}
			if err := conenv.ValidateScope(resolved); err != nil {
				return fail(err, 2)
			}
		}
		plan, err := conenv.BuildPlan(resolved)
		if err != nil {
			return fail(err, 1)
		}
		if !*asJSON {
			if err := printPlan(plan, false); err != nil {
				return fail(err, 1)
			}
		}
		if len(plan.Actions) == 0 {
			if *asJSON {
				if err := writeJSON(applyCommandResult{Plan: plan}); err != nil {
					return fail(err, 1)
				}
			}
			return 0
		}
		if !*yes && !tuiConfirmed {
			if !canPrompt(os.Stdin) {
				return fail(errors.New("non-interactive apply requires --yes"), 2)
			}
			if !confirm("Apply these changes? [y/N] ") {
				return fail(errors.New("apply cancelled"), 1)
			}
		}
		run, err := conenv.Apply(plan)
		if err != nil {
			if len(run.Packages) > 0 {
				fmt.Fprintf(os.Stderr, "packages retained after failure: %s\n", strings.Join(run.Packages, ", "))
			}
			return fail(err, 1)
		}
		if *asJSON {
			if err := writeJSON(applyCommandResult{Plan: plan, Applied: true, Run: run}); err != nil {
				return fail(err, 1)
			}
		} else {
			fmt.Printf("applied run %s\n", run.ID)
		}
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
			if err := conenv.ValidateScope(resolved); err != nil {
				return fail(err, 2)
			}
		}
		results := conenv.Doctor(resolved)
		unhealthy := false
		if common.json {
			report := doctorReport{
				Repository: resolved.Root, Profile: resolved.Profile, Scope: resolvedComponentNames(resolved),
				Platform: runtime.GOOS + "/" + runtime.GOARCH, Skipped: resolved.Skipped, Healthy: true, Checks: results,
			}
			for _, result := range results {
				report.Healthy = report.Healthy && result.Healthy
			}
			if err := writeJSON(report); err != nil {
				return fail(err, 1)
			}
		} else {
			fmt.Printf("repository: %s\nprofile: %s\nplatform: %s\nscope: %s\n", resolved.Root, resolved.Profile, runtime.GOOS+"/"+runtime.GOARCH, strings.Join(resolvedComponentNames(resolved), ", "))
			if len(resolved.Skipped) > 0 {
				fmt.Printf("skipped: %s\n", strings.Join(resolved.Skipped, ", "))
			}
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
			return 3
		}
		return 0
	case "profile":
		return runProfileCommand(args[1:])
	case "component":
		return runComponentCommand(args[1:])
	case "history":
		return runHistoryCommand(args[1:])
	case "select":
		fs := flag.NewFlagSet("select", flag.ContinueOnError)
		repo := fs.String("repo", "", "configuration repository")
		root := fs.String("root", "", "deprecated alias for --repo")
		profile := fs.String("profile", "", "profile name")
		writePath := fs.String("write", "", "write the selection YAML")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		if fs.NArg() != 0 || *writePath == "" {
			return fail(errors.New("usage: conenv select --write FILE [--repo DIR] [--profile NAME]"), 2)
		}
		common := commonFlags{repo: *repo, root: *root, profile: *profile, selectComponents: true}
		resolved, err := loadResolved(common)
		if err != nil {
			return fail(err, 2)
		}
		if err := chooseComponents(os.Stdin, os.Stdout, resolved); err != nil {
			return failSelection(err)
		}
		targets := make([]string, 0, len(resolved.Components))
		for _, component := range resolved.Components {
			targets = append(targets, component.Name)
		}
		spec, err := conenv.SelectionFromTargets(resolved, targets)
		if err != nil {
			return fail(err, 2)
		}
		if err := conenv.SaveSelection(*writePath, spec); err != nil {
			return fail(err, 1)
		}
		absolute, _ := filepath.Abs(*writePath)
		fmt.Printf("wrote selection to %s\n", absolute)
		return 0
	case "restore":
		options, err := parseRestore(args[1:])
		if err != nil {
			return fail(errors.New("usage: conenv restore RUN_ID [--dry-run] [--yes] [--json]"), 2)
		}
		plan, err := conenv.PlanRestore(options.runID)
		if err != nil {
			return fail(err, 1)
		}
		if options.dryRun {
			if options.json {
				if err := writeJSON(plan); err != nil {
					return fail(err, 1)
				}
			} else {
				printRestorePlan(plan)
			}
			return 0
		}
		if !options.yes {
			if !canPrompt(os.Stdin) {
				return fail(errors.New("non-interactive restore requires --yes"), 2)
			}
			if options.json {
				printRestorePlanTo(os.Stderr, plan)
			} else {
				printRestorePlan(plan)
			}
			if !confirm("Restore this run and replace current destinations? [y/N] ") {
				return fail(errors.New("restore cancelled"), 1)
			}
		}
		if err := conenv.Restore(options.runID); err != nil {
			return fail(err, 1)
		}
		if options.json {
			if err := writeJSON(struct {
				Restored bool                `json:"restored"`
				Plan     *conenv.RestorePlan `json:"plan"`
			}{Restored: true, Plan: plan}); err != nil {
				return fail(err, 1)
			}
		} else {
			fmt.Printf("restored run %s\n", options.runID)
		}
		return 0
	case "repo":
		return runRepoCommand(args[1:])
	case "use":
		return runRepoCommand(append([]string{"use"}, args[1:]...))
	case "config":
		return runRepoCommand(append([]string{"show"}, args[1:]...))
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

type profileInfo struct {
	Name       string   `json:"name"`
	Default    bool     `json:"default"`
	Extends    []string `json:"extends,omitempty"`
	Components []string `json:"components"`
	Skipped    []string `json:"skipped,omitempty"`
}

type componentInfo struct {
	Name          string           `json:"name"`
	Category      string           `json:"category"`
	Description   string           `json:"description,omitempty"`
	Platforms     []string         `json:"platforms,omitempty"`
	Architectures []string         `json:"architectures,omitempty"`
	Supported     bool             `json:"supported"`
	Declaration   conenv.Component `json:"declaration,omitempty"`
}

type applyCommandResult struct {
	Plan    *conenv.Plan      `json:"plan"`
	Applied bool              `json:"applied"`
	Run     *conenv.RunRecord `json:"run,omitempty"`
}

type validationResult struct {
	Valid      bool     `json:"valid"`
	Repository string   `json:"repository"`
	Profile    string   `json:"profile"`
	Scope      []string `json:"scope"`
	Platform   string   `json:"platform"`
	Skipped    []string `json:"skipped,omitempty"`
}

type doctorReport struct {
	Repository string               `json:"repository"`
	Profile    string               `json:"profile"`
	Scope      []string             `json:"scope"`
	Platform   string               `json:"platform"`
	Skipped    []string             `json:"skipped,omitempty"`
	Healthy    bool                 `json:"healthy"`
	Checks     []conenv.CheckResult `json:"checks"`
}

func runRepoCommand(args []string) int {
	if len(args) == 0 {
		return fail(errors.New("usage: conenv repo show|use REPOSITORY|unset"), 2)
	}
	switch args[0] {
	case "show":
		fs := flag.NewFlagSet("repo show", flag.ContinueOnError)
		asJSON := fs.Bool("json", false, "emit JSON")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
			return fail(errors.New("usage: conenv repo show [--json]"), 2)
		}
		selection, err := conenv.ResolveRepository("")
		if err != nil {
			return fail(err, 2)
		}
		if *asJSON {
			if err := writeJSON(selection); err != nil {
				return fail(err, 1)
			}
		} else {
			configPath, _ := conenv.UserConfigPath()
			fmt.Printf("repository: %s\nsource: %s\nuser config: %s\n", selection.Path, selection.Source, configPath)
		}
		return 0
	case "use":
		if len(args) != 2 {
			return fail(errors.New("usage: conenv repo use REPOSITORY"), 2)
		}
		selection, err := conenv.SaveRepository(args[1])
		if err != nil {
			return fail(err, 2)
		}
		fmt.Printf("default configuration repository: %s\n", selection.Path)
		return 0
	case "unset":
		if len(args) != 1 {
			return fail(errors.New("usage: conenv repo unset"), 2)
		}
		if err := conenv.ClearRepository(); err != nil {
			return fail(err, 1)
		}
		fmt.Println("cleared default configuration repository")
		return 0
	default:
		return fail(fmt.Errorf("unknown repo command %q", args[0]), 2)
	}
}

func runProfileCommand(args []string) int {
	if len(args) == 0 {
		return fail(errors.New("usage: conenv profile list|show NAME [--repo DIR] [--json]"), 2)
	}
	action := args[0]
	name := ""
	flagArgs := args[1:]
	if action == "show" {
		if len(args) < 2 || strings.HasPrefix(args[1], "-") {
			return fail(errors.New("usage: conenv profile show NAME [--repo DIR] [--json]"), 2)
		}
		name = args[1]
		flagArgs = args[2:]
	}
	repository, asJSON, rest, err := loadManifestCommand("profile "+action, flagArgs)
	if err != nil {
		return fail(err, 2)
	}
	manifest, err := conenv.LoadManifest(repository.Path)
	if err != nil {
		return fail(err, 2)
	}
	switch action {
	case "list":
		if len(rest) != 0 {
			return fail(errors.New("profile list does not accept positional arguments"), 2)
		}
		names := make([]string, 0, len(manifest.Profiles))
		for name := range manifest.Profiles {
			names = append(names, name)
		}
		sort.Strings(names)
		profiles := make([]profileInfo, 0, len(names))
		for _, name := range names {
			resolved, resolveErr := conenv.LoadProfile(repository.Path, name)
			if resolveErr != nil {
				return fail(resolveErr, 2)
			}
			profiles = append(profiles, profileInfo{Name: name, Default: name == manifest.DefaultProfile, Extends: manifest.Profiles[name].Extends, Components: resolvedComponentNames(resolved), Skipped: resolved.Skipped})
		}
		if asJSON {
			if err := writeJSON(profiles); err != nil {
				return fail(err, 1)
			}
		} else {
			for _, profile := range profiles {
				marker := ""
				if profile.Default {
					marker = " (default)"
				}
				fmt.Printf("%s%s: %s\n", profile.Name, marker, strings.Join(profile.Components, ", "))
			}
		}
		return 0
	case "show":
		if len(rest) != 0 {
			return fail(errors.New("usage: conenv profile show NAME [--repo DIR] [--json]"), 2)
		}
		profile, ok := manifest.Profiles[name]
		if !ok {
			return fail(fmt.Errorf("unknown profile %q", name), 2)
		}
		resolved, err := conenv.LoadProfile(repository.Path, name)
		if err != nil {
			return fail(err, 2)
		}
		info := profileInfo{Name: name, Default: name == manifest.DefaultProfile, Extends: profile.Extends, Components: resolvedComponentNames(resolved), Skipped: resolved.Skipped}
		if asJSON {
			if err := writeJSON(info); err != nil {
				return fail(err, 1)
			}
		} else {
			fmt.Printf("profile: %s\ndefault: %t\nextends: %s\ncomponents: %s\n", info.Name, info.Default, strings.Join(info.Extends, ", "), strings.Join(info.Components, ", "))
			if len(info.Skipped) > 0 {
				fmt.Printf("skipped: %s\n", strings.Join(info.Skipped, ", "))
			}
		}
		return 0
	default:
		return fail(fmt.Errorf("unknown profile command %q", action), 2)
	}
}

func runComponentCommand(args []string) int {
	if len(args) == 0 {
		return fail(errors.New("usage: conenv component list|show NAME [--repo DIR] [--json]"), 2)
	}
	action := args[0]
	name := ""
	flagArgs := args[1:]
	if action == "show" {
		if len(args) < 2 || strings.HasPrefix(args[1], "-") {
			return fail(errors.New("usage: conenv component show NAME [--repo DIR] [--json]"), 2)
		}
		name = args[1]
		flagArgs = args[2:]
	}
	repository, asJSON, rest, err := loadManifestCommand("component "+action, flagArgs)
	if err != nil {
		return fail(err, 2)
	}
	manifest, err := conenv.LoadManifest(repository.Path)
	if err != nil {
		return fail(err, 2)
	}
	switch action {
	case "list":
		if len(rest) != 0 {
			return fail(errors.New("component list does not accept positional arguments"), 2)
		}
		names := make([]string, 0, len(manifest.Components))
		for name := range manifest.Components {
			names = append(names, name)
		}
		sort.Strings(names)
		components := make([]componentInfo, 0, len(names))
		for _, name := range names {
			components = append(components, presentComponent(name, manifest.Components[name], false))
		}
		if asJSON {
			if err := writeJSON(components); err != nil {
				return fail(err, 1)
			}
		} else {
			for _, component := range components {
				state := "supported"
				if !component.Supported {
					state = "unsupported"
				}
				fmt.Printf("%s [%s, %s] %s\n", component.Name, component.Category, state, component.Description)
			}
		}
		return 0
	case "show":
		if len(rest) != 0 {
			return fail(errors.New("usage: conenv component show NAME [--repo DIR] [--json]"), 2)
		}
		component, ok := manifest.Components[name]
		if !ok {
			return fail(fmt.Errorf("unknown component %q", name), 2)
		}
		info := presentComponent(name, component, true)
		if asJSON {
			if err := writeJSON(info); err != nil {
				return fail(err, 1)
			}
		} else {
			fmt.Printf("component: %s\ncategory: %s\nsupported: %t\ndescription: %s\npackages: %d\nfiles: %d\nchecks: %d\n", info.Name, info.Category, info.Supported, info.Description, len(component.Packages), len(component.Files), len(component.Checks))
			if component.Codex != nil {
				fmt.Printf("skills: %d\nmcp servers: %d\n", len(component.Codex.Skills), len(component.Codex.MCPServers))
			}
		}
		return 0
	default:
		return fail(fmt.Errorf("unknown component command %q", action), 2)
	}
}

func runHistoryCommand(args []string) int {
	if len(args) == 0 {
		return fail(errors.New("usage: conenv history list|show RUN_ID [--json]"), 2)
	}
	action := args[0]
	runID := ""
	flagArgs := args[1:]
	if action == "show" {
		if len(args) < 2 || strings.HasPrefix(args[1], "-") {
			return fail(errors.New("usage: conenv history show RUN_ID [--json]"), 2)
		}
		runID = args[1]
		flagArgs = args[2:]
	}
	fs := flag.NewFlagSet("history "+action, flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	rest := fs.Args()
	state, err := conenv.LoadState()
	if err != nil {
		return fail(err, 1)
	}
	switch action {
	case "list":
		if len(rest) != 0 {
			return fail(errors.New("history list does not accept positional arguments"), 2)
		}
		runs := append([]conenv.RunRecord(nil), state.Runs...)
		for left, right := 0, len(runs)-1; left < right; left, right = left+1, right-1 {
			runs[left], runs[right] = runs[right], runs[left]
		}
		if *asJSON {
			if err := writeJSON(runs); err != nil {
				return fail(err, 1)
			}
		} else if len(runs) == 0 {
			fmt.Println("no recorded runs")
		} else {
			for _, run := range runs {
				fmt.Printf("%s  %s  %s  %d change(s)\n", run.ID, run.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), run.Profile, len(run.Changes))
			}
		}
		return 0
	case "show":
		if len(rest) != 0 {
			return fail(errors.New("usage: conenv history show RUN_ID [--json]"), 2)
		}
		run, err := conenv.FindRun(state, runID)
		if err != nil {
			return fail(err, 2)
		}
		if *asJSON {
			if err := writeJSON(run); err != nil {
				return fail(err, 1)
			}
		} else {
			fmt.Printf("run: %s\nprofile: %s\ncreated: %s\nchanges: %d\npackages retained: %d\nresources: %d\n", run.ID, run.Profile, run.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), len(run.Changes), len(run.Packages), len(run.Resources))
		}
		return 0
	default:
		return fail(fmt.Errorf("unknown history command %q", action), 2)
	}
}

func loadManifestCommand(name string, args []string) (conenv.RepositorySelection, bool, []string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	repo := fs.String("repo", "", "configuration repository")
	root := fs.String("root", "", "deprecated alias for --repo")
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return conenv.RepositorySelection{}, false, nil, err
	}
	if *repo != "" && *root != "" {
		return conenv.RepositorySelection{}, false, nil, errors.New("use only one of --repo and --root")
	}
	explicit := *repo
	if explicit == "" {
		explicit = *root
	}
	repository, err := conenv.ResolveRepository(explicit)
	return repository, *asJSON, fs.Args(), err
}

func resolvedComponentNames(resolved *conenv.Resolved) []string {
	names := make([]string, 0, len(resolved.Components))
	for _, component := range resolved.Components {
		names = append(names, component.Name)
	}
	return names
}

func presentComponent(name string, component conenv.Component, declaration bool) componentInfo {
	category := component.Category
	if category == "" {
		category = "program"
		if component.Codex != nil && len(component.Files) == 0 && len(component.Packages) == 0 {
			category = "ai"
		}
	}
	info := componentInfo{
		Name: name, Category: category, Description: component.Description,
		Platforms: component.Platforms, Architectures: component.Architectures,
		Supported: matchesCurrent(runtime.GOOS, component.Platforms) && matchesCurrent(runtime.GOARCH, component.Architectures),
	}
	if declaration {
		info.Declaration = component
	}
	return info
}

func matchesCurrent(value string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, candidate := range allowed {
		if candidate == value {
			return true
		}
	}
	return false
}

func printStatus(report conenv.StatusReport) {
	fmt.Printf("repository: %s\nprofile: %s\nplatform: %s\nscope: %s\n", report.Repository, report.Profile, report.Platform, strings.Join(report.Scope, ", "))
	if len(report.Skipped) > 0 {
		fmt.Printf("skipped: %s\n", strings.Join(report.Skipped, ", "))
	}
	for _, component := range report.Components {
		detail := component.State
		if component.Pending > 0 {
			detail = fmt.Sprintf("%s (%d pending)", detail, component.Pending)
		}
		if component.Error != "" {
			detail = detail + ": " + component.Error
		}
		fmt.Printf("- %s: %s\n", component.Name, detail)
	}
}

type restoreOptions struct {
	runID  string
	yes    bool
	dryRun bool
	json   bool
}

func parseRestore(args []string) (restoreOptions, error) {
	options := restoreOptions{}
	for _, arg := range args {
		switch arg {
		case "--yes":
			options.yes = true
		case "--dry-run":
			options.dryRun = true
		case "--json":
			options.json = true
		default:
			if strings.HasPrefix(arg, "-") || options.runID != "" {
				return restoreOptions{}, errors.New("invalid restore arguments")
			}
			options.runID = arg
		}
	}
	if options.runID == "" {
		return restoreOptions{}, errors.New("run ID is required")
	}
	return options, nil
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
		fs.StringVar(&common.selection, "selection", "", "load an exact selection from YAML")
		fs.Var(&common.only, "only", "select a component or nested resource (repeatable)")
	}
	if err := fs.Parse(args); err != nil {
		return common, nil, err
	}
	if err := validateScopeFlags(common); err != nil {
		return common, nil, err
	}
	return common, fs.Args(), nil
}

func validateScopeFlags(common commonFlags) error {
	selectedModes := 0
	if common.selectComponents {
		selectedModes++
	}
	if common.selection != "" {
		selectedModes++
	}
	if len(common.only) > 0 {
		selectedModes++
	}
	if selectedModes > 1 {
		return errors.New("use only one of --select, --selection, and --only")
	}
	return nil
}

func loadResolved(common commonFlags) (*conenv.Resolved, error) {
	if common.repo != "" && common.root != "" {
		return nil, errors.New("use only one of --repo and --root")
	}
	explicit := common.repo
	if explicit == "" {
		explicit = common.root
	}
	repository, err := conenv.ResolveRepository(explicit)
	if err != nil {
		return nil, err
	}
	var spec *conenv.SelectionSpec
	if common.selection != "" {
		spec, err = conenv.LoadSelection(common.selection)
		if err != nil {
			return nil, err
		}
		if common.profile == "" {
			common.profile = spec.Profile
		}
	}
	resolved, err := conenv.LoadProfile(repository.Path, common.profile)
	if err != nil {
		return nil, err
	}
	if len(common.only) > 0 {
		spec, err = conenv.SelectionFromTargets(resolved, common.only)
		if err != nil {
			return nil, err
		}
	}
	if spec != nil {
		resolved, err = conenv.ApplySelection(resolved, spec)
		if err != nil {
			return nil, err
		}
	}
	if common.selectComponents {
		return resolved, nil
	}
	if common.deferRuntime {
		return resolved, nil
	}
	if spec != nil {
		return resolved, conenv.ValidateScope(resolved)
	}
	return resolved, conenv.ValidateResolved(resolved)
}

func printPlan(plan *conenv.Plan, asJSON bool) error {
	if asJSON {
		return writeJSON(plan)
	}
	fmt.Printf("repository: %s\nprofile: %s\nplatform: %s\nscope: %s\n", plan.Repository, plan.Profile, plan.Platform, strings.Join(plan.Scope, ", "))
	if len(plan.Skipped) > 0 {
		fmt.Printf("skipped: %s\n", strings.Join(plan.Skipped, ", "))
	}
	if len(plan.Actions) == 0 {
		fmt.Println("environment is in sync")
		return nil
	}
	fmt.Printf("actions: %d\n", len(plan.Actions))
	for _, action := range plan.Actions {
		fmt.Printf("- [%s] %s\n", action.Kind, action.Summary)
	}
	return nil
}

func writeJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func confirm(prompt string) bool {
	fmt.Fprint(os.Stderr, prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}

func printRestorePlan(plan *conenv.RestorePlan) {
	printRestorePlanTo(os.Stdout, plan)
}

func printRestorePlanTo(output io.Writer, plan *conenv.RestorePlan) {
	fmt.Fprintf(output, "run: %s\nprofile: %s\n", plan.RunID, plan.Profile)
	for _, action := range plan.Actions {
		fmt.Fprintf(output, "- [%s] %s\n", action.Action, action.Target)
	}
	if len(plan.PackagesRetained) > 0 {
		fmt.Fprintf(output, "packages retained: %s\n", strings.Join(plan.PackagesRetained, ", "))
	}
}

func fail(err error, code int) int {
	fmt.Fprintln(os.Stderr, "conenv:", err)
	return code
}

func usage() {
	fmt.Fprintln(os.Stderr, `conenv manages a declarative development environment.

Usage:
  conenv validate [--repo DIR] [--profile NAME] [--json]
  conenv plan     [--repo DIR] [--profile NAME] [--select | --selection FILE | --only TARGET...] [--json]
  conenv apply    [--repo DIR] [--profile NAME] [--select | --selection FILE | --only TARGET...] [--yes] [--json]
  conenv status   [--repo DIR] [--profile NAME] [--select | --selection FILE | --only TARGET...] [--json]
  conenv doctor   [--repo DIR] [--profile NAME] [--select | --selection FILE | --only TARGET...] [--json]
  conenv select   --write FILE [--repo DIR] [--profile NAME]
  conenv profile  list|show NAME [--repo DIR] [--json]
  conenv component list|show NAME [--repo DIR] [--json]
  conenv history  list|show RUN_ID [--json]
  conenv restore  RUN_ID [--dry-run] [--yes] [--json]
  conenv repo     show|use REPOSITORY|unset
  conenv use      REPOSITORY
  conenv config   [--json]
  conenv schema   [--write FILE]
  conenv version`)
}
