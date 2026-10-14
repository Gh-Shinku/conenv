package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

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

// testAgent builds an agent declaration for presentation tests.
func testAgent(harness string, config map[string]any, mcp map[string]conenv.MCP, skills ...conenv.Skill) map[string]conenv.Agent {
	return map[string]conenv.Agent{harness: {Name: harness, Harness: harness, Config: config, MCPServers: mcp, Skills: skills}}
}

func TestChooseComponentsGroupsAndFilters(t *testing.T) {
	resolved := &conenv.Resolved{Profile: "personal", Components: []conenv.NamedComponent{
		{Name: "skills", Component: conenv.Component{Category: "ai", Description: "Shared skills", Agents: testAgent("codex", nil, nil)}},
		{Name: "shell", Component: conenv.Component{Category: "program", Description: "Shell config", Files: []conenv.ManagedFile{{Source: "shell", Target: "/shell", Mode: "link"}}}},
		{Name: "vim", Component: conenv.Component{Category: "program"}},
	}}
	var output bytes.Buffer
	if err := chooseComponents(strings.NewReader("n\n1,3\n\n"), &output, resolved); err != nil {
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
		{Name: "skills", Component: conenv.Component{Agents: testAgent("codex", nil, nil,
			conenv.Skill{Name: "project-bootstrap", Source: "project"},
			conenv.Skill{Name: "vibe-bootstrap", Source: "vibe"},
		)}},
		{Name: "shell", Component: conenv.Component{Files: []conenv.ManagedFile{{Source: "shell", Target: "/shell", Mode: "link"}}}},
		{Name: "vim"},
	}}
	catalog := newSelectionCatalog(resolved)
	var selected []string
	for _, item := range catalog.items {
		if item.Title == "vibe-bootstrap" || item.Title == "shell" {
			selected = append(selected, item.ID)
		}
	}
	filtered, err := filteredResolved(resolved, selected)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{filtered.Components[0].Name, filtered.Components[1].Name}; !reflect.DeepEqual(got, []string{"skills", "shell"}) {
		t.Fatalf("filtered order = %v", got)
	}
	if got := filtered.Components[0].Agents["codex"].Skills; len(got) != 1 || got[0].Name != "vibe-bootstrap" {
		t.Fatalf("filtered skills = %#v", got)
	}
	if len(resolved.Components) != 3 {
		t.Fatal("filtering a copy should not mutate the resolved profile")
	}
}

func TestChooseComponentsAllowsIndividualSkill(t *testing.T) {
	resolved := &conenv.Resolved{Profile: "personal", Components: []conenv.NamedComponent{
		{Name: "codex-skills", Component: conenv.Component{Category: "ai", Agents: testAgent("codex", nil, nil,
			conenv.Skill{Name: "project-bootstrap", Source: "project"},
			conenv.Skill{Name: "vibe-bootstrap", Source: "vibe"},
		)}},
	}}
	var output bytes.Buffer
	// The tree is component, agent, then one node per skill. Toggling the agent
	// off and a skill back on leaves a partially selected agent.
	if err := chooseComponents(strings.NewReader("2\n3\n\n"), &output, resolved); err != nil {
		t.Fatal(err)
	}
	if got := resolved.Components[0].Agents["codex"].Skills; len(got) != 1 || got[0].Name != "project-bootstrap" {
		t.Fatalf("partial selection = %#v", got)
	}
	if !strings.Contains(output.String(), "[~] 2. codex") {
		t.Fatalf("agent did not render a partial state: %s", output.String())
	}
}

