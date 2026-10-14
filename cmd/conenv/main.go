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

	"conenv/internal/conenv"
)

var version = "dev"

// stdin is the interactive input source. It is a variable so tests can drive
// prompts and the line-mode selector without a terminal.
var stdin io.Reader = os.Stdin

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
	if isHelp(args) {
		usage()
		return 0
	}
	switch args[0] {
	case "config":
		return runConfig(args[1:])
	case "apply":
		return runApply(args[1:])
	case "status":
		return runStatus(args[1:])
	default:
		return unknownCommand(args[0])
	}
}

func isHelp(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" || arg == "help" {
			return true
		}
	}
	return false
}

// replacements point legacy command names at their current equivalent so a
// removed command fails with a usable suggestion instead of a bare error.
var replacements = map[string]string{
	"validate":  "config --check",
	"plan":      "apply --dry-run",
	"doctor":    "status --check",
	"restore":   "apply --undo",
	"undo":      "apply --undo",
	"select":    "apply --select",
	"profile":   "config --profile-detail NAME",
	"component": "config --component-detail NAME",
	"history":   "apply --undo --list",
	"runs":      "apply --undo --list",
	"repo":      "config",
	"use":       "config DIR",
	"schema":    "internal/assets/schema/conenv.v1.schema.json",
}

func unknownCommand(name string) int {
	message := fmt.Sprintf("unknown command %q", name)
	if replacement, ok := replacements[name]; ok {
		message = fmt.Sprintf("%s; use conenv %s", message, replacement)
	}
	fmt.Fprintln(os.Stderr, "conenv:", message)
	usage()
	return 2
}

func isInteractive() bool {
	return canPrompt(stdin)
}

// ---------------------------------------------------------------------------
// shared flags
// ---------------------------------------------------------------------------

type repositoryFlags struct {
	repo    string
	root    string
	profile string
	json    bool
}

func (flags *repositoryFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&flags.repo, "repo", "", "configuration repository")
	fs.StringVar(&flags.root, "root", "", "deprecated alias for --repo")
	fs.StringVar(&flags.profile, "profile", "", "profile name")
	fs.BoolVar(&flags.json, "json", false, "emit JSON")
}

func (flags repositoryFlags) repository() string {
	if flags.repo != "" {
		return flags.repo
	}
	return flags.root
}

func (flags repositoryFlags) validate() error {
	if flags.repo != "" && flags.root != "" {
		return errors.New("use only one of --repo and --root")
	}
	return nil
}

type scopeFlags struct {
	selectComponents bool
	selection        string
	only             stringList
}

func (flags *scopeFlags) register(fs *flag.FlagSet) {
	fs.BoolVar(&flags.selectComponents, "select", false, "interactively select components")
	fs.StringVar(&flags.selection, "selection", "", "load an exact selection from YAML")
	fs.Var(&flags.only, "only", "select a component or nested resource (repeatable)")
}

func (flags scopeFlags) validate() error {
	modes := 0
	if flags.selectComponents {
		modes++
	}
	if flags.selection != "" {
		modes++
	}
	if len(flags.only) > 0 {
		modes++
	}
	if modes > 1 {
		return errors.New("use only one of --select, --selection, and --only")
	}
	return nil
}

type scopeOrigin string

const (
	scopeDefault scopeOrigin = "profile"
	scopeSaved   scopeOrigin = "saved selection"
	scopeFile    scopeOrigin = "selection file"
	scopeOnly    scopeOrigin = "only targets"
)

