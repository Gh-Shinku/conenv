package conenv

import (
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func dshAdapterForTest(t *testing.T) AgentAdapter {
	t.Helper()
	adapter, err := LookupAdapter("dsh")
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func dshParseDocument(t *testing.T, data []byte) *yaml.Node {
	t.Helper()
	root := &yaml.Node{}
	if err := yaml.Unmarshal(data, root); err != nil {
		t.Fatalf("parse serialized DSH patch: %v\n%s", err, data)
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) != 1 || root.Content[0].Kind != yaml.SequenceNode {
		t.Fatalf("serialized DSH patch is not a sequence:\n%s", data)
	}
	return root
}

func dshEntry(t *testing.T, root *yaml.Node, id string) *yaml.Node {
	t.Helper()
	for _, entry := range root.Content[0].Content {
		if entry.Kind == yaml.MappingNode && nodeValue(mappingValue(entry, "id")) == id {
			return entry
		}
	}
	t.Fatalf("entry %q not found", id)
	return nil
}

func dshResolved() *Resolved {
	return &Resolved{Variables: map[string]string{}, Secrets: map[string]SecretRef{}}
}

func TestDSHAdapterUsesPrivateDirectories(t *testing.T) {
	adapter := dshAdapterForTest(t)
	home := t.TempDir()
	if got, want := adapter.DefaultConfigPath(home), filepath.Join(home, ".dsh", "cordis.patch.yml"); got != want {
		t.Fatalf("config path = %s, want %s", got, want)
	}
	if got, want := adapter.DefaultSkillsDir(home), filepath.Join(home, ".dsh", "skills"); got != want {
		t.Fatalf("skills dir = %s, want %s", got, want)
	}
	if keys := adapter.AllowedConfigKeys(); len(keys) != 0 {
		t.Fatalf("dsh manages no settings keys, got %v", keys)
	}
	if adapter.SkillsOnly() {
		t.Fatal("dsh owns a configuration file and must not be skills-only")
	}
}

func TestDSHRejectsSettingsKeys(t *testing.T) {
	agent := Agent{Name: "dsh", Harness: "dsh", Config: map[string]any{"model": "deepseek-flash"}}
	err := ValidateAgent(agent)
	if err == nil || !strings.Contains(err.Error(), "mcpServers") {
		t.Fatalf("expected a settings rejection pointing at mcpServers, got %v", err)
	}

	adapter := dshAdapterForTest(t)
	if _, mergeErr := adapter.Merge(dshEmptyPatch(), agent.Config, nil, nil, dshResolved()); mergeErr == nil {
		t.Fatal("expected Merge to reject settings keys")
	}
	if err := ValidateAgent(Agent{Name: "dsh", Harness: "dsh", MCPServers: map[string]MCP{"docs": {Type: "http", URL: "https://example.com/mcp"}}}); err != nil {
		t.Fatalf("a dsh MCP declaration must validate: %v", err)
	}
}

func TestDSHMergePreservesUnmanagedEntriesAndKeys(t *testing.T) {
	adapter := dshAdapterForTest(t)
	existing := `# hand-managed profile entries and expressions survive
- id: agent-default-model
  name: "@deepseek-ai/dsh-agent-default-model"
  config:
    provider: !!js process.env.PROVIDER
    model: deepseek-flash
- id: mcp-garbro
  name: "@deepseek-ai/dsh-mcp-client"
  config:
    serverName: garbro
    transport: stdio
    command: npx
    args: ["-y", "garbro-mcp@latest"]
    exposure: direct
    env:
      TOKEN: !!js process.env.GARBRO_TOKEN
`
	document, err := adapter.Parse([]byte(existing))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DSH_MCP_TOOL_PATH", "/tools/mcp")
	t.Setenv("DSH_MCP_TOKEN", "s3cret")
	resolved := &Resolved{
		Variables: map[string]string{},
		Secrets: map[string]SecretRef{
			"tool_path": {Env: "DSH_MCP_TOOL_PATH"},
			"api_token": {Env: "DSH_MCP_TOKEN"},
		},
	}
	merged, err := adapter.Merge(document, nil, map[string]MCP{
		"garbro": {Type: "stdio", Command: "npx", Args: []string{"-y", "garbro-mcp@latest"}},
		"docs":   {Type: "http", URL: "https://example.com/mcp"},
		"local":  {Type: "stdio", Command: `{{ secret "tool_path" }}`, Env: map[string]string{"TOKEN": `{{ secret "api_token" }}`}},
	}, nil, resolved)
	if err != nil {
		t.Fatal(err)
	}
	data, err := adapter.Serialize(merged)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "!!js process.env.PROVIDER") {
		t.Fatalf("custom tag on an unrelated entry was lost:\n%s", data)
	}
	root := dshParseDocument(t, data)

	if got := len(root.Content[0].Content); got != 4 {
		t.Fatalf("expected four entries, got %d:\n%s", got, data)
	}
	garbro := dshEntry(t, root, "mcp-garbro")
	config := mappingValue(garbro, "config")
	if nodeValue(mappingValue(config, "exposure")) != "direct" {
		t.Fatalf("unmanaged config key was dropped: %s", data)
	}
	token := mappingValue(mappingValue(config, "env"), "TOKEN")
	if token == nil || token.Tag != "!!js" || token.Value != "process.env.GARBRO_TOKEN" {
		t.Fatalf("custom tag inside an untouched config key should survive: %s", data)
	}
	docs := mappingValue(dshEntry(t, root, "mcp-docs"), "config")
	if nodeValue(mappingValue(docs, "transport")) != "streamable-http" {
		t.Fatalf("http transport not rendered: %s", data)
	}
	if nodeValue(mappingValue(docs, "url")) != "https://example.com/mcp" {
		t.Fatalf("http url not rendered: %s", data)
	}
	localConfig := mappingValue(dshEntry(t, root, "mcp-local"), "config")
	if nodeValue(mappingValue(localConfig, "command")) != "/tools/mcp" {
		t.Fatalf("secret expansion failed: %s", data)
	}
	if nodeValue(mappingValue(mappingValue(localConfig, "env"), "TOKEN")) != "s3cret" {
		t.Fatalf("env secret expansion failed: %s", data)
	}
}

func TestDSHMergeSwitchesTransport(t *testing.T) {
	adapter := dshAdapterForTest(t)
	document, err := adapter.Parse([]byte(`- id: mcp-server
  name: "@deepseek-ai/dsh-mcp-client"
  config:
    serverName: server
    transport: stdio
    command: npx
    args: ["-y", "old-mcp"]
    headers:
      Authorization: Bearer stale
    custom: keep
`))
	if err != nil {
		t.Fatal(err)
	}
	merged, err := adapter.Merge(document, nil, map[string]MCP{"server": {Type: "http", URL: "https://example.com/mcp"}}, nil, dshResolved())
	if err != nil {
		t.Fatal(err)
	}
	data, err := adapter.Serialize(merged)
	if err != nil {
		t.Fatal(err)
	}
	config := mappingValue(dshEntry(t, dshParseDocument(t, data), "mcp-server"), "config")
	if nodeValue(mappingValue(config, "transport")) != "streamable-http" || nodeValue(mappingValue(config, "url")) != "https://example.com/mcp" {
		t.Fatalf("http shape not rendered:\n%s", data)
	}
	for _, stale := range []string{"command", "args", "env"} {
		if mappingValue(config, stale) != nil {
			t.Fatalf("stale stdio key %q kept:\n%s", stale, data)
		}
	}
	if nodeValue(mappingValue(config, "custom")) != "keep" {
		t.Fatalf("unknown key dropped:\n%s", data)
	}

	document, err = adapter.Parse([]byte(`- id: mcp-server
  name: "@deepseek-ai/dsh-mcp-client"
  config:
    serverName: server
    transport: streamable-http
    url: https://example.com/mcp
    headers:
      Authorization: Bearer stale
    custom: keep
`))
	if err != nil {
		t.Fatal(err)
	}
	merged, err = adapter.Merge(document, nil, map[string]MCP{"server": {Type: "stdio", Command: "npx"}}, nil, dshResolved())
	if err != nil {
		t.Fatal(err)
	}
	data, err = adapter.Serialize(merged)
	if err != nil {
		t.Fatal(err)
	}
	config = mappingValue(dshEntry(t, dshParseDocument(t, data), "mcp-server"), "config")
	if nodeValue(mappingValue(config, "transport")) != "stdio" || nodeValue(mappingValue(config, "command")) != "npx" {
		t.Fatalf("stdio shape not rendered:\n%s", data)
	}
	for _, stale := range []string{"url", "headers"} {
		if mappingValue(config, stale) != nil {
			t.Fatalf("stale http key %q kept:\n%s", stale, data)
		}
	}
	if nodeValue(mappingValue(config, "custom")) != "keep" {
		t.Fatalf("unknown key dropped:\n%s", data)
	}
}

func TestDSHDisabledFollowsEnabled(t *testing.T) {
	adapter := dshAdapterForTest(t)
	disabled := false
	merged, err := adapter.Merge(dshEmptyPatch(), nil, map[string]MCP{"docs": {Type: "http", URL: "https://example.com/mcp", Enabled: &disabled}}, nil, dshResolved())
	if err != nil {
		t.Fatal(err)
	}
	data, err := adapter.Serialize(merged)
	if err != nil {
		t.Fatal(err)
	}
	entry := dshEntry(t, dshParseDocument(t, data), "mcp-docs")
	if nodeValue(mappingValue(entry, "disabled")) != "true" {
		t.Fatalf("disabled entry not rendered:\n%s", data)
	}

	document, err := adapter.Parse([]byte(`- id: mcp-docs
  name: "@deepseek-ai/dsh-mcp-client"
  config:
    serverName: docs
    transport: streamable-http
    url: https://example.com/mcp
  disabled: true
`))
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	merged, err = adapter.Merge(document, nil, map[string]MCP{"docs": {Type: "http", URL: "https://example.com/mcp", Enabled: &enabled}}, nil, dshResolved())
	if err != nil {
		t.Fatal(err)
	}
	data, err = adapter.Serialize(merged)
	if err != nil {
		t.Fatal(err)
	}
	entry = dshEntry(t, dshParseDocument(t, data), "mcp-docs")
	if mappingValue(entry, "disabled") != nil {
		t.Fatalf("enabled entry kept disabled:\n%s", data)
	}
}

func TestDSHSerializeParsesBackIdempotently(t *testing.T) {
	adapter := dshAdapterForTest(t)
	merged, err := adapter.Merge(map[string]any{}, nil, map[string]MCP{
		"docs":  {Type: "http", URL: "https://example.com/mcp"},
		"local": {Type: "stdio", Command: "uvx", Args: []string{"example"}, Env: map[string]string{"TOKEN": "value"}},
	}, nil, dshResolved())
	if err != nil {
		t.Fatal(err)
	}
	first, err := adapter.Serialize(merged)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := adapter.Parse(first)
	if err != nil {
		t.Fatal(err)
	}
	mergedAgain, err := adapter.Merge(parsed, nil, map[string]MCP{
		"docs":  {Type: "http", URL: "https://example.com/mcp"},
		"local": {Type: "stdio", Command: "uvx", Args: []string{"example"}, Env: map[string]string{"TOKEN": "value"}},
	}, nil, dshResolved())
	if err != nil {
		t.Fatal(err)
	}
	second, err := adapter.Serialize(mergedAgain)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("serialization is not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestDSHParseAcceptsEmptyPatch(t *testing.T) {
	adapter := dshAdapterForTest(t)
	for _, input := range []string{"", "# a comment only\n", "[]\n", "null\n"} {
		document, err := adapter.Parse([]byte(input))
		if err != nil {
			t.Fatalf("parse %q: %v", input, err)
		}
		data, err := adapter.Serialize(document)
		if err != nil {
			t.Fatalf("serialize %q: %v", input, err)
		}
		if strings.TrimSpace(string(data)) != "[]" {
			t.Fatalf("empty patch %q serialized as %q", input, data)
		}
	}
	if _, err := adapter.Parse([]byte("id: not-a-list\n")); err == nil {
		t.Fatal("expected a mapping document to be rejected")
	}
}

func TestDSHRejectsConflictingEntryIdentity(t *testing.T) {
	adapter := dshAdapterForTest(t)
	document, err := adapter.Parse([]byte(`- id: mcp-docs
  name: "@deepseek-ai/other-plugin"
  config: {}
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Merge(document, nil, map[string]MCP{"docs": {Type: "http", URL: "https://example.com/mcp"}}, nil, dshResolved()); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("expected an id conflict, got %v", err)
	}

	document, err = adapter.Parse([]byte(`- id: first
  name: "@deepseek-ai/dsh-mcp-client"
  config:
    serverName: docs
- id: second
  name: "@deepseek-ai/dsh-mcp-client"
  config:
    serverName: docs
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Merge(document, nil, map[string]MCP{"docs": {Type: "http", URL: "https://example.com/mcp"}}, nil, dshResolved()); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("expected a duplicate server error, got %v", err)
	}
}

func TestDSHRejectsInvalidServerName(t *testing.T) {
	adapter := dshAdapterForTest(t)
	_, err := adapter.Merge(dshEmptyPatch(), nil, map[string]MCP{"docs.v2": {Type: "http", URL: "https://example.com/mcp"}}, nil, dshResolved())
	if err == nil || !strings.Contains(err.Error(), "server names") {
		t.Fatalf("expected a serverName validation error, got %v", err)
	}
}