func TestFilteredResolvedSeparatesResourcesFromSkills(t *testing.T) {
	component := conenv.NamedComponent{Name: "mixed", Component: conenv.Component{
		Packages: []conenv.Package{{Manager: "winget", ID: "Example.Tool"}},
		Checks:   []conenv.Check{{Name: "tool", Type: "command", Command: "tool"}},
		Agents: testAgent("codex",
			map[string]any{"model": "example"},
			map[string]conenv.MCP{"docs": {Type: "http", URL: "https://example.com"}},
			conenv.Skill{Name: "one", Source: "one"}, conenv.Skill{Name: "two", Source: "two"},
		),
	}}
	resolved := &conenv.Resolved{Profile: "personal", Components: []conenv.NamedComponent{component}}
	catalog := newSelectionCatalog(resolved)
	idsByTitle := map[string]string{}
	for _, item := range catalog.items {
		idsByTitle[item.Title] = item.ID
	}

	skillFiltered, err := filteredResolved(resolved, []string{idsByTitle["two"]})
	if err != nil {
		t.Fatal(err)
	}
	skillOnly := skillFiltered.Components[0]
	agent := skillOnly.Agents["codex"]
	if len(skillOnly.Packages) != 0 || len(skillOnly.Checks) != 0 || len(agent.Config) != 0 || len(agent.MCPServers) != 0 {
		t.Fatalf("skill-only selection retained component resources: %#v", skillOnly)
	}
	if len(agent.Skills) != 1 || agent.Skills[0].Name != "two" {
		t.Fatalf("skill-only selection = %#v", agent.Skills)
	}

	resourcesFiltered, err := filteredResolved(resolved, []string{idsByTitle["Agent resources"]})
	if err != nil {
		t.Fatal(err)
	}
	resourcesOnly := resourcesFiltered.Components[0]
	agent = resourcesOnly.Agents["codex"]
	if len(resourcesOnly.Packages) != 0 || len(resourcesOnly.Checks) != 0 {
		t.Fatalf("agent-only selection retained component resources: %#v", resourcesOnly)
	}
	if len(agent.Config) != 1 || len(agent.MCPServers) != 1 {
		t.Fatalf("resource selection lost agent resources: %#v", agent)
	}
	if len(agent.Skills) != 0 {
		t.Fatalf("resource selection retained skills: %#v", agent.Skills)
	}
}

func TestTUISelectionMatchesDeclarativeSelection(t *testing.T) {
	resolved := &conenv.Resolved{Profile: "personal", Components: []conenv.NamedComponent{{
		Name: "ai-agents", Component: conenv.Component{Agents: testAgent("codex",
			map[string]any{"model": "example"}, nil,
			conenv.Skill{Name: "one", Source: "one"}, conenv.Skill{Name: "two", Source: "two"},
		)},
	}}}
	catalog := newSelectionCatalog(resolved)
	var selectedIDs []string
	for _, item := range catalog.items {
		if item.Title == "Agent resources" || item.Title == "two" {
			selectedIDs = append(selectedIDs, item.ID)
		}
	}
	fromTUI, err := filteredResolved(resolved, selectedIDs)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := conenv.SelectionFromTargets(resolved, []string{"ai-agents/agents/codex/resources", "ai-agents/agents/codex/skills/two"})
	if err != nil {
		t.Fatal(err)
	}
	fromFile, err := conenv.ApplySelection(resolved, spec)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fromTUI.Components, fromFile.Components) {
		t.Fatalf("TUI selection %#v differs from declarative selection %#v", fromTUI.Components, fromFile.Components)
	}
}

// ---------------------------------------------------------------------------
// CLI surface
// ---------------------------------------------------------------------------