// loadScope resolves a repository, profile, and optional narrowing into the
// scope a command runs against. Command line selectors win over the saved
// machine default, and a saved selection recorded for another profile is
// ignored rather than treated as an error.
func loadScope(repository string, flags scopeFlags, profile string, deferRuntime bool) (*conenv.Resolved, scopeOrigin, error) {
	if err := flags.validate(); err != nil {
		return nil, "", err
	}
	selected, err := conenv.ResolveRepository(repository)
	if err != nil {
		return nil, "", err
	}
	saved, err := conenv.LoadDefaultSelection()
	if err != nil {
		return nil, "", err
	}
	requestedProfile := profile
	if requestedProfile == "" && len(flags.only) == 0 && flags.selection == "" && saved != nil {
		requestedProfile = saved.Profile
	}
	resolved, err := conenv.LoadProfile(selected.Path, requestedProfile)
	if err != nil {
		return nil, "", err
	}
	switch {
	case flags.selectComponents:
		// The interactive chooser narrows the resolved profile in place.
		resolved.SelectionSource = string(scopeDefault)
		return resolved, scopeDefault, nil
	case flags.selection != "":
		spec, err := conenv.LoadSelection(flags.selection)
		if err != nil {
			return nil, "", err
		}
		if spec.Profile != resolved.Profile {
			return nil, "", fmt.Errorf("selection profile %q does not match profile %q", spec.Profile, resolved.Profile)
		}
		narrowed, err := conenv.ApplySelection(resolved, spec)
		if err != nil {
			return nil, "", err
		}
		narrowed.SelectionSource = string(scopeFile)
		return narrowed, scopeFile, nil
	case len(flags.only) > 0:
		spec, err := conenv.SelectionFromTargets(resolved, flags.only)
		if err != nil {
			return nil, "", err
		}
		narrowed, err := conenv.ApplySelection(resolved, spec)
		if err != nil {
			return nil, "", err
		}
		narrowed.SelectionSource = string(scopeOnly)
		return narrowed, scopeOnly, nil
	}
	if saved != nil && profile == "" {
		narrowed, err := conenv.ApplySelection(resolved, saved)
		if err != nil {
			return nil, "", fmt.Errorf("saved selection: %w", err)
		}
		narrowed.SelectionSource = string(scopeSaved)
		return narrowed, scopeSaved, nil
	}
	resolved.SelectionSource = string(scopeDefault)
	if deferRuntime {
		return resolved, scopeDefault, nil
	}
	return resolved, scopeDefault, conenv.ValidateResolved(resolved)
}

func scopeSourceLabel(origin scopeOrigin) string {
	if origin == "" {
		return string(scopeDefault)
	}
	return string(origin)
}

// ---------------------------------------------------------------------------
// config
// ---------------------------------------------------------------------------

type configOptions struct {
	repositoryFlags
	unset           bool
	check           bool
	profileDetail   string
	componentDetail string
	profiles        bool
	components      bool
	unsetSelection  bool
	write           string
}

type scopeView struct {
	Profile         string   `json:"profile"`
	Default         bool     `json:"default"`
	DefaultProfile  string   `json:"defaultProfile,omitempty"`
	Components      []string `json:"components"`
	Skipped         []string `json:"skipped,omitempty"`
	SelectionSource string   `json:"selectionSource,omitempty"`
	SelectionPath   string   `json:"selectionPath,omitempty"`
}

