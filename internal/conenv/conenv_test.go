package conenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestLoadResolvesLayeredProfile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "dotfile"), []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONENV_TEST_SECRET", "value")
	manifest := `schemaVersion: conenv/v1
defaultProfile: workstation
variables:
  editor: vim
secrets:
  token:
    env: CONENV_TEST_SECRET
profiles:
  base:
    components: [shell]
    variables:
      editor: nvim
  workstation:
    extends: [base]
    components: [codex]
components:
  shell:
    files:
      - source: dotfile
        target: '{{ .home }}/.testrc'
        mode: render
  codex:
    platforms: [` + runtime.GOOS + `]
`
	if err := os.WriteFile(filepath.Join(root, "conenv.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Profile != "workstation" || resolved.Variables["editor"] != "nvim" {
		t.Fatalf("unexpected resolution: %#v", resolved)
	}
	if len(resolved.Components) != 2 || resolved.Components[0].Name != "shell" || resolved.Components[1].Name != "codex" {
		t.Fatalf("unexpected components: %#v", resolved.Components)
	}
}

func TestRepositoryResolutionPrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	repositoryOne := createTestRepository(t)
	repositoryTwo := createTestRepository(t)

	if _, err := SaveRepository(repositoryOne); err != nil {
		t.Fatal(err)
	}
	selection, err := ResolveRepository("")
	if err != nil {
		t.Fatal(err)
	}
	if selection.Path != repositoryOne || selection.Source != "user config" {
		t.Fatalf("unexpected user selection: %#v", selection)
	}

	t.Setenv(RepositoryEnvironment, repositoryTwo)
	selection, err = ResolveRepository("")
	if err != nil {
		t.Fatal(err)
	}
	if selection.Path != repositoryTwo || selection.Source != RepositoryEnvironment {
		t.Fatalf("unexpected environment selection: %#v", selection)
	}

	selection, err = ResolveRepository(repositoryOne)
	if err != nil {
		t.Fatal(err)
	}
	if selection.Path != repositoryOne || selection.Source != "flag" {
		t.Fatalf("unexpected explicit selection: %#v", selection)
	}
	if err := ClearRepository(); err != nil {
		t.Fatal(err)
	}
	config, err := loadUserConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.Repository != "" {
		t.Fatalf("repository was not cleared: %#v", config)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	root := t.TempDir()
	manifest := "schemaVersion: conenv/v1\nprofiles: {}\ncomponents: {}\nsurprise: true\n"
	if err := os.WriteFile(filepath.Join(root, "conenv.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, ""); err == nil || !strings.Contains(err.Error(), "surprise") {
		t.Fatalf("expected unknown-field error, got %v", err)
	}
}

func TestLoadRejectsCycleInUnselectedProfile(t *testing.T) {
	root := t.TempDir()
	manifest := `schemaVersion: conenv/v1
defaultProfile: good
profiles:
  good: {}
  first:
    extends: [second]
  second:
    extends: [first]
components: {}
`
	if err := os.WriteFile(filepath.Join(root, "conenv.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, ""); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestLoadProfileTracksUnsupportedComponents(t *testing.T) {
	root := t.TempDir()
	manifest := `schemaVersion: conenv/v1
defaultProfile: base
profiles:
  base:
    components: [supported, skipped]
components:
  supported: {}
  skipped:
    platforms: [definitely-not-a-real-platform]
`
	if err := os.WriteFile(filepath.Join(root, "conenv.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := LoadProfile(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Components) != 1 || resolved.Components[0].Name != "supported" || !reflect.DeepEqual(resolved.Skipped, []string{"skipped"}) {
		t.Fatalf("unexpected platform resolution: components=%#v skipped=%#v", resolved.Components, resolved.Skipped)
	}
}

func TestExpandStringDoesNotExpandDollarInSecret(t *testing.T) {
	t.Setenv("CONENV_DOLLAR_SECRET", "pa$$word")
	got, err := expandString(`token={{ secret "token" }}`, map[string]string{}, map[string]SecretRef{"token": {Env: "CONENV_DOLLAR_SECRET"}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "token=pa$$word" {
		t.Fatalf("secret changed during expansion: %q", got)
	}
}

func TestApplyAndRestoreRenderedFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	target := filepath.Join(home, "managed.txt")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan := &Plan{Profile: "test", Actions: []Action{{
		Kind: "render", Component: "test", Target: target, Content: []byte("new"), Summary: "render test",
	}}}
	run, err := Apply(plan)
	if err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, target, "new")
	secondPlan := &Plan{Profile: "test", Actions: []Action{{
		Kind: "render", Component: "test", Target: target, Content: []byte("newer"), Summary: "render test again",
	}}}
	secondRun, err := Apply(secondPlan)
	if err != nil {
		t.Fatalf("second apply should update existing state: %v", err)
	}
	assertFileContent(t, target, "newer")
	if err := Restore(secondRun.ID); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, target, "new")
	restorePlan, err := PlanRestore(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(restorePlan.Actions) != 1 || restorePlan.Actions[0].Action != "restore" || restorePlan.Actions[0].Target != target {
		t.Fatalf("unexpected restore plan: %#v", restorePlan)
	}
	if err := Restore(run.ID); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, target, "old")
	state, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.LastRun != "" || len(state.Restores) != 2 || state.Restores[1].RunID != run.ID {
		t.Fatalf("restore did not update state: %#v", state)
	}
}

func TestBuildPlanIsIdempotentAndRedactsContent(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	source := filepath.Join(root, "template")
	target := filepath.Join(home, "output")
	t.Setenv("CONENV_PLAN_SECRET", "do-not-print")
	if err := os.WriteFile(source, []byte(`value={{ secret "token" }}`), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved := &Resolved{
		Root: root, Profile: "test", Variables: map[string]string{"home": home},
		Secrets:    map[string]SecretRef{"token": {Env: "CONENV_PLAN_SECRET"}},
		Components: []NamedComponent{{Name: "files", Component: Component{Files: []ManagedFile{{Source: source, Target: target, Mode: "render"}}}}},
	}
	plan, err := BuildPlan(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 || !plan.Actions[0].Sensitive {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "do-not-print") {
		t.Fatalf("JSON plan leaked secret: %s", encoded)
	}
	if err := os.WriteFile(target, plan.Actions[0].Content, 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := BuildPlan(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Actions) != 0 {
		t.Fatalf("expected no-op second plan, got %#v", second.Actions)
	}
}

func TestInspectStatusReportsEachComponent(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	source := filepath.Join(root, "template")
	if err := os.WriteFile(source, []byte("desired"), 0o600); err != nil {
		t.Fatal(err)
	}
	inSyncTarget := filepath.Join(home, "in-sync")
	if err := os.WriteFile(inSyncTarget, []byte("desired"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved := &Resolved{
		Root: root, Profile: "test", Variables: map[string]string{"home": home},
		Components: []NamedComponent{
			{Name: "ready", Component: Component{Files: []ManagedFile{{Source: source, Target: inSyncTarget, Mode: "render"}}}},
			{Name: "drifted", Component: Component{Files: []ManagedFile{{Source: source, Target: filepath.Join(home, "missing"), Mode: "render"}}}},
			{Name: "broken", Component: Component{Files: []ManagedFile{{Source: filepath.Join(root, "absent"), Target: filepath.Join(home, "broken"), Mode: "render"}}}},
		},
	}
	report := InspectStatus(resolved)
	if report.InSync || len(report.Components) != 3 || report.Components[0].State != "in_sync" || report.Components[1].State != "drifted" || report.Components[1].Pending != 1 || report.Components[2].State != "error" {
		t.Fatalf("unexpected status report: %#v", report)
	}
}

func TestCodexMergePreservesUnmanagedSettings(t *testing.T) {
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte("model = \"keep-me\"\n[mcp_servers.existing]\nurl = \"https://example.com\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved := &Resolved{
		Variables: map[string]string{"home": home}, Secrets: map[string]SecretRef{},
		Components: []NamedComponent{{Name: "ai", Component: Component{Codex: &Codex{
			Config:     map[string]any{"approval_policy": "on-request"},
			MCPServers: map[string]MCP{"docs": {Type: "http", URL: "https://developers.openai.com/mcp"}},
		}}}},
	}
	data, _, err := desiredCodexConfig(resolved)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := toml.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result["model"] != "keep-me" || result["approval_policy"] != "on-request" {
		t.Fatalf("unmanaged or managed settings missing: %#v", result)
	}
	servers := result["mcp_servers"].(map[string]any)
	if len(servers) != 2 {
		t.Fatalf("expected both MCP servers, got %#v", servers)
	}
}

func assertFileContent(t *testing.T, path, expected string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != expected {
		t.Fatalf("%s: got %q, want %q", path, data, expected)
	}
}

func createTestRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	manifest := "schemaVersion: conenv/v1\ndefaultProfile: base\nprofiles:\n  base: {}\ncomponents: {}\n"
	if err := os.WriteFile(filepath.Join(root, "conenv.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(root)
}
