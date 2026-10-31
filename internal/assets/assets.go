package assets

import (
	_ "embed"
)

// ManifestSchema is the JSON Schema for a conenv/v2 manifest.
//
//go:embed schema/conenv.v2.schema.json
var ManifestSchema []byte

// SelectionSchema is the JSON Schema for a conenv/selection/v2 allowlist.
//
//go:embed schema/selection.v2.schema.json
var SelectionSchema []byte