type configReport struct {
	Repository      string          `json:"repository"`
	Source          string          `json:"source"`
	Valid           *bool           `json:"valid,omitempty"`
	SelectionSource string          `json:"selectionSource,omitempty"`
	SelectionPath   string          `json:"selectionPath,omitempty"`
	Scope           *scopeView      `json:"scope,omitempty"`
	Profiles        []profileInfo   `json:"profiles,omitempty"`
	Components      []componentInfo `json:"components,omitempty"`
	detail          bool
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

func runConfig(args []string) int {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	var options configOptions
	options.register(fs)
	fs.BoolVar(&options.unset, "unset", false, "clear the default configuration repository")
	fs.BoolVar(&options.check, "check", false, "validate the configuration without changing the machine")
	fs.StringVar(&options.profileDetail, "profile-detail", "", "show a profile and its resolved components")
	fs.StringVar(&options.componentDetail, "component-detail", "", "show a component declaration")
	fs.BoolVar(&options.profiles, "profiles", false, "list profiles")
	fs.BoolVar(&options.components, "components", false, "list components")
	fs.BoolVar(&options.unsetSelection, "unset-selection", false, "clear the saved default selection")
	fs.StringVar(&options.write, "write", "", "write the saved selection to a file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 1 {
		return fail(errors.New("usage: conenv config [DIR]"), 2)
	}
	if err := options.validate(); err != nil {
		return fail(err, 2)
	}
	if options.unset {
		return clearRepository()
	}
	if options.unsetSelection {
		return clearDefaultSelection()
	}
	if options.write != "" {
		return writeSavedSelection(options.write)
	}
	directory := fs.Arg(0)
	if directory != "" && options.repository() != "" {
		return fail(errors.New("use either a positional directory or --repo, not both"), 2)
	}
	explicit := directory
	if explicit == "" {
		explicit = options.repository()
	}
	selection, err := conenv.ResolveRepository(explicit)
	if err != nil {
		return fail(err, 2)
	}
	if directory != "" {
		if selection, err = conenv.SaveRepository(directory); err != nil {
			return fail(err, 2)
		}
	}

	switch {
	case options.check:
		resolved, origin, err := loadScope(selection.Path, scopeFlags{}, options.profile, false)
		if err != nil {
			return fail(err, 2)
		}
		valid := true
		report := configReport{Repository: resolved.Root, Source: selection.Source, Valid: &valid}
		report.SelectionSource = scopeSourceLabel(origin)
		if view := profileView(resolved); view != nil {
			view.DefaultProfile = resolved.Profile
			report.Scope = view
		}
		if options.json {
			if err := writeJSON(report); err != nil {
				return fail(err, 1)
			}
		} else {
			fmt.Printf("valid profile %q with %d component(s)\n", resolved.Profile, len(resolved.Components))
		}
		return 0
	case options.profileDetail != "":
		report, err := profileDetailReport(selection, options)
		if err != nil {
			return fail(err, 2)
		}
		return emitConfigReport(report, options.json)
	case options.componentDetail != "":
		report, err := componentDetailReport(selection, options)
		if err != nil {
			return fail(err, 2)
		}
		return emitConfigReport(report, options.json)
	}

	report, err := buildConfigReport(selection, options)
	if err != nil {
		return fail(err, 2)
	}
	return emitConfigReport(report, options.json)
}

func (options *configOptions) register(fs *flag.FlagSet) {
	options.repositoryFlags.register(fs)
}

func (options configOptions) validate() error {
	if err := options.repositoryFlags.validate(); err != nil {
		return err
	}
	modes := 0
	for _, set := range []bool{options.unset, options.check, options.profileDetail != "", options.componentDetail != "", options.unsetSelection, options.write != ""} {
		if set {
			modes++
		}
	}
	if modes > 1 {
		return errors.New("use only one of --unset, --check, --profile-detail, --component-detail, --unset-selection, and --write")
	}
	return nil
}

func clearRepository() int {
	if err := conenv.ClearRepository(); err != nil {
		return fail(err, 1)
	}
	fmt.Println("cleared default configuration repository")
	return 0
}

func clearDefaultSelection() int {
	if err := conenv.ClearDefaultSelection(); err != nil {
		return fail(err, 1)
	}
	fmt.Println("cleared saved selection")
	return 0
}

func writeSavedSelection(path string) int {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return fail(err, 1)
	}
	saved, err := conenv.LoadDefaultSelection()
	if err != nil {
		return fail(err, 1)
	}
	if saved == nil {
		return fail(errors.New("no saved selection to write; run conenv apply --select --save first"), 2)
	}
	if err := conenv.SaveSelection(absolute, saved); err != nil {
		return fail(err, 1)
	}
	fmt.Printf("wrote selection to %s\n", absolute)
	return 0
}

