package conenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestLookupAdapterRejectsUnknownHarness(t *testing.T) {
	if _, err := LookupAdapter("nope"); err == nil || !strings.Contains(err.Error(), "codex") {
		t.Fatalf("expected an unknown harness error listing codex, got %v", err)
	}
	adapter, err := LookupAdapter("codex")
	if err != nil {
		t.Fatal(err)
	}
	if adapter.Harness() != "codex" || len(adapter.AllowedConfigKeys()) == 0 {
		t.Fatalf("unexpected adapter: %#v", adapter)
	}
}

func TestAdaptersAreStable(t *testing.T) {
	list := Adapters()
	if len(list) == 0 {
		t.Fatal("no adapters registered")
	}
	for i := 1; i < len(list); i++ {
		if list[i-1].Harness() > list[i].Harness() {
			t.Fatalf("adapters are not sorted: %v", list)
		}
	}
}

func TestValidateAgentRejectsUnregisteredSettings(t *testing.T) {
	agent := Agent{Name: "codex", Harness: "codex", Config: map[string]any{"approval_policy": "on-request"}}
	if err := ValidateAgent(agent); err != nil {
		t.Fatal(err)
	}
	agent.Config["aprovall_policy"] = "on-request"
	if err := ValidateAgent(agent); err == nil || !strings.Contains(err.Error(), "aprovall_policy") {
		t.Fatalf("expected an unknown setting error, got %v", err)
	}

	agent = Agent{Name: "codex", Harness: "codex", MCPServers: map[string]MCP{"docs": {Type: "http"}}}
	if err := ValidateAgent(agent); err == nil {
		t.Fatal("expected an http MCP server without a url to fail")
	}
	agent = Agent{Name: "codex", Harness: "nope"}
	if err := ValidateAgent(agent); err == nil {
		t.Fatal("expected an unknown harness to fail")
	}
}

func TestAgentPathsDefaultToGlobalDirectories(t *testing.T) {
	home := t.TempDir()
	resolved := &Resolved{Variables: map[string]string{"home": home}}

	codex := Agent{Name: "codex", Harness: "codex"}
	configPath, err := AgentConfigPath(resolved, codex)
	if err != nil {
		t.Fatal(err)
	}
	if configPath != filepath.Join(home, ".codex", "config.toml") {
		t.Fatalf("config path = %s", configPath)
	}
	skillsDir, err := AgentSkillsDir(resolved, codex)
	if err != nil {
		t.Fatal(err)
	}
	if skillsDir != filepath.Join(home, ".agents", "skills") {
		t.Fatalf("skills dir = %s", skillsDir)
	}
}

