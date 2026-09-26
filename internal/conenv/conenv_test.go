package conenv

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	if _, err := Apply(secondPlan); err != nil {
		t.Fatalf("second apply should update existing state: %v", err)
	}
	assertFileContent(t, target, "newer")
	if err := Restore(run.ID); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, target, "old")
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