func buildConfigReport(selection conenv.RepositorySelection, options configOptions) (configReport, error) {
	report := configReport{Repository: selection.Path, Source: selection.Source}
	manifest, err := conenv.LoadManifest(selection.Path)
	if err != nil {
		return report, err
	}
	if options.profiles {
		report.Profiles = listProfiles(selection.Path, manifest)
	}
	if options.components {
		report.Components = listComponents(manifest)
	}
	resolved, origin, err := loadScope(selection.Path, scopeFlags{}, options.profile, true)
	if err != nil {
		return report, err
	}
	view := profileView(resolved)
	view.DefaultProfile = manifest.DefaultProfile
	view.SelectionSource = scopeSourceLabel(origin)
	if saved, _ := conenv.LoadDefaultSelection(); saved != nil {
		if path, err := conenv.SavedSelectionPath(); err == nil {
			view.SelectionPath = path
		}
	}
	report.SelectionSource = view.SelectionSource
	report.SelectionPath = view.SelectionPath
	report.Scope = view
	return report, nil
}

func profileDetailReport(selection conenv.RepositorySelection, options configOptions) (configReport, error) {
	manifest, err := conenv.LoadManifest(selection.Path)
	if err != nil {
		return configReport{}, err
	}
	profile, ok := manifest.Profiles[options.profileDetail]
	if !ok {
		return configReport{}, fmt.Errorf("unknown profile %q", options.profileDetail)
	}
	resolved, err := conenv.LoadProfile(selection.Path, options.profileDetail)
	if err != nil {
		return configReport{}, err
	}
	return configReport{
		Repository: selection.Path,
		Source:     selection.Source,
		detail:     true,
		Profiles: []profileInfo{{
			Name: options.profileDetail, Default: options.profileDetail == manifest.DefaultProfile,
			Extends: profile.Extends, Components: resolvedComponentNames(resolved), Skipped: resolved.Skipped,
		}},
	}, nil
}

func componentDetailReport(selection conenv.RepositorySelection, options configOptions) (configReport, error) {
	manifest, err := conenv.LoadManifest(selection.Path)
	if err != nil {
		return configReport{}, err
	}
	declaration, ok := manifest.Components[options.componentDetail]
	if !ok {
		return configReport{}, fmt.Errorf("unknown component %q", options.componentDetail)
	}
	return configReport{
		Repository: selection.Path,
		Source:     selection.Source,
		detail:     true,
		Components: []componentInfo{presentComponent(options.componentDetail, declaration, true)},
	}, nil
}

func emitConfigReport(report configReport, asJSON bool) int {
	if asJSON {
		if err := writeJSON(report); err != nil {
			return fail(err, 1)
		}
		return 0
	}
	printConfigReport(report)
	return 0
}

func printConfigReport(report configReport) {
	fmt.Printf("repository: %s\nsource: %s\n", report.Repository, report.Source)
	if report.Valid != nil {
		fmt.Printf("valid: %t\n", *report.Valid)
	}
	if report.detail {
		printDetail(report)
		return
	}
	if report.Scope != nil {
		fmt.Printf("profile: %s\n", report.Scope.Profile)
		if report.Scope.DefaultProfile != "" {
			fmt.Printf("default profile: %s\n", report.Scope.DefaultProfile)
		}
		fmt.Printf("scope source: %s\nscope: %s\n", report.Scope.SelectionSource, strings.Join(report.Scope.Components, ", "))
		if report.Scope.SelectionPath != "" {
			fmt.Printf("saved selection: %s\n", report.Scope.SelectionPath)
		}
		if len(report.Scope.Skipped) > 0 {
			fmt.Printf("skipped: %s\n", strings.Join(report.Scope.Skipped, ", "))
		}
	}
	if len(report.Profiles) > 0 {
		fmt.Println("profiles:")
		for _, profile := range report.Profiles {
			marker := ""
			if profile.Default {
				marker = " (default)"
			}
			fmt.Printf("- %s%s: %s\n", profile.Name, marker, strings.Join(profile.Components, ", "))
		}
	}
	if len(report.Components) > 0 {
		fmt.Println("components:")
		for _, component := range report.Components {
			state := "supported"
			if !component.Supported {
				state = "unsupported"
			}
			fmt.Printf("- %s [%s, %s] packages=%d files=%d checks=%d", component.Name, component.Category, state, len(component.Declaration.Packages), len(component.Declaration.Files), len(component.Declaration.Checks))
			if component.Declaration.Codex != nil {
				fmt.Printf(" skills=%d mcpServers=%d", len(component.Declaration.Codex.Skills), len(component.Declaration.Codex.MCPServers))
			}
			if component.Description != "" {
				fmt.Printf(": %s", component.Description)
			}
			fmt.Println()
		}
	}
}

