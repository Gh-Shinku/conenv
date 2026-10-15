package conenv

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
)

func BuildPlan(r *Resolved) (*Plan, error) {
	plan := &Plan{
		Repository: r.Root,
		Profile:    r.Profile,
		Scope:      componentNames(r.Components),
		Platform:   runtime.GOOS + "/" + runtime.GOARCH,
		Skipped:    append([]string(nil), r.Skipped...),
		Actions:    []Action{},
	}
	for _, component := range r.Components {
		for _, pkg := range component.Packages {
			plan.Resources = append(plan.Resources, ManagedResource{Component: component.Name, Kind: "package", Target: pkg.Manager + ":" + pkg.ID, Digest: digestString(pkg.Version)})
			installed, err := packageInstalled(pkg)
			if err != nil {
				return nil, fmt.Errorf("component %s package %s: %w", component.Name, pkg.ID, err)
			}
			if !installed {
				plan.Actions = append(plan.Actions, Action{
					Kind: "install_package", Component: component.Name, Manager: pkg.Manager,
					PackageID: pkg.ID, Version: pkg.Version,
					Summary: fmt.Sprintf("install %s with %s", pkg.ID, pkg.Manager),
				})
			}
		}
		for _, file := range component.Files {
			source, err := expandPath(file.Source, r)
			if err != nil {
				return nil, fmt.Errorf("component %s source: %w", component.Name, err)
			}
			if !filepath.IsAbs(source) {
				source = filepath.Join(r.Root, source)
			}
			target, err := expandPath(file.Target, r)
			if err != nil {
				return nil, fmt.Errorf("component %s target: %w", component.Name, err)
			}
			action := Action{Component: component.Name, Source: source, Target: target, Sensitive: file.Sensitive}
			if file.Mode == "link" {
				plan.Resources = append(plan.Resources, ManagedResource{Component: component.Name, Kind: "link", Target: target, Digest: digestString(source)})
				equal, err := sameLink(target, source)
				if err != nil {
					return nil, err
				}
				if equal {
					continue
				}
				action.Kind = "link"
				action.Summary = fmt.Sprintf("link %s -> %s", target, source)
			} else {
				content, containsSecret, err := renderFile(source, r.Variables, r.Secrets)
				if err != nil {
					return nil, fmt.Errorf("render %s: %w", source, err)
				}
				plan.Resources = append(plan.Resources, ManagedResource{Component: component.Name, Kind: "render", Target: target, Digest: digestBytes(content)})
				equal, err := sameContent(target, content)
				if err != nil {
					return nil, err
				}
				if equal {
					continue
				}
				action.Kind = "render"
				action.Content = content
				action.Sensitive = action.Sensitive || containsSecret
				action.Summary = fmt.Sprintf("render %s", target)
			}
			plan.Actions = append(plan.Actions, action)
		}
	}

	agents, err := collectAgents(r)
	if err != nil {
		return nil, err
	}
	seenSkills := map[string]bool{}
	for _, entry := range agents {
		for _, skill := range entry.skills {
			source, ok := entry.skillSources[skill.Name]
			if !ok {
				continue
			}
			target := filepath.Join(entry.skillsDir, skill.Name)
			if seenSkills[target] {
				return nil, fmt.Errorf("skill %q is planned more than once", target)
			}
			seenSkills[target] = true
			plan.Resources = append(plan.Resources, ManagedResource{
				Component: entry.agent.Name, Harness: entry.agent.Harness, Kind: "agent_skill",
				Target: target, Digest: digestString(source),
			})
			equal, err := sameLink(target, source)
			if err != nil {
				return nil, err
			}
			if equal {
				continue
			}
			plan.Actions = append(plan.Actions, Action{
				Kind: "agent_skill", Component: entry.agent.Name, Source: source, Target: target,
				Summary: fmt.Sprintf("link %s skill %s", entry.agent.Harness, skill.Name),
			})
		}
	}
	for _, entry := range agents {
		if !entry.agent.ManagesConfig() {
			continue
		}
		content, err := desiredAgentConfig(entry)
		if err != nil {
			return nil, err
		}
		plan.Resources = append(plan.Resources, ManagedResource{
			Component: entry.agent.Name, Harness: entry.agent.Harness, Kind: "agent_config",
			Target: entry.configPath, Digest: digestBytes(content),
		})
		equal, err := sameContent(entry.configPath, content)
		if err != nil {
			return nil, err
		}
		if equal {
			continue
		}
		plan.Actions = append(plan.Actions, Action{
			Kind: "agent_config", Component: entry.agent.Name, Target: entry.configPath,
			Content: content, Sensitive: true,
			Summary: fmt.Sprintf("merge %s config into %s", entry.agent.Harness, entry.configPath),
		})
	}
	return plan, nil
}

// agentEntry aggregates every declaration that targets one agent on one
// machine. Components are merged in scope order so the result is deterministic.
type agentEntry struct {
	agent        Agent
	resolved     *Resolved
	configPath   string
	skillsDir    string
	config       map[string]any
	mcp          map[string]MCP
	skills       []Skill
	skillSources map[string]string
	components   []string
}

