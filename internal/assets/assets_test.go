package assets

import (
	"encoding/json"
	"testing"
)

func TestManifestSchemaIsEmbeddedJSON(t *testing.T) {
	if len(ManifestSchema) == 0 {
		t.Fatal("embedded manifest schema is empty")
	}
	var schema map[string]any
	if err := json.Unmarshal(ManifestSchema, &schema); err != nil {
		t.Fatalf("embedded manifest schema is invalid: %v", err)
	}
	if schema["title"] != "conenv manifest" {
		t.Fatalf("unexpected schema title: %v", schema["title"])
	}
	defs, ok := schema["$defs"].(map[string]any)
	if !ok {
		t.Fatalf("schema has no definitions: %v", schema)
	}
	for _, name := range []string{"component", "agent", "mcp", "file", "check"} {
		if _, ok := defs[name]; !ok {
			t.Fatalf("schema is missing the %q definition", name)
		}
	}
	component, _ := defs["component"].(map[string]any)
	properties, _ := component["properties"].(map[string]any)
	if _, ok := properties["agents"]; !ok {
		t.Fatal("component definition does not declare agents")
	}
	if _, ok := properties["codex"]; ok {
		t.Fatal("component definition still declares the removed codex block")
	}
}