// printDetail renders the single profile or component requested by
// --profile-detail or --component-detail.
func printDetail(report configReport) {
	for _, profile := range report.Profiles {
		fmt.Printf("profile: %s\ndefault: %t\nextends: %s\ncomponents: %s\n", profile.Name, profile.Default, strings.Join(profile.Extends, ", "), strings.Join(profile.Components, ", "))
		if len(profile.Skipped) > 0 {
			fmt.Printf("skipped: %s\n", strings.Join(profile.Skipped, ", "))
		}
	}
	for _, component := range report.Components {
		declaration := component.Declaration
		fmt.Printf("component: %s\ncategory: %s\nsupported: %t\ndescription: %s\npackages: %d\nfiles: %d\nchecks: %d\n", component.Name, component.Category, component.Supported, component.Description, len(declaration.Packages), len(declaration.Files), len(declaration.Checks))
		if declaration.Codex != nil {
			fmt.Printf("skills: %d\nmcp servers: %d\n", len(declaration.Codex.Skills), len(declaration.Codex.MCPServers))
		}
	}
}

func listProfiles(root string, manifest *conenv.Manifest) []profileInfo {
	names := make([]string, 0, len(manifest.Profiles))
	for name := range manifest.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	profiles := make([]profileInfo, 0, len(names))
	for _, name := range names {
		resolved, err := conenv.LoadProfile(root, name)
		if err != nil {
			profiles = append(profiles, profileInfo{Name: name, Default: name == manifest.DefaultProfile, Extends: manifest.Profiles[name].Extends})
			continue
		}
		profiles = append(profiles, profileInfo{
			Name: name, Default: name == manifest.DefaultProfile, Extends: manifest.Profiles[name].Extends,
			Components: resolvedComponentNames(resolved), Skipped: resolved.Skipped,
		})
	}
	return profiles
}

func listComponents(manifest *conenv.Manifest) []componentInfo {
	names := make([]string, 0, len(manifest.Components))
	for name := range manifest.Components {
		names = append(names, name)
	}
	sort.Strings(names)
	components := make([]componentInfo, 0, len(names))
	for _, name := range names {
		components = append(components, presentComponent(name, manifest.Components[name], false))
	}
	return components
}

func profileView(resolved *conenv.Resolved) *scopeView {
	if resolved == nil {
		return nil
	}
	return &scopeView{
		Profile:         resolved.Profile,
		Components:      resolvedComponentNames(resolved),
		Skipped:         resolved.Skipped,
		SelectionSource: resolved.SelectionSource,
	}
}

// ---------------------------------------------------------------------------
// apply
// ---------------------------------------------------------------------------

type applyOptions struct {
	repositoryFlags
	scopeFlags
	dryRun bool
	yes    bool
	save   bool
	write  string
	undo   bool
	list   bool
}

type applyCommandResult struct {
	Plan           *conenv.Plan      `json:"plan"`
	Applied        bool              `json:"applied"`
	Run            *conenv.RunRecord `json:"run,omitempty"`
	SavedSelection string            `json:"savedSelection,omitempty"`
}