func registerTestEnvironment(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(conenv.RepositoryEnvironment, "")
	t.Setenv("CONENV_TEST_SECRET", "value")
	if err := os.MkdirAll(filepath.Join(home, ".conenv"), 0o700); err != nil {
		t.Fatal(err)
	}
	return home
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// testRepository creates a configuration repository whose declarations resolve
// without touching the network. Sources are relative to the repository root so
// the manifest stays valid even when the temporary path needs quoting.
func testRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "shell"), "shell\n")
	writeFile(t, filepath.Join(root, "vim"), "vim\n")
	skillDir := filepath.Join(root, "skills", "release-notes")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(skillDir, "SKILL.md"), "skill\n")
	manifest := `schemaVersion: conenv/v2
defaultProfile: base
profiles:
  base:
    components: [shell, vim, ai-agents]
components:
  shell:
    category: program
    description: Shell config
    files:
      - source: shell
        target: '{{ .home }}/shell'
        mode: render
  vim:
    category: program
    files:
      - source: vim
        target: '{{ .home }}/vim'
        mode: render
  ai-agents:
    category: ai
    description: Shared agent skills
    agents:
      codex:
        harness: codex
        config:
          approval_policy: on-request
        skills:
          - name: release-notes
            source: skills/release-notes
`
	writeFile(t, filepath.Join(root, "conenv.yaml"), manifest)
	return root
}

func runCLI(t *testing.T, input string, args ...string) (int, string, string) {
	t.Helper()
	previous := stdin
	stdin = strings.NewReader(input)
	defer func() { stdin = previous }()

	originalOut, originalErr := os.Stdout, os.Stderr
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errRead, errWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = outWrite, errWrite

	var wait sync.WaitGroup
	wait.Add(2)
	var out, errs bytes.Buffer
	go func() {
		defer wait.Done()
		_, _ = io.Copy(&out, outRead)
	}()
	go func() {
		defer wait.Done()
		_, _ = io.Copy(&errs, errRead)
	}()

	code := run(args)

	os.Stdout, os.Stderr = originalOut, originalErr
	outWrite.Close()
	errWrite.Close()
	wait.Wait()
	outRead.Close()
	errRead.Close()
	return code, out.String(), errs.String()
}

func TestUsageListsOnlyDailyCommands(t *testing.T) {
	code, _, stderr := runCLI(t, "", "help")
	if code != 0 {
		t.Fatalf("help exit code = %d", code)
	}
	for _, command := range []string{"conenv config", "conenv apply", "conenv status"} {
		if !strings.Contains(stderr, command) {
			t.Fatalf("usage does not mention %q:\n%s", command, stderr)
		}
	}
	for _, removed := range []string{"conenv validate", "conenv plan", "conenv doctor", "conenv restore", "conenv history", "conenv select", "conenv repo"} {
		if strings.Contains(stderr, removed) {
			t.Fatalf("usage still mentions removed command %q:\n%s", removed, stderr)
		}
	}
}

func TestUnknownCommandSuggestsReplacement(t *testing.T) {
	cases := map[string]string{
		"doctor":  "status --check",
		"plan":    "apply --dry-run",
		"restore": "apply --undo",
		"select":  "apply --select",
		"history": "apply --undo --list",
	}
	for command, replacement := range cases {
		code, _, stderr := runCLI(t, "", command)
		if code != 2 {
			t.Fatalf("%s exit code = %d, want 2", command, code)
		}
		if !strings.Contains(stderr, replacement) {
			t.Fatalf("%s stderr does not suggest %q:\n%s", command, replacement, stderr)
		}
	}
}

