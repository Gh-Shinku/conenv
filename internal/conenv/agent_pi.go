package conenv

import (
	"encoding/json"
	"fmt"
	"path/filepath"
)

// piAdapter manages the Pi coding agent global declarations stored as JSON in
// ~/.pi/agent/mcp.json, and links skills into Pi's private skill directory
// ~/.pi/agent/skills.
//
// Pi also keeps its own settings in ~/.pi/agent/settings.json, which contains
// resource filters, packages, and model selection that Pi rewrites itself.
// conenv manages no settings keys for this harness: the adapter renders only
// mcpServers, so a declared `config` is rejected instead of injecting keys into
// a file another tool owns.
type piAdapter struct{}

func (piAdapter) Harness() string { return "pi" }

func (piAdapter) DefaultConfigPath(home string) string {
	return filepath.Join(home, ".pi", "agent", "mcp.json")
}

func (piAdapter) DefaultSkillsDir(home string) string {
	return filepath.Join(home, ".pi", "agent", "skills")
}

// AllowedConfigKeys is empty by design: Pi owns its settings file and conenv
// only writes the MCP declarations next to it.
func (piAdapter) AllowedConfigKeys() []string { return nil }

func (piAdapter) Validate(config map[string]any) error {
	if len(config) > 0 {
		return fmt.Errorf("harness pi manages no settings; declare MCP servers under mcpServers")
	}
	return nil
}

func (piAdapter) Parse(data []byte) (any, error) {
	doc := map[string]any{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse Pi MCP config: %w", err)
	}
	return doc, nil
}

func (piAdapter) Merge(doc any, config map[string]any, mcp map[string]MCP, _ []Skill, r *Resolved) (any, error) {
	root, ok := doc.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("Pi MCP config document has unexpected type %T", doc)
	}
	if len(config) > 0 {
		return nil, fmt.Errorf("harness pi manages no settings; declare MCP servers under mcpServers")
	}
	merged := cloneConfigMap(root)
	if len(mcp) > 0 {
		servers, err := mergedPiMCPServers(root, mcp, r)
		if err != nil {
			return nil, err
		}
		merged["mcpServers"] = servers
	}
	return merged, nil
}

func (piAdapter) Serialize(doc any) ([]byte, error) {
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode Pi MCP config: %w", err)
	}
	return append(data, '\n'), nil
}

// mergedPiMCPServers expands the declared servers and merges them into the
// servers already present, so unknown keys on an existing entry are retained.
// Pi infers the transport from the keys themselves, so the keys of the other
// transport are removed when a declaration changes one.
func mergedPiMCPServers(root map[string]any, mcp map[string]MCP, r *Resolved) (map[string]any, error) {
	servers := cloneConfigMap(nil)
	if existing, ok := root["mcpServers"].(map[string]any); ok {
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
			delete(entry, "command")
			delete(entry, "args")
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
			delete(entry, "url")
		}
		if declaration.Enabled != nil {
			entry["enabled"] = *declaration.Enabled
		}
		servers[name] = entry
	}
	return servers, nil
}