func runApply(args []string) int {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	var options applyOptions
	options.repositoryFlags.register(fs)
	options.scopeFlags.register(fs)
	fs.BoolVar(&options.dryRun, "dry-run", false, "preview the plan without changing the machine")
	fs.BoolVar(&options.yes, "yes", false, "apply without prompting")
	fs.BoolVar(&options.save, "save", false, "save the scope as this machine's default selection")
	fs.StringVar(&options.write, "write", "", "write the selected scope to a selection file")
	fs.BoolVar(&options.undo, "undo", false, "undo a recorded run instead of applying")
	fs.BoolVar(&options.list, "list", false, "list recorded runs (requires --undo)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 1 {
		return fail(errors.New("usage: conenv apply [--undo [RUN_ID]]"), 2)
	}
	if err := options.validate(); err != nil {
		return fail(err, 2)
	}
	if options.undo {
		return undoApply(options, fs.Arg(0))
	}
	return applyChanges(options, fs.Arg(0))
}

func (options applyOptions) validate() error {
	if err := options.repositoryFlags.validate(); err != nil {
		return err
	}
	if options.undo {
		if options.selectComponents || len(options.only) > 0 || options.selection != "" || options.save || options.write != "" {
			return errors.New("--undo cannot be combined with --select, --only, --selection, --save, or --write")
		}
		return nil
	}
	if options.list {
		return errors.New("--list requires --undo")
	}
	if options.write != "" && !options.selectComponents {
		return errors.New("--write requires --select")
	}
	return options.scopeFlags.validate()
}