func TestConfigSavesAndReportsRepository(t *testing.T) {
	registerTestEnvironment(t)
	repository := testRepository(t)

	if code, stdout, stderr := runCLI(t, "", "config", repository); code != 0 {
		t.Fatalf("config DIR exit = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}

	code, stdout, stderr := runCLI(t, "", "config", "--json")
	if code != 0 {
		t.Fatalf("config --json exit = %d\nstderr: %s", code, stderr)
	}
	report := decodeReport(t, stdout)
	if report.Source != "user config" {
		t.Fatalf("source = %q, want user config\n%s", report.Source, stdout)
	}
	if report.Scope == nil || report.Scope.Profile != "base" || len(report.Scope.Components) != 3 {
		t.Fatalf("unexpected scope: %s", stdout)
	}
	if len(report.Scope.Agents) != 1 || report.Scope.Agents[0].Agent != "codex" || report.Scope.Agents[0].Harness != "codex" || report.Scope.Agents[0].Skills != 1 {
		t.Fatalf("scope did not report the declared agent: %s", stdout)
	}

	if code, stdout, stderr := runCLI(t, "", "config", "--profiles", "--components"); code != 0 {
		t.Fatalf("config --profiles --components exit = %d\nstderr: %s", code, stderr)
	} else if !strings.Contains(stdout, "base (default)") || !strings.Contains(stdout, "- shell") {
		t.Fatalf("unexpected listing:\n%s", stdout)
	}

	if code, stdout, stderr := runCLI(t, "", "config", "--profile-detail", "base"); code != 0 {
		t.Fatalf("config --profile-detail exit = %d\nstderr: %s", code, stderr)
	} else if !strings.Contains(stdout, "components: shell, vim, ai-agents") {
		t.Fatalf("unexpected profile detail: %s", stdout)
	}

	if code, stdout, stderr := runCLI(t, "", "config", "--component-detail", "vim"); code != 0 {
		t.Fatalf("config --component-detail exit = %d\nstderr: %s", code, stderr)
	} else if !strings.Contains(stdout, "files: 1") {
		t.Fatalf("unexpected component detail: %s", stdout)
	}

	if code, _, stderr := runCLI(t, "", "config", "--unset"); code != 0 {
		t.Fatalf("config --unset exit = %d\nstderr: %s", code, stderr)
	}
	// Without a saved repository conenv falls back to the working directory,
	// which is not a configuration repository.
	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(previousDirectory); err != nil {
			t.Fatal(err)
		}
	}()
	code, stdout, stderr = runCLI(t, "", "config", "--json")
	if code != 2 {
		t.Fatalf("config after --unset exit = %d, want 2\nstdout: %s", code, stdout)
	}
	if !strings.Contains(stderr, "conenv.yaml") {
		t.Fatalf("unexpected error after clearing the repository: %s", stderr)
	}
}

func TestConfigReportsInvalidManifest(t *testing.T) {
	registerTestEnvironment(t)
	repository := t.TempDir()
	writeFile(t, filepath.Join(repository, "conenv.yaml"), "schemaVersion: conenv/v3\nprofiles: {}\ncomponents: {}\n")
	code, _, stderr := runCLI(t, "", "config", "--repo", repository, "--check")
	if code != 2 {
		t.Fatalf("config --check exit = %d, want 2\nstderr: %s", code, stderr)
	}

	valid := testRepository(t)
	if code, stdout, stderr := runCLI(t, "", "config", "--repo", valid, "--check", "--json"); code != 0 {
		t.Fatalf("config --check exit = %d\nstderr: %s", code, stderr)
	} else {
		report := decodeReport(t, stdout)
		if report.Valid == nil || !*report.Valid {
			t.Fatalf("expected a valid report:\n%s", stdout)
		}
	}
}

func decodeReport(t *testing.T, stdout string) configReport {
	t.Helper()
	var report configReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode report: %v\n%s", err, stdout)
	}
	return report
}

func TestSavedSelectionDrivesStatusAndConfig(t *testing.T) {
	home := registerTestEnvironment(t)
	repository := testRepository(t)

	code, stdout, stderr := runCLI(t, "", "apply", "--repo", repository, "--only", "shell", "--dry-run", "--save")
	if code != 0 {
		t.Fatalf("apply --only --save exit = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	selectionPath := filepath.Join(home, ".conenv", "selection.yaml")
	if _, err := os.Stat(selectionPath); err != nil {
		t.Fatalf("saved selection missing: %v\n%s", err, stdout)
	}

	code, stdout, stderr = runCLI(t, "", "status", "--repo", repository, "--json")
	if code != 0 && code != 3 {
		t.Fatalf("status exit = %d\nstderr: %s", code, stderr)
	}
	var report conenv.StatusReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode status: %v\n%s", err, stdout)
	}
	if report.SelectionSource != "saved selection" {
		t.Fatalf("selection source = %q\n%s", report.SelectionSource, stdout)
	}
	if !reflect.DeepEqual(report.Scope, []string{"shell"}) {
		t.Fatalf("scope = %v, want [shell]", report.Scope)
	}

	if code, _, stderr := runCLI(t, "", "config", "--repo", repository, "--unset-selection"); code != 0 {
		t.Fatalf("config --unset-selection exit = %d\nstderr: %s", code, stderr)
	}
	code, stdout, _ = runCLI(t, "", "status", "--repo", repository, "--json")
	if code != 0 && code != 3 {
		t.Fatalf("status exit = %d", code)
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err)
	}
	if report.SelectionSource != "profile" || len(report.Scope) != 3 {
		t.Fatalf("scope did not fall back to the profile: %s", stdout)
	}
}

