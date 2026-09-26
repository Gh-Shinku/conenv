package conenv

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

func desiredCodexConfig(r *Resolved) ([]byte, string, error) {
	home := r.Variables["home"]
	target := filepath.Join(home, ".codex", "config.toml")
	root := map[string]any{}
	if data, err := os.ReadFile(target); err == nil {
		if err := toml.Unmarshal(data, &root); err != nil {
			return nil, target, fmt.Errorf("parse existing Codex config: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, target, err
	}
	servers, _ := root["mcp_servers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	managed := false
	for _, component := range r.Components {
		if component.Codex == nil {
			continue
		}
		if len(component.Codex.Config) > 0 || len(component.Codex.MCPServers) > 0 {
			managed = true
		}
		expandedConfig, err := expandMap(component.Codex.Config, r)
		if err != nil {
			return nil, target, fmt.Errorf("Codex config in component %s: %w", component.Name, err)
		}
		deepMerge(root, expandedConfig)
		for name, declaration := range component.Codex.MCPServers {
			entry := map[string]any{}
			if declaration.Type == "http" {
				url, err := expandString(declaration.URL, r.Variables, r.Secrets)
				if err != nil {
					return nil, target, fmt.Errorf("MCP server %s: %w", name, err)
				}
				entry["url"] = url
			} else {
				command, err := expandString(declaration.Command, r.Variables, r.Secrets)
				if err != nil {
					return nil, target, fmt.Errorf("MCP server %s: %w", name, err)
				}
				entry["command"] = command
				if len(declaration.Args) > 0 {
					args := make([]string, len(declaration.Args))
					for i, arg := range declaration.Args {
						expanded, err := expandString(arg, r.Variables, r.Secrets)
						if err != nil {
							return nil, target, fmt.Errorf("MCP server %s arg %d: %w", name, i, err)
						}
						args[i] = expanded
					}
					entry["args"] = args
				}
				if len(declaration.Env) > 0 {
					env := map[string]string{}
					for key, value := range declaration.Env {
						expanded, err := expandString(value, r.Variables, r.Secrets)
						if err != nil {
							return nil, target, fmt.Errorf("MCP server %s env %s: %w", name, key, err)
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
	}
	if !managed {
		return nil, target, nil
	}
	if len(servers) > 0 {
		root["mcp_servers"] = servers
	}
	data, err := toml.Marshal(root)
	return data, target, err
}

func deepMerge(target, source map[string]any) {
	for key, value := range source {
		incoming, incomingOK := value.(map[string]any)
		existing, existingOK := target[key].(map[string]any)
		if incomingOK && existingOK {
			deepMerge(existing, incoming)
			continue
		}
		target[key] = value
	}
}

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