func applyChanges(options applyOptions, runID string) int {
	if runID != "" {
		return fail(errors.New("apply does not accept positional arguments; use --undo RUN_ID to undo a run"), 2)
	}
	resolved, origin, err := loadScope(options.repository(), options.scopeFlags, options.profile, options.selectComponents)
	if err != nil {
		return fail(err, 2)
	}
	var effective *conenv.SelectionSpec
	confirmed := false
	if options.selectComponents {
		output := io.Writer(os.Stdout)
		if options.json {
			output = os.Stderr
		}
		if canUseTUI(stdin, output) {
			result, err := runComponentTUI(stdin, output, resolved, !options.yes && !options.dryRun)
			if err != nil {
				return failSelection(err)
			}
			if err := filterComponents(resolved, result.SelectedIDs); err != nil {
				return failSelection(err)
			}
			effective, err = selectionFromIDs(resolved, result.SelectedIDs)
			if err != nil {
				return failSelection(err)
			}
			confirmed = result.Confirmed && !options.yes && !options.dryRun
		} else {
			if err := chooseComponentsLineMode(stdin, output, resolved); err != nil {
				return failSelection(err)
			}
			effective, err = selectionSpecFromResolved(resolved)
			if err != nil {
				return failSelection(err)
			}
		}
		if err := conenv.ValidateScope(resolved); err != nil {
			return fail(err, 2)
		}
	}
	plan, err := conenv.BuildPlan(resolved)
	if err != nil {
		return fail(err, 1)
	}
	result := applyCommandResult{Plan: plan}

	shouldSave := options.save
	if options.selectComponents && !options.save && !options.yes && !options.dryRun && isInteractive() {
		if confirm("Save this selection as this machine's default? [y/N] ") {
			shouldSave = true
		}
	}
	if shouldSave {
		if effective == nil {
			spec, specErr := conenv.SelectionFromTargets(resolved, resolvedComponentNames(resolved))
			if specErr != nil {
				return fail(fmt.Errorf("cannot derive the selected scope: %w", specErr), 2)
			}
			effective = spec
		}
		path, saveErr := conenv.SaveDefaultSelection(effective)
		if saveErr != nil {
			return fail(saveErr, 1)
		}
		result.SavedSelection = path
		if !options.json {
			fmt.Printf("saved selection to %s\n", path)
		}
	}
	if options.write != "" && effective != nil {
		absolute, writeErr := filepath.Abs(options.write)
		if writeErr != nil {
			return fail(writeErr, 1)
		}
		if writeErr := conenv.SaveSelection(absolute, effective); writeErr != nil {
			return fail(writeErr, 1)
		}
		if !options.json {
			fmt.Printf("wrote selection to %s\n", absolute)
		}
	}

	if options.dryRun {
		if options.json {
			if err := writeJSON(result); err != nil {
				return fail(err, 1)
			}
		} else {
			printPlan(plan, origin)
		}
		return 0
	}
	if !options.json {
		if err := printPlan(plan, origin); err != nil {
			return fail(err, 1)
		}
	}
	if len(plan.Actions) == 0 {
		if options.json {
			if err := writeJSON(result); err != nil {
				return fail(err, 1)
			}
		}
		return 0
	}
	if !options.yes && !confirmed {
		if !isInteractive() {
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
	result.Applied = true
	result.Run = run
	if options.json {
		if err := writeJSON(result); err != nil {
			return fail(err, 1)
		}
	} else {
		fmt.Printf("applied run %s\n", run.ID)
	}
	return 0
}

func undoApply(options applyOptions, requestedRun string) int {
	if options.list {
		return listRuns(options.json)
	}
	runID := requestedRun
	if runID == "" {
		latest, err := conenv.LatestRunID()
		if err != nil {
			return fail(err, 1)
		}
		runID = latest
	}
	state, err := conenv.LoadState()
	if err != nil {
		return fail(err, 1)
	}
	run, err := conenv.FindRun(state, runID)
	if err != nil {
		return fail(err, 2)
	}
	if options.profile != "" && options.profile != run.Profile {
		return fail(fmt.Errorf("run %s applied profile %q, not %q", runID, run.Profile, options.profile), 1)
	}
	plan, err := conenv.PlanRestore(runID)
	if err != nil {
		return fail(err, 1)
	}
	if options.dryRun {
		if options.json {
			if err := writeJSON(plan); err != nil {
				return fail(err, 1)
			}
		} else {
			printUndoPlan(run, plan)
		}
		return 0
	}
	if !options.yes {
		if !isInteractive() {
			return fail(errors.New("non-interactive apply --undo requires --yes"), 2)
		}
		printUndoPlan(run, plan)
		if !confirm("Undo this run and restore the previous state? [y/N] ") {
			return fail(errors.New("undo cancelled"), 1)
		}
	}
	if err := conenv.Restore(runID); err != nil {
		return fail(err, 1)
	}
	if options.json {
		if err := writeJSON(struct {
			Undone bool                `json:"undone"`
			Plan   *conenv.RestorePlan `json:"plan"`
		}{Undone: true, Plan: plan}); err != nil {
			return fail(err, 1)
		}
	} else {
		fmt.Printf("undid run %s\n", runID)
	}
	return 0
}

func listRuns(asJSON bool) int {
	state, err := conenv.LoadState()
	if err != nil {
		return fail(err, 1)
	}
	runs := append([]conenv.RunRecord(nil), state.Runs...)
	for left, right := 0, len(runs)-1; left < right; left, right = left+1, right-1 {
		runs[left], runs[right] = runs[right], runs[left]
	}
	if asJSON {
		if err := writeJSON(runs); err != nil {
			return fail(err, 1)
		}
		return 0
	}
	if len(runs) == 0 {
		fmt.Println("no recorded runs")
		return 0
	}
	for _, run := range runs {
		backup := "no backup"
		if conenv.RunBackupExists(run.ID) {
			backup = "backup available"
		}
		fmt.Printf("%s  %s  %s  %d change(s)  %s\n", run.ID, run.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), run.Profile, len(run.Changes), backup)
	}
	return 0
}

func printUndoPlan(run conenv.RunRecord, plan *conenv.RestorePlan) {
	fmt.Printf("run: %s\nprofile: %s\ncreated: %s\nchanges: %d\n", plan.RunID, plan.Profile, run.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), len(run.Changes))
	for _, action := range plan.Actions {
		fmt.Printf("- [%s] %s\n", action.Action, action.Target)
	}
	if len(plan.PackagesRetained) > 0 {
		fmt.Printf("packages retained (not removed): %s\n", strings.Join(plan.PackagesRetained, ", "))
	}
}

// ---------------------------------------------------------------------------
// status
// ---------------------------------------------------------------------------

type statusOptions struct {
	repositoryFlags
	scopeFlags
	check bool
}

func runStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	var options statusOptions
	options.repositoryFlags.register(fs)
	options.scopeFlags.register(fs)
	fs.BoolVar(&options.check, "check", false, "run health checks for the current scope")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		return fail(errors.New("status does not accept positional arguments"), 2)
	}
	if err := options.validate(); err != nil {
		return fail(err, 2)
	}
	resolved, origin, err := loadScope(options.repository(), options.scopeFlags, options.profile, true)
	if err != nil {
		return fail(err, 2)
	}
	report := conenv.InspectStatus(resolved)
	report.SelectionSource = scopeSourceLabel(origin)
	if options.check {
		results := conenv.Doctor(resolved)
		report.Checks = results
		report.Healthy = true
		for _, result := range results {
			report.Healthy = report.Healthy && result.Healthy
		}
	}
	if options.json {
		if err := writeJSON(report); err != nil {
			return fail(err, 1)
		}
	} else {
		printStatus(report, options.check)
	}
	if options.check && !report.Healthy {
		return 3
	}
	if !report.InSync {
		return 3
	}
	return 0
}

func (options statusOptions) validate() error {
	if err := options.repositoryFlags.validate(); err != nil {
		return err
	}
	return options.scopeFlags.validate()
}

func printStatus(report conenv.StatusReport, checked bool) {
	fmt.Printf("repository: %s\nprofile: %s\nplatform: %s\nscope: %s\nscope source: %s\n", report.Repository, report.Profile, report.Platform, strings.Join(report.Scope, ", "), report.SelectionSource)
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
	if checked {
		for _, result := range report.Checks {
			label := "ok"
			if !result.Healthy {
				label = "fail"
			}
			fmt.Printf("[%s] %s: %s\n", label, result.Name, strings.TrimSpace(result.Detail))
		}
	}
}

// ---------------------------------------------------------------------------
// shared helpers
// ---------------------------------------------------------------------------

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

// selectionSpecFromResolved derives a declarative selection from a narrowed
// resolved scope, so an interactive choice can be saved or exported.
func selectionSpecFromResolved(resolved *conenv.Resolved) (*conenv.SelectionSpec, error) {
	targets := make([]string, 0, len(resolved.Components))
	for _, component := range resolved.Components {
		if conenv.HasComponentResources(component.Component) {
			targets = append(targets, component.Name+"/resources")
		}
		if component.Codex != nil {
			for _, skill := range component.Codex.Skills {
				targets = append(targets, component.Name+"/skills/"+skill.Name)
			}
		}
	}
	if len(targets) == 0 {
		return nil, errors.New("the selected scope contains no selectable resources")
	}
	return conenv.SelectionFromTargets(resolved, targets)
}

func printPlan(plan *conenv.Plan, origin scopeOrigin) error {
	fmt.Printf("repository: %s\nprofile: %s\nplatform: %s\nscope: %s\nscope source: %s\n", plan.Repository, plan.Profile, plan.Platform, strings.Join(plan.Scope, ", "), scopeSourceLabel(origin))
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
	line, _ := bufio.NewReader(stdin).ReadString('\n')
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
  conenv config [DIR] [--unset | --check | --profiles | --components
                 | --profile-detail NAME | --component-detail NAME
                 | --unset-selection | --write FILE]
  conenv apply  [--dry-run] [--select | --selection FILE | --only TARGET...]
                [--save] [--write FILE] [--yes]
  conenv apply  --undo [RUN_ID] [--list] [--dry-run] [--yes]
  conenv status [--check] [--select | --selection FILE | --only TARGET...]

Common flags: [--repo DIR] [--profile NAME] [--json]

Scopes:
  A command uses the scope given on the command line. Otherwise it uses the
  selection saved in ~/.conenv/selection.yaml, and falls back to the whole
  profile resolved from the configuration repository. Save a scope with
  "conenv apply --select --save" or "conenv apply --only TARGET --yes --save",
  and clear it with "conenv config --unset-selection".

Exit status:
  0 success, 1 operational failure, 2 invalid input or configuration,
  3 the inspection found drift or an unhealthy check.`)
}