func TestConfigWriteRequiresSavedSelection(t *testing.T) {
	registerTestEnvironment(t)
	if code, _, stderr := runCLI(t, "", "config", "--write", "out.yaml"); code != 2 {
		t.Fatalf("config --write without a saved selection exit = %d\nstderr: %s", code, stderr)
	}
}

func TestApplyRejectsConflictingFlags(t *testing.T) {
	registerTestEnvironment(t)
	cases := [][]string{
		{"apply", "--select", "--only", "shell", "--dry-run"},
		{"apply", "--undo", "--select"},
		{"apply", "--undo", "--save"},
		{"apply", "--list"},
		{"apply", "--write", "out.yaml"},
		{"status", "--repo", "a", "--root", "b"},
	}
	for _, args := range cases {
		if code, _, _ := runCLI(t, "", args...); code != 2 {
			t.Fatalf("%v exit = %d, want 2", args, code)
		}
	}
}

func TestStatusCheckRunsHealthChecks(t *testing.T) {
	registerTestEnvironment(t)
	repository := t.TempDir()
	writeFile(t, filepath.Join(repository, "conenv.yaml"), `schemaVersion: conenv/v2
defaultProfile: base
profiles:
  base:
    components: [shell]
components:
  shell:
    checks:
      - name: home
        type: path
        path: '{{ .home }}'
`)
	code, stdout, stderr := runCLI(t, "", "status", "--repo", repository, "--check")
	if code != 0 {
		t.Fatalf("status --check exit = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "[ok] home") {
		t.Fatalf("health checks were not reported:\n%s", stdout)
	}
	if !strings.Contains(stdout, "scope source: profile") {
		t.Fatalf("scope source missing:\n%s", stdout)
	}
}

func TestApplySelectSavesScopeInLineMode(t *testing.T) {
	home := registerTestEnvironment(t)
	repository := testRepository(t)
	// Every component starts selected, so deselecting "vim" and "ai-agents"
	// leaves only "shell".
	code, stdout, stderr := runCLI(t, "n\n1\n\n", "apply", "--repo", repository, "--select", "--save", "--yes", "--dry-run")
	if code != 0 {
		t.Fatalf("apply --select exit = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	saved, err := os.ReadFile(filepath.Join(home, ".conenv", "selection.yaml"))
	if err != nil {
		t.Fatalf("saved selection missing: %v", err)
	}
	if !strings.Contains(string(saved), "shell") || strings.Contains(string(saved), "vim") {
		t.Fatalf("saved selection does not match the choice:\n%s", saved)
	}

	exported := filepath.Join(t.TempDir(), "selection.yaml")
	if code, _, stderr := runCLI(t, "n\n1\n\n", "apply", "--repo", repository, "--select", "--write", exported, "--yes", "--dry-run"); code != 0 {
		t.Fatalf("apply --select --write exit = %d\nstderr: %s", code, stderr)
	}
	if _, err := os.Stat(exported); err != nil {
		t.Fatalf("exported selection missing: %v", err)
	}
}

func TestApplyUndoListAndDryRun(t *testing.T) {
	registerTestEnvironment(t)
	if code, stdout, stderr := runCLI(t, "", "apply", "--undo", "--list"); code != 0 {
		t.Fatalf("apply --undo --list exit = %d\nstderr: %s", code, stderr)
	} else if !strings.Contains(stdout, "no recorded runs") {
		t.Fatalf("unexpected listing:\n%s", stdout)
	}
	if code, _, stderr := runCLI(t, "", "apply", "--undo", "--dry-run"); code != 1 {
		t.Fatalf("apply --undo --dry-run without runs exit = %d, want 1\nstderr: %s", code, stderr)
	}
	if code, _, stderr := runCLI(t, "", "apply", "--undo", "--yes"); code != 1 {
		t.Fatalf("apply --undo without runs exit = %d, want 1\nstderr: %s", code, stderr)
	}
}

func TestApplyUndoRequiresYesWithoutTerminal(t *testing.T) {
	home := registerTestEnvironment(t)
	writeRunState(t, home, "run-1", "base", nil)
	if code, _, stderr := runCLI(t, "", "apply", "--undo"); code != 2 {
		t.Fatalf("non-interactive apply --undo exit = %d, want 2\nstderr: %s", code, stderr)
	}
}

func TestApplyUndoRestoresBackup(t *testing.T) {
	home := registerTestEnvironment(t)
	backup := filepath.Join(home, ".conenv", "backups", "run-1")
	if err := os.MkdirAll(backup, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(backup, "original"), "original\n")
	target := filepath.Join(home, "shell")
	writeFile(t, target, "managed\n")
	writeRunState(t, home, "run-1", "base", []conenv.BackupRecord{{
		Target: target, Backup: filepath.Join(backup, "original"), Existed: true,
	}})

	code, stdout, stderr := runCLI(t, "", "apply", "--undo", "--dry-run")
	if code != 0 {
		t.Fatalf("apply --undo --dry-run exit = %d\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "run-1") || !strings.Contains(stdout, "[restore]") {
		t.Fatalf("unexpected undo preview:\n%s", stdout)
	}

	code, stdout, stderr = runCLI(t, "", "apply", "--undo", "--yes", "--json")
	if code != 0 {
		t.Fatalf("apply --undo --yes exit = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"undone": true`) {
		t.Fatalf("unexpected undo result:\n%s", stdout)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "original\n" {
		t.Fatalf("target was not restored: %q", data)
	}
	state, err := conenv.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Runs) != 0 {
		t.Fatalf("undone run is still listed: %#v", state.Runs)
	}
}

// requireSymlinks skips a test when the environment cannot create symbolic
// links, which is the default for an unprivileged Windows process.
func requireSymlinks(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}
}

func TestAgentLifecycleAppliesAndUndoesGlobalPaths(t *testing.T) {
	home := registerTestEnvironment(t)
	requireSymlinks(t)
	repository := testRepository(t)

	// The harness listing reports the adapter and where it writes by default.
	code, stdout, stderr := runCLI(t, "", "config", "--repo", repository, "--list-agents", "--json")
	if code != 0 {
		t.Fatalf("config --list-agents exit = %d\nstderr: %s", code, stderr)
	}
	report := decodeReport(t, stdout)
	if len(report.Adapters) != 1 || report.Adapters[0].Harness != "codex" {
		t.Fatalf("unexpected adapters: %s", stdout)
	}
	if report.Adapters[0].ConfigPath != filepath.Join(home, ".codex", "config.toml") {
		t.Fatalf("config path = %s", report.Adapters[0].ConfigPath)
	}
	if report.Adapters[0].SkillsDir != filepath.Join(home, ".agents", "skills") {
		t.Fatalf("skills dir = %s", report.Adapters[0].SkillsDir)
	}

	code, stdout, stderr = runCLI(t, "", "apply", "--repo", repository, "--only", "ai-agents/agents/codex", "--yes")
	if code != 0 {
		t.Fatalf("apply exit = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}

	configPath := filepath.Join(home, ".codex", "config.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("agent configuration was not written: %v", err)
	}
	if !strings.Contains(string(data), `approval_policy = 'on-request'`) {
		t.Fatalf("unexpected agent configuration:\n%s", data)
	}
	link := filepath.Join(home, ".agents", "skills", "release-notes")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("skill was not linked into the shared global directory: %v", err)
	}
	if filepath.Base(target) != "release-notes" {
		t.Fatalf("skill link points at %s", target)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "skills")); err == nil {
		t.Fatal("skills must not be linked into the harness private directory")
	}

	code, stdout, stderr = runCLI(t, "", "status", "--repo", repository, "--json")
	if code != 0 {
		t.Fatalf("status exit = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	var status conenv.StatusReport
	if err := json.Unmarshal([]byte(stdout), &status); err != nil {
		t.Fatal(err)
	}
	if !status.InSync {
		t.Fatalf("scope is not in sync after apply: %s", stdout)
	}

	code, stdout, stderr = runCLI(t, "", "apply", "--repo", repository, "--undo", "--yes")
	if code != 0 {
		t.Fatalf("apply --undo exit = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("skill link survived undo: %v", err)
	}
	if _, err := os.Lstat(configPath); !os.IsNotExist(err) {
		t.Fatalf("agent configuration created by apply survived undo: %v", err)
	}
}

func TestAgentConfigPreservesUnmanagedKeys(t *testing.T) {
	home := registerTestEnvironment(t)
	repository := testRepository(t)
	configPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, configPath, "model = \"keep-me\"\n[mcp_servers.existing]\nurl = \"https://example.com\"\n")

	if code, stdout, stderr := runCLI(t, "", "apply", "--repo", repository, "--only", "ai-agents/agents/codex/resources", "--yes"); code != 0 {
		t.Fatalf("apply exit = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "keep-me") || !strings.Contains(text, "example.com") {
		t.Fatalf("unmanaged settings were dropped:\n%s", text)
	}
	if !strings.Contains(text, "approval_policy") {
		t.Fatalf("declared settings were not merged:\n%s", text)
	}
}

func TestRemovedCodexBlockIsRejected(t *testing.T) {
	registerTestEnvironment(t)
	repository := t.TempDir()
	writeFile(t, filepath.Join(repository, "conenv.yaml"), `schemaVersion: conenv/v1
defaultProfile: base
profiles:
  base:
    components: [codex-skills]
components:
  codex-skills:
    codex:
      skills: []
`)
	code, _, stderr := runCLI(t, "", "config", "--repo", repository, "--check")
	if code != 2 {
		t.Fatalf("config --check exit = %d, want 2\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "codex") {
		t.Fatalf("expected a removed field error, got: %s", stderr)
	}

	writeFile(t, filepath.Join(repository, "conenv.yaml"), `schemaVersion: conenv/v3
defaultProfile: base
profiles:
  base:
    components: []
components: {}
`)
	code, _, stderr = runCLI(t, "", "config", "--repo", repository, "--check")
	if code != 2 {
		t.Fatalf("config --check exit = %d, want 2\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "conenv/v2") {
		t.Fatalf("expected a schema version error, got: %s", stderr)
	}
}

// writeRunState records a run in the user state file and writes the run record
// that undo reads from the backup directory.
func writeRunState(t *testing.T, home, runID, profile string, changes []conenv.BackupRecord) {
	t.Helper()
	run := conenv.RunRecord{ID: runID, Profile: profile, CreatedAt: time.Now().UTC(), Changes: changes}
	state := conenv.State{ActiveProfile: profile, LastRun: runID, Runs: []conenv.RunRecord{run}}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, ".conenv", "state.json"), string(data)+"\n")

	runData, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	runPath := filepath.Join(home, ".conenv", "backups", runID, "run.json")
	if err := os.MkdirAll(filepath.Dir(runPath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, runPath, string(runData)+"\n")
}