func TestAgentPathsPreferDeclarationThenEnvironment(t *testing.T) {
	home := t.TempDir()
	resolved := &Resolved{Variables: map[string]string{"home": home}}
	declared := Agent{
		Name: "codex", Harness: "codex",
		ConfigPath: "{{ .home }}/custom/config.toml",
		SkillsDir:  "~/custom-skills",
	}
	configPath, err := AgentConfigPath(resolved, declared)
	if err != nil {
		t.Fatal(err)
	}
	if configPath != filepath.Join(home, "custom", "config.toml") {
		t.Fatalf("config path = %s", configPath)
	}
	skillsDir, err := AgentSkillsDir(resolved, declared)
	if err != nil {
		t.Fatal(err)
	}
	if skillsDir != filepath.Join(home, "custom-skills") {
		t.Fatalf("skills dir = %s", skillsDir)
	}

	t.Setenv("CONENV_AGENT_CODEX_CONFIG", filepath.Join(home, "env", "config.toml"))
	t.Setenv("CONENV_AGENT_CODEX_SKILLS_DIR", filepath.Join(home, "env-skills"))
	configPath, err = AgentConfigPath(resolved, Agent{Name: "codex", Harness: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if configPath != filepath.Join(home, "env", "config.toml") {
		t.Fatalf("environment override ignored: %s", configPath)
	}
	skillsDir, err = AgentSkillsDir(resolved, Agent{Name: "codex", Harness: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if skillsDir != filepath.Join(home, "env-skills") {
		t.Fatalf("environment override ignored: %s", skillsDir)
	}
}

func TestAgentPathsRejectRelativeDeclarations(t *testing.T) {
	resolved := &Resolved{Variables: map[string]string{"home": t.TempDir()}}
	if _, err := AgentConfigPath(resolved, Agent{Name: "codex", Harness: "codex", ConfigPath: "relative/config.toml"}); err == nil {
		t.Fatal("expected a relative config path to fail")
	}
	if _, err := AgentSkillsDir(resolved, Agent{Name: "codex", Harness: "codex", SkillsDir: "relative-skills"}); err == nil {
		t.Fatal("expected a relative skills dir to fail")
	}
}

func TestAgentNamesMustBeSafeDirectoryNames(t *testing.T) {
	if err := ValidateAgent(Agent{Name: "codex", Harness: "codex"}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	manifest := `schemaVersion: conenv/v2
defaultProfile: base
profiles:
  base:
    components: [ai]
components:
  ai:
    agents:
      "../escape":
        harness: codex
        skills:
          - { name: "../escape", source: source }
`
	if err := os.WriteFile(filepath.Join(root, "conenv.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(root); err == nil {
		t.Fatal("expected an unsafe agent name to be rejected")
	}
}

func TestAgentConfigMergesComponentsIntoOneFile(t *testing.T) {
	home := t.TempDir()
	resolved := &Resolved{
		Variables: map[string]string{"home": home}, Secrets: map[string]SecretRef{},
		Components: []NamedComponent{
			{Name: "one", Component: Component{Agents: map[string]Agent{
				"codex": {Name: "codex", Harness: "codex", Config: map[string]any{"approval_policy": "on-request"}},
			}}},
			{Name: "two", Component: Component{Agents: map[string]Agent{
				"codex": {Name: "codex", Harness: "codex", Config: map[string]any{"model": "example"}},
			}}},
		},
	}
	plan, err := BuildPlan(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 {
		t.Fatalf("expected exactly one agent config action, got %#v", plan.Actions)
	}
	var merged map[string]any
	if err := toml.Unmarshal(plan.Actions[0].Content, &merged); err != nil {
		t.Fatal(err)
	}
	if merged["approval_policy"] != "on-request" || merged["model"] != "example" {
		t.Fatalf("both components should contribute: %#v", merged)
	}
}

func TestAgentConfigRejectsConflictingComponentValues(t *testing.T) {
	resolved := &Resolved{
		Variables: map[string]string{"home": t.TempDir()}, Secrets: map[string]SecretRef{},
		Components: []NamedComponent{
			{Name: "one", Component: Component{Agents: map[string]Agent{
				"codex": {Name: "codex", Harness: "codex", Config: map[string]any{"model": "one"}},
			}}},
			{Name: "two", Component: Component{Agents: map[string]Agent{
				"codex": {Name: "codex", Harness: "codex", Config: map[string]any{"model": "two"}},
			}}},
		},
	}
	if _, err := BuildPlan(resolved); err == nil {
		t.Fatal("expected conflicting agent settings to fail")
	}
}

func TestAgentPlanLinksSharedSkillsOnce(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	source := filepath.Join(root, "skill")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("skill"), 0o600); err != nil {
		t.Fatal(err)
	}
	shared := map[string]Agent{
		"codex-a": {Name: "codex-a", Harness: "codex", Skills: []Skill{{Name: "shared", Source: source}}},
		"codex-b": {Name: "codex-b", Harness: "codex", Skills: []Skill{{Name: "shared", Source: source}}},
	}
	resolved := &Resolved{Variables: map[string]string{"home": home}, Components: []NamedComponent{
		{Name: "ai", Component: Component{Agents: shared}},
	}}
	// Two agent names that share one global skills directory are a conflict.
	if err := ValidateScope(resolved); err == nil {
		t.Fatal("expected two agents writing the same skill target to fail")
	}

	resolved.Components[0].Agents = map[string]Agent{
		"codex": {Name: "codex", Harness: "codex", Skills: []Skill{{Name: "shared", Source: source}}},
	}
	plan, err := BuildPlan(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != "agent_skill" {
		t.Fatalf("unexpected plan: %#v", plan.Actions)
	}
	expected := filepath.Join(home, ".agents", "skills", "shared")
	if plan.Actions[0].Target != expected {
		t.Fatalf("skill target = %s, want %s", plan.Actions[0].Target, expected)
	}
}

func TestAgentConfigOwnershipConflict(t *testing.T) {
	home := t.TempDir()
	resolved := &Resolved{Variables: map[string]string{"home": home}, Components: []NamedComponent{
		{Name: "one", Component: Component{Agents: map[string]Agent{
			"codex": {Name: "codex", Harness: "codex", Config: map[string]any{"model": "example"}},
		}}},
		{Name: "two", Component: Component{Agents: map[string]Agent{
			"codex": {Name: "codex", Harness: "codex", MCPServers: map[string]MCP{"docs": {Type: "http", URL: "https://example.com"}}},
		}}},
	}}
	if err := ValidateScope(resolved); err == nil || !strings.Contains(err.Error(), "managed by both") {
		t.Fatalf("expected an ownership conflict, got %v", err)
	}
}

// A skill that is already linked to the declared source is in sync, so a
// second apply is a no-op. This also covers links the planner did not create,
// such as a directory junction made by hand.
func TestAgentSkillAlreadyLinkedIsInSync(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	source := filepath.Join(root, "skill")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("skill"), 0o600); err != nil {
		t.Fatal(err)
	}
	skillsDir := filepath.Join(home, ".agents", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(skillsDir, "skill")
	if err := os.Symlink(source, target); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}
	resolved := &Resolved{
		Variables: map[string]string{"home": home},
		Components: []NamedComponent{{Name: "ai", Component: Component{Agents: map[string]Agent{
			"codex": {Name: "codex", Harness: "codex", Skills: []Skill{{Name: "skill", Source: source}}},
		}}}},
	}
	plan, err := BuildPlan(resolved)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range plan.Actions {
		if action.Kind == "agent_skill" {
			t.Fatalf("an existing matching link produced an action: %#v", action)
		}
	}
}
