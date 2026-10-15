package conenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobalAdapterResolvesTheSharedSkillsDirectory(t *testing.T) {
	adapter, err := LookupAdapter("global")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if !adapter.SkillsOnly() {
		t.Fatal("the global harness owns no configuration file")
	}
	if got, want := adapter.DefaultSkillsDir(home), filepath.Join(home, ".agents", "skills"); got != want {
		t.Fatalf("skills dir = %s, want %s", got, want)
	}
	if keys := adapter.AllowedConfigKeys(); len(keys) != 0 {
		t.Fatalf("global manages no settings keys, got %v", keys)
	}
	resolved := &Resolved{Variables: map[string]string{"home": home}}
	if _, pathErr := AgentConfigPath(resolved, Agent{Name: "global", Harness: "global"}); pathErr == nil {
		t.Fatal("expected a configuration path to be refused")
	}
}

func TestGlobalAdapterRejectsConfigurationDeclarations(t *testing.T) {
	if err := ValidateAgent(Agent{Name: "global", Harness: "global"}); err != nil {
		t.Fatalf("a skills-only declaration must validate: %v", err)
	}
	declarations := []Agent{
		{Name: "global", Harness: "global", Config: map[string]any{"model": "example"}},
		{Name: "global", Harness: "global", MCPServers: map[string]MCP{"docs": {Type: "http", URL: "https://example.com/mcp"}}},
	}
	for _, agent := range declarations {
		err := ValidateAgent(agent)
		if err == nil || !strings.Contains(err.Error(), "skills only") {
			t.Fatalf("expected a skills-only rejection, got %v", err)
		}
	}

	adapter, lookupErr := LookupAdapter("global")
	if lookupErr != nil {
		t.Fatal(lookupErr)
	}
	resolved := &Resolved{Variables: map[string]string{}, Secrets: map[string]SecretRef{}}
	if _, mergeErr := adapter.Merge(map[string]any{}, map[string]any{"model": "example"}, nil, nil, resolved); mergeErr == nil {
		t.Fatal("expected Merge to refuse configuration")
	}
	if _, parseErr := adapter.Parse([]byte("{}")); parseErr == nil {
		t.Fatal("expected Parse to refuse a configuration document")
	}
	if _, serializeErr := adapter.Serialize(map[string]any{}); serializeErr == nil {
		t.Fatal("expected Serialize to refuse a configuration document")
	}
}

func TestGlobalPlanLinksSkillsWithoutConfiguration(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	source := filepath.Join(root, "shared-skill")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("skill"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved := &Resolved{
		Variables: map[string]string{"home": home}, Secrets: map[string]SecretRef{},
		Components: []NamedComponent{{Name: "ai", Component: Component{Agents: map[string]Agent{
			"global": {Name: "global", Harness: "global", Skills: []Skill{{Name: "shared-skill", Source: source}}},
		}}}},
	}
	plan, err := BuildPlan(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != "agent_skill" {
		t.Fatalf("unexpected plan: %#v", plan.Actions)
	}
	if want := filepath.Join(home, ".agents", "skills", "shared-skill"); plan.Actions[0].Target != want {
		t.Fatalf("skill target = %s, want %s", plan.Actions[0].Target, want)
	}

	resolved.Components[0].Agents["global"] = Agent{
		Name: "global", Harness: "global",
		MCPServers: map[string]MCP{"docs": {Type: "http", URL: "https://example.com/mcp"}},
	}
	if _, planErr := BuildPlan(resolved); planErr == nil || !strings.Contains(planErr.Error(), "skills only") {
		t.Fatalf("expected the planner to refuse MCP declarations, got %v", planErr)
	}
}
