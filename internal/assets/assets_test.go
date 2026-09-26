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
	if schema["title"] != "Conenv v1 manifest" {
		t.Fatalf("unexpected schema title: %v", schema["title"])
	}
}
