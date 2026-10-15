package conenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPiAdapterUsesPrivateDirectories(t *testing.T) {
	adapter, err := LookupAdapter("pi")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if got, want := adapter.DefaultConfigPath(home), filepath.Join(home, ".pi", "agent", "mcp.json"); got != want {
		t.Fatalf("config path = %s, want %s", got, want)
	}
	if got, want := adapter.DefaultSkillsDir(home), filepath.Join(home, ".pi", "agent", "skills"); got != want {
		t.Fatalf("skills dir = %s, want %s", got, want)
	}
	if keys := adapter.AllowedConfigKeys(); len(keys) != 0 {
		t.Fatalf("pi manages no settings keys, got %v", keys)
	}
}

func TestPiRejectsSettingsKeys(t *testing.T) {
	agent := Agent{Name: "pi", Harness: "pi", Config: map[string]any{"model": "example"}}
	err := ValidateAgent(agent)
	if err == nil || !strings.Contains(err.Error(), "mcpServers") {
		t.Fatalf("expected a settings rejection pointing at mcpServers, got %v", err)
	}

	adapter, lookupErr := LookupAdapter("pi")
	if lookupErr != nil {
		t.Fatal(lookupErr)
	}
	if _, mergeErr := adapter.Merge(map[string]any{}, agent.Config, nil, nil, &Resolved{Variables: map[string]string{}, Secrets: map[string]SecretRef{}}); mergeErr == nil {
		t.Fatal("expected Merge to reject settings keys")
	}
	if err := ValidateAgent(Agent{Name: "pi", Harness: "pi", MCPServers: map[string]MCP{"docs": {Type: "http", URL: "https://example.com/mcp"}}}); err != nil {
		t.Fatalf("a pi MCP declaration must validate: %v", err)
	}
}

func TestPiMergePreservesUnmanagedMCPEntries(t *testing.T) {
	home := t.TempDir()
	piDir := filepath.Join(home, ".pi", "agent")
	if err := os.MkdirAll(piDir, 0o700); err != nil {
		t.Fatal(err)
	}
	existing := `{
  "mcpServers": {
    "garbro": {
      "command": "npx",
      "args": ["-y", "garbro-mcp@latest"],
      "exposure": "direct"
    }
  }
}
`
	if err := os.WriteFile(filepath.Join(piDir, "mcp.json"), []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_MCP_TOOL_PATH", "/tools/mcp")
	t.Setenv("PI_MCP_TOKEN", "s3cret")

	resolved := &Resolved{
		Variables: map[string]string{"home": home},
		Secrets: map[string]SecretRef{
			"tool_path": {Env: "PI_MCP_TOOL_PATH"},
			"api_token": {Env: "PI_MCP_TOKEN"},
		},
		Components: []NamedComponent{{Name: "ai", Component: Component{Agents: map[string]Agent{
			"pi": {
				Name: "pi", Harness: "pi",
				MCPServers: map[string]MCP{
					"garbro": {Type: "stdio", Command: "npx", Args: []string{"-y", "garbro-mcp@latest"}},
					"docs":   {Type: "http", URL: "https://example.com/mcp"},
					"local":  {Type: "stdio", Command: `{{ secret "tool_path" }}`, Env: map[string]string{"TOKEN": `{{ secret "api_token" }}`}},
				},
			},
		}}}},
	}
	plan, err := BuildPlan(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != "agent_config" {
		t.Fatalf("unexpected actions: %#v", plan.Actions)
	}
	if !plan.Actions[0].Sensitive {
		t.Fatal("agent configuration must be treated as sensitive")
	}
	var result map[string]any
	if err := json.Unmarshal(plan.Actions[0].Content, &result); err != nil {
		t.Fatal(err)
	}
	servers, ok := result["mcpServers"].(map[string]any)
	if !ok || len(servers) != 3 {
		t.Fatalf("expected three MCP servers, got %#v", result["mcpServers"])
	}
	garbro, ok := servers["garbro"].(map[string]any)
	if !ok {
		t.Fatalf("garbro entry missing: %#v", servers["garbro"])
	}
	if garbro["exposure"] != "direct" {
		t.Fatalf("unmanaged key was dropped: %#v", garbro)
	}
	docs, ok := servers["docs"].(map[string]any)
	if !ok || docs["url"] != "https://example.com/mcp" {
		t.Fatalf("http server not rendered: %#v", servers["docs"])
	}
	local, ok := servers["local"].(map[string]any)
	if !ok || local["command"] != "/tools/mcp" {
		t.Fatalf("secret expansion failed: %#v", servers["local"])
	}
	env, ok := local["env"].(map[string]any)
	if !ok || env["TOKEN"] != "s3cret" {
		t.Fatalf("env expansion failed: %#v", local["env"])
	}
}

func TestPiMergeSwitchesTransport(t *testing.T) {
	adapter, err := LookupAdapter("pi")
	if err != nil {
		t.Fatal(err)
	}
	resolved := &Resolved{Variables: map[string]string{}, Secrets: map[string]SecretRef{}}
	doc := map[string]any{"mcpServers": map[string]any{
		"server": map[string]any{"command": "npx", "args": []any{"-y", "old-mcp"}},
	}}
	merged, err := adapter.Merge(doc, nil, map[string]MCP{"server": {Type: "http", URL: "https://example.com/mcp"}}, nil, resolved)
	if err != nil {
		t.Fatal(err)
	}
	entry := merged.(map[string]any)["mcpServers"].(map[string]any)["server"].(map[string]any)
	if entry["url"] != "https://example.com/mcp" {
		t.Fatalf("url not rendered: %#v", entry)
	}
	if _, stale := entry["command"]; stale {
		t.Fatalf("stale stdio command kept: %#v", entry)
	}
	if _, stale := entry["args"]; stale {
		t.Fatalf("stale stdio args kept: %#v", entry)
	}

	doc = map[string]any{"mcpServers": map[string]any{
		"server": map[string]any{"url": "https://example.com/mcp"},
	}}
	merged, err = adapter.Merge(doc, nil, map[string]MCP{"server": {Type: "stdio", Command: "npx"}}, nil, resolved)
	if err != nil {
		t.Fatal(err)
	}
	entry = merged.(map[string]any)["mcpServers"].(map[string]any)["server"].(map[string]any)
	if entry["command"] != "npx" {
		t.Fatalf("command not rendered: %#v", entry)
	}
	if _, stale := entry["url"]; stale {
		t.Fatalf("stale http url kept: %#v", entry)
	}
}

func TestPiSerializeParsesBack(t *testing.T) {
	adapter, err := LookupAdapter("pi")
	if err != nil {
		t.Fatal(err)
	}
	resolved := &Resolved{Variables: map[string]string{}, Secrets: map[string]SecretRef{}}
	merged, err := adapter.Merge(map[string]any{}, nil, map[string]MCP{"docs": {Type: "http", URL: "https://example.com/mcp"}}, nil, resolved)
	if err != nil {
		t.Fatal(err)
	}
	data, err := adapter.Serialize(merged)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("serialized config must end with a newline: %q", data)
	}
	parsed, err := adapter.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	servers, ok := parsed.(map[string]any)["mcpServers"].(map[string]any)
	if !ok || len(servers) != 1 {
		t.Fatalf("round trip lost the server: %#v", parsed)
	}
}
