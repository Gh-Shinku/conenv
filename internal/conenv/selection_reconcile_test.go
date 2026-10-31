package conenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeselectedMCPRemovalAcrossHarnesses(t *testing.T) {
	for _, harness := range []string{"codex", "pi", "dsh"} {
		t.Run(harness, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "config")
			adapter, err := LookupAdapter(harness)
			if err != nil {
				t.Fatal(err)
			}
			r := &Resolved{Root: root, Profile: "test", Variables: map[string]string{"home": root}}
			// Seed two servers, only one of which is managed by the declaration.
			doc, err := adapter.Merge(map[string]any{}, nil, map[string]MCP{
				"rea":    {Type: "stdio", Command: "rea"},
				"manual": {Type: "stdio", Command: "keep-manual"},
			}, nil, r)
			if err != nil {
				t.Fatal(err)
			}
			data, err := adapter.Serialize(doc)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			r.Components = []NamedComponent{{Name: "ai", Component: Component{Agents: map[string]Agent{
				harness: {Name: harness, Harness: harness, ConfigPath: path, MCPServers: map[string]MCP{
					// This unselected server must not expand runtime prerequisites.
					"rea": {Type: "stdio", Command: "{{ .secrets.unavailable }}"},
				}},
			}}}}
			selected := *r
			selected.Components = nil
			ReconcileMCPSelection(r, &selected)
			plan, err := BuildPlan(&selected)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Actions) != 1 || !strings.Contains(plan.Actions[0].Summary, "remove MCP servers: rea") {
				t.Fatalf("cleanup plan = %#v", plan.Actions)
			}
			content := string(plan.Actions[0].Content)
			if strings.Contains(content, "rea") || !strings.Contains(content, "keep-manual") {
				t.Fatalf("cleanup changed unmanaged servers: %s", content)
			}
			if err := os.WriteFile(path, plan.Actions[0].Content, 0o600); err != nil {
				t.Fatal(err)
			}
			again, err := BuildPlan(&selected)
			if err != nil || len(again.Actions) != 0 {
				t.Fatalf("cleanup is not idempotent: %v, %v", again, err)
			}
			missing := selected.Components[0].Agents[harness]
			missing.ConfigPath = filepath.Join(root, "absent")
			selected.Components[0].Agents[harness] = missing
			plan, err = BuildPlan(&selected)
			if err != nil || len(plan.Actions) != 0 {
				t.Fatalf("cleanup created absent config: %v, %v", plan, err)
			}
		})
	}
}
