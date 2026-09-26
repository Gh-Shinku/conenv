package assets

import _ "embed"

// ManifestSchema is the JSON Schema for a conenv/v1 manifest.
//
//go:embed schema/conenv.v1.schema.json
var ManifestSchema []byte
