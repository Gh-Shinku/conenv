package conenv

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/pelletier/go-toml/v2"
)

// codexAdapter manages the Codex CLI global configuration stored as TOML in
// ~/.codex/config.toml. Skills are linked into the shared global skills
// directory, which Codex reads alongside its own.
type codexAdapter struct{}

func (codexAdapter) Harness() string { return "codex" }

func (codexAdapter) DefaultConfigPath(home string) string {
	return filepath.Join(home, ".codex", "config.toml")
}

func (codexAdapter) DefaultSkillsDir(home string) string {
	return filepath.Join(home, ".agents", "skills")
}

// codexConfigKeys lists the top level settings conenv manages. Declaring
// anything else is rejected so a typo cannot inject an unmanaged key into a
// configuration file that belongs to another tool. MCP servers are generated
// from the harness neutral mcpServers declarations, never written by hand.
func (codexAdapter) AllowedConfigKeys() []string {
	keys := make([]string, 0, len(codexConfigKeys))
	for key := range codexConfigKeys {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

var codexConfigKeys = map[string]bool{
	"approval_policy":          true,
	"history":                  true,
	"model":                    true,
	"model_provider":           true,
	"model_reasoning_effort":   true,
	"model_reasoning_summary":  true,
	"project_doc_max_bytes":    true,
	"sandbox_mode":             true,
	"sandbox_workspace_write":  true,
	"shell_environment_policy": true,
	"tools":                    true,
	"tui":                      true,
	"web_search":               true,
}

func (codexAdapter) Validate(config map[string]any) error {
	for _, key := range sortedKeys(config) {
		if !codexConfigKeys[key] {
			return fmt.Errorf("unknown codex setting %q; managed settings: %v", key, codexAllowedKeys())
		}
	}
	return nil
}

func codexAllowedKeys() []string {
	keys := make([]string, 0, len(codexConfigKeys))
	for key := range codexConfigKeys {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (codexAdapter) Parse(data []byte) (any, error) {
	doc := map[string]any{}
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse Codex config: %w", err)
	}
	return doc, nil
}

func (codexAdapter) Merge(doc any, config map[string]any, mcp map[string]MCP, skills []Skill, r *Resolved) (any, error) {
	root, ok := doc.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("Codex config document has unexpected type %T", doc)
	}
	merged := cloneConfigMap(root)
	for _, key := range sortedKeys(config) {
		merged[key] = config[key]
	}
	if len(mcp) > 0 {
		servers, err := mergedMCPServers(root, mcp, r)
		if err != nil {
			return nil, err
		}
		merged["mcp_servers"] = servers
	}
	return merged, nil
}

func (codexAdapter) Serialize(doc any) ([]byte, error) {
	data, err := toml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encode Codex config: %w", err)
	}
	return data, nil
}

// mergedMCPServers expands the declared servers and merges them into the
// servers already present, so unknown keys on an existing entry are retained.
func mergedMCPServers(root map[string]any, mcp map[string]MCP, r *Resolved) (map[string]any, error) {
	servers := cloneConfigMap(nil)
	if existing, ok := root["mcp_servers"].(map[string]any); ok {
		servers = cloneConfigMap(existing)
	}
	for _, name := range sortedKeys(mcp) {
		declaration := mcp[name]
		entry := cloneConfigMap(nil)
		if previous, ok := servers[name].(map[string]any); ok {
			entry = cloneConfigMap(previous)
		}
		if declaration.Type == "http" {
			url, err := expandString(declaration.URL, r.Variables, r.Secrets)
			if err != nil {
				return nil, fmt.Errorf("MCP server %s: %w", name, err)
			}
			entry["url"] = url
		} else {
			command, err := expandString(declaration.Command, r.Variables, r.Secrets)
			if err != nil {
				return nil, fmt.Errorf("MCP server %s: %w", name, err)
			}
			entry["command"] = command
			if len(declaration.Args) > 0 {
				args := make([]string, len(declaration.Args))
				for i, arg := range declaration.Args {
					expanded, err := expandString(arg, r.Variables, r.Secrets)
					if err != nil {
						return nil, fmt.Errorf("MCP server %s arg %d: %w", name, i, err)
					}
					args[i] = expanded
				}
				entry["args"] = args
			}
			if len(declaration.Env) > 0 {
				env := map[string]string{}
				for _, key := range sortedKeys(declaration.Env) {
					expanded, err := expandString(declaration.Env[key], r.Variables, r.Secrets)
					if err != nil {
						return nil, fmt.Errorf("MCP server %s env %s: %w", name, key, err)
					}
					env[key] = expanded
				}
				entry["env"] = env
			}
		}
		if declaration.Enabled != nil {
			entry["enabled"] = *declaration.Enabled
		}
		servers[name] = entry
	}
	return servers, nil
}

// ValidateAgent checks a declared agent against its adapter without touching
// the machine, so config --check reports problems before an apply.
func ValidateAgent(agent Agent) error {
	adapter, err := LookupAdapter(agent.Harness)
	if err != nil {
		return err
	}
	if err := adapter.Validate(agent.Config); err != nil {
		return err
	}
	return validateMCPServers(agent.MCPServers)
}

// validateMCP is shared with manifest validation so both entry points report
// the same problems.
func validateMCPServers(mcp map[string]MCP) error {
	for _, name := range sortedKeys(mcp) {
		server := mcp[name]
		switch {
		case server.Type == "http" && server.URL == "":
			return fmt.Errorf("MCP server %q requires url", name)
		case server.Type == "stdio" && server.Command == "":
			return fmt.Errorf("MCP server %q requires command", name)
		case server.Type != "http" && server.Type != "stdio":
			return fmt.Errorf("MCP server %q type must be http or stdio", name)
		}
	}
	return nil
}

// cloneConfigMap copies a TOML document so a merge never mutates the parsed
// original or shares nested maps with it.
func cloneConfigMap(source map[string]any) map[string]any {
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = cloneConfigValue(value)
	}
	return clone
}

func cloneConfigValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneConfigMap(typed)
	case []any:
		clone := make([]any, len(typed))
		for i, item := range typed {
			clone[i] = cloneConfigValue(item)
		}
		return clone
	default:
		return value
	}
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Expand helpers live here because the Codex adapter is the only consumer of
// nested configuration values.
func expandMap(source map[string]any, r *Resolved) (map[string]any, error) {
	result := make(map[string]any, len(source))
	for key, value := range source {
		expanded, err := expandValue(value, r)
		if err != nil {
			return nil, err
		}
		result[key] = expanded
	}
	return result, nil
}

func expandValue(value any, r *Resolved) (any, error) {
	switch typed := value.(type) {
	case string:
		return expandString(typed, r.Variables, r.Secrets)
	case map[string]any:
		return expandMap(typed, r)
	case []any:
		result := make([]any, len(typed))
		for i, item := range typed {
			expanded, err := expandValue(item, r)
			if err != nil {
				return nil, err
			}
			result[i] = expanded
		}
		return result, nil
	default:
		return value, nil
	}
}