// collectAgents groups the declared agents by name and resolves their global
// paths. Components that declare the same agent name contribute to the same
// configuration file, which is why merging happens here rather than per
// component.
func collectAgents(r *Resolved) ([]agentEntry, error) {
	entries := map[string]*agentEntry{}
	for _, component := range r.Components {
		for _, agentName := range agentNames(component.Component) {
			agent := component.Agents[agentName]
			if !agent.ManagesAnything() {
				continue
			}
			entry, ok := entries[agentName]
			if !ok {
				adapter, err := LookupAdapter(agent.Harness)
				if err != nil {
					return nil, fmt.Errorf("component %q agent %q: %w", component.Name, agentName, err)
				}
				if adapter.SkillsOnly() && agent.ManagesConfig() {
					return nil, fmt.Errorf("component %q agent %q: harness %s manages skills only; declare settings and MCP servers under a harness that owns their configuration file", component.Name, agentName, agent.Harness)
				}
				configPath := ""
				if !adapter.SkillsOnly() {
					configPath, err = AgentConfigPath(r, agent)
					if err != nil {
						return nil, fmt.Errorf("component %q: %w", component.Name, err)
					}
				}
				skillsDir, err := AgentSkillsDir(r, agent)
				if err != nil {
					return nil, fmt.Errorf("component %q: %w", component.Name, err)
				}
				entry = &agentEntry{
					agent: agent, resolved: r, configPath: configPath, skillsDir: skillsDir,
					config: map[string]any{}, mcp: map[string]MCP{}, skillSources: map[string]string{},
				}
				entries[agentName] = entry
			} else if entry.agent.Harness != agent.Harness {
				return nil, fmt.Errorf("agent %q is declared with harnesses %q and %q", agentName, entry.agent.Harness, agent.Harness)
			}
			key := agentName + "\x00" + component.Name
			for _, name := range sortedKeys(agent.Config) {
				existing, exists := entry.config[name]
				if !exists {
					entry.config[name] = agent.Config[name]
					continue
				}
				// Only an identical redeclaration of a scalar is tolerated; a
				// real conflict would silently pick one component.
				if fmt.Sprint(existing) != fmt.Sprint(agent.Config[name]) {
					return nil, fmt.Errorf("agent %q setting %q is set to different values by multiple components", agentName, name)
				}
			}
			for _, name := range sortedKeys(agent.MCPServers) {
				if owner, exists := entry.mcp[name]; exists {
					return nil, fmt.Errorf("MCP server %q for agent %q is declared twice (%s)", name, agentName, owner.Type)
				}
				entry.mcp[name] = agent.MCPServers[name]
			}
			for _, skill := range agent.Skills {
				if _, exists := entry.skillSources[skill.Name]; exists {
					return nil, fmt.Errorf("skill %q is declared twice for agent %q in %s", skill.Name, agentName, key)
				}
				source, err := expandPath(skill.Source, r)
				if err != nil {
					return nil, fmt.Errorf("skill %q: %w", skill.Name, err)
				}
				if !filepath.IsAbs(source) {
					source = filepath.Join(r.Root, source)
				}
				entry.skills = append(entry.skills, skill)
				entry.skillSources[skill.Name] = source
			}
			entry.components = append(entry.components, component.Name)
		}
	}
	if len(entries) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	collected := make([]agentEntry, 0, len(names))
	for _, name := range names {
		collected = append(collected, *entries[name])
	}
	return collected, nil
}

// desiredAgentConfig renders the configuration file for one agent. A nil
// result means the agent declares nothing that belongs in that file.
func desiredAgentConfig(entry agentEntry) ([]byte, error) {
	if !entry.agent.ManagesConfig() {
		return nil, nil
	}
	adapter, err := LookupAdapter(entry.agent.Harness)
	if err != nil {
		return nil, err
	}
	document := any(map[string]any{})
	data, err := os.ReadFile(entry.configPath)
	if err == nil {
		document, err = adapter.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("agent %s: %w", entry.agent.Name, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	config, err := expandMap(entry.config, entry.resolved)
	if err != nil {
		return nil, fmt.Errorf("agent %s config: %w", entry.agent.Name, err)
	}
	merged, err := adapter.Merge(document, config, entry.mcp, entry.skills, entry.resolved)
	if err != nil {
		return nil, fmt.Errorf("agent %s: %w", entry.agent.Name, err)
	}
	return adapter.Serialize(merged)
}

func componentNames(components []NamedComponent) []string {
	names := make([]string, 0, len(components))
	for _, component := range components {
		names = append(names, component.Name)
	}
	return names
}

func digestString(value string) string {
	return digestBytes([]byte(value))
}

func digestBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func sameContent(path string, desired []byte) (bool, error) {
	if desired == nil {
		return true, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	return bytes.Equal(data, desired), nil
}

func sameLink(path, desired string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return false, nil
	}
	target, err := os.Readlink(path)
	if err != nil {
		return false, err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	desiredAbs, err := filepath.Abs(desired)
	if err != nil {
		return false, err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false, err
	}
	return filepath.Clean(targetAbs) == filepath.Clean(desiredAbs), nil
}
