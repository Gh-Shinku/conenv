package conenv

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const SelectionSchemaVersion = "conenv/selection/v1"

// SelectionSpec narrows a resolved profile to an explicit allowlist. A
// component selects its own resources and, separately, individual agent
// declarations by name.
type SelectionSpec struct {
	SchemaVersion string                        `yaml:"schemaVersion" json:"schemaVersion"`
	Profile       string                        `yaml:"profile" json:"profile"`
	Components    map[string]ComponentSelection `yaml:"components" json:"components"`
}

type ComponentSelection struct {
	Resources bool                          `yaml:"resources,omitempty" json:"resources,omitempty"`
	Skills    []string                      `yaml:"skills,omitempty" json:"skills,omitempty"`
	Agents    map[string]ComponentSelection `yaml:"agents,omitempty" json:"agents,omitempty"`
}

func LoadSelection(path string) (*SelectionSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read selection %s: %w", path, err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var spec SelectionSpec
	if err := decoder.Decode(&spec); err != nil {
		return nil, fmt.Errorf("decode selection %s: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("selection %s must contain one YAML document", path)
	}
	if err := validateSelectionShape(&spec); err != nil {
		return nil, fmt.Errorf("selection %s: %w", path, err)
	}
	return &spec, nil
}

func SaveSelection(path string, spec *SelectionSpec) error {
	if err := validateSelectionShape(spec); err != nil {
		return err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(spec)
	if err != nil {
		return err
	}
	return atomicWrite(absolute, data, 0o644)
}

// SelectionFromTargets converts stable resource IDs into a selection spec.
// Accepted forms are COMPONENT, COMPONENT/resources, COMPONENT/agents/AGENT,
// COMPONENT/agents/AGENT/resources, and COMPONENT/agents/AGENT/skills/SKILL.
func SelectionFromTargets(resolved *Resolved, targets []string) (*SelectionSpec, error) {
	spec := &SelectionSpec{
		SchemaVersion: SelectionSchemaVersion,
		Profile:       resolved.Profile,
		Components:    map[string]ComponentSelection{},
	}
	components := make(map[string]NamedComponent, len(resolved.Components))
	for _, component := range resolved.Components {
		components[component.Name] = component
	}
	for _, target := range targets {
		component, suffix, ok := matchSelectionTarget(components, target)
		if !ok {
			return nil, fmt.Errorf("unknown selection target %q", target)
		}
		selected := spec.Components[component.Name]
		switch {
		case suffix == "":
			selected = componentSelection(component)
		case suffix == "resources":
			if !HasComponentResources(component.Component) {
				return nil, fmt.Errorf("component %q has no component resources", component.Name)
			}
			selected.Resources = true
		default:
			if _, err := selectAgentTarget(component, suffix, &selected); err != nil {
				return nil, fmt.Errorf("invalid selection target %q: %w", target, err)
			}
		}
		// A component that declares nothing selectable cannot contribute to a
		// narrowed scope; ApplySelection simply drops it.
		spec.Components[component.Name] = selected
	}
	if len(spec.Components) == 0 {
		return nil, errors.New("selection must include at least one resource")
	}
	return spec, nil
}

func (selection ComponentSelection) selectsAnything() bool {
	return selection.Resources || len(selection.Skills) > 0 || len(selection.Agents) > 0
}

// componentSelection selects everything a component declares.
func componentSelection(component NamedComponent) ComponentSelection {
	selected := ComponentSelection{Resources: HasComponentResources(component.Component)}
	for _, name := range agentNames(component.Component) {
		agent := component.Agents[name]
		selection := ComponentSelection{Resources: agent.ManagesConfig()}
		for _, skill := range agent.Skills {
			selection.Skills = append(selection.Skills, skill.Name)
		}
		if !selection.selectsAnything() {
			continue
		}
		if selected.Agents == nil {
			selected.Agents = map[string]ComponentSelection{}
		}
		selected.Agents[name] = selection
	}
	return selected
}

// selectAgentTarget applies an agents/... suffix to a component selection.
func selectAgentTarget(component NamedComponent, suffix string, selected *ComponentSelection) (string, error) {
	rest, ok := strings.CutPrefix(suffix, "agents/")
	if !ok {
		return "", fmt.Errorf("use resources or agents/AGENT/...")
	}
	agentName, agentSuffix := splitTarget(rest)
	if agentName == "" {
		return "", errors.New("agent name is required")
	}
	agent, ok := component.Agents[agentName]
	if !ok {
		return "", fmt.Errorf("unknown agent %q", agentName)
	}
	if selected.Agents == nil {
		selected.Agents = map[string]ComponentSelection{}
	}
	entry := selected.Agents[agentName]
	switch {
	case agentSuffix == "":
		entry.Resources = agent.ManagesConfig()
		for _, skill := range agent.Skills {
			if !contains(entry.Skills, skill.Name) {
				entry.Skills = append(entry.Skills, skill.Name)
			}
		}
	case agentSuffix == "resources":
		if !agent.ManagesConfig() {
			return "", fmt.Errorf("agent %q has no configuration or MCP servers", agentName)
		}
		entry.Resources = true
	case strings.HasPrefix(agentSuffix, "skills/") && strings.TrimPrefix(agentSuffix, "skills/") != "":
		skillName := strings.TrimPrefix(agentSuffix, "skills/")
		if !agent.hasSkill(skillName) {
			return "", fmt.Errorf("agent %q has no skill %q", agentName, skillName)
		}
		if !contains(entry.Skills, skillName) {
			entry.Skills = append(entry.Skills, skillName)
		}
	default:
		return "", fmt.Errorf("agent %q target %q must be resources or skills/SKILL", agentName, agentSuffix)
	}
	selected.Agents[agentName] = entry
	return agentSuffix, nil
}

func matchSelectionTarget(components map[string]NamedComponent, target string) (NamedComponent, string, bool) {
	if component, ok := components[target]; ok {
		return component, "", true
	}
	names := make([]string, 0, len(components))
	for name := range components {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })
	for _, name := range names {
		prefix := name + "/"
		if strings.HasPrefix(target, prefix) {
			return components[name], strings.TrimPrefix(target, prefix), true
		}
	}
	return NamedComponent{}, "", false
}

func splitTarget(value string) (string, string) {
	if index := strings.Index(value, "/"); index >= 0 {
		return value[:index], value[index+1:]
	}
	return value, ""
}

func ApplySelection(resolved *Resolved, spec *SelectionSpec) (*Resolved, error) {
	if err := validateSelectionShape(spec); err != nil {
		return nil, err
	}
	if spec.Profile != resolved.Profile {
		return nil, fmt.Errorf("selection profile %q does not match resolved profile %q", spec.Profile, resolved.Profile)
	}
	available := make(map[string]NamedComponent, len(resolved.Components))
	for _, component := range resolved.Components {
		available[component.Name] = component
	}
	for _, name := range sortedKeys(spec.Components) {
		selection := spec.Components[name]
		component, ok := available[name]
		if !ok {
			return nil, fmt.Errorf("selection references unknown or unsupported component %q", name)
		}
		if selection.Resources && !HasComponentResources(component.Component) {
			return nil, fmt.Errorf("component %q has no component resources", name)
		}
		seenSkills := map[string]bool{}
		for _, skill := range selection.Skills {
			if seenSkills[skill] {
				return nil, fmt.Errorf("component %q repeats skill %q", name, skill)
			}
			seenSkills[skill] = true
			if !componentHasSkill(component, skill) {
				return nil, fmt.Errorf("component %q has no skill %q", name, skill)
			}
		}
		for _, agentName := range sortedKeys(selection.Agents) {
			entry := selection.Agents[agentName]
			// An empty entry is a deselected placeholder from the interactive
			// checklist, not an invalid reference.
			if !entry.selectsAnything() {
				continue
			}
			agent, ok := component.Agents[agentName]
			if !ok {
				return nil, fmt.Errorf("component %q has no agent %q or it is unsupported here", name, agentName)
			}
			if entry.Resources && !agent.ManagesConfig() {
				return nil, fmt.Errorf("agent %q in component %q has no configuration or MCP servers", agentName, name)
			}
			seenAgentSkills := map[string]bool{}
			for _, skill := range entry.Skills {
				if seenAgentSkills[skill] {
					return nil, fmt.Errorf("agent %q in component %q repeats skill %q", agentName, name, skill)
				}
				seenAgentSkills[skill] = true
				if !agent.hasSkill(skill) {
					return nil, fmt.Errorf("agent %q in component %q has no skill %q", agentName, name, skill)
				}
			}
		}
		if !selection.selectsAnything() {
			return nil, fmt.Errorf("component %q selects no resources", name)
		}
	}

	copyResolved := *resolved
	copyResolved.Components = make([]NamedComponent, 0, len(spec.Components))
	for _, component := range resolved.Components {
		selection, ok := spec.Components[component.Name]
		if !ok {
			continue
		}
		filtered := component
		if !selection.Resources {
			filtered.Packages = nil
			filtered.Files = nil
			filtered.Checks = nil
		}
		filtered.Agents = nil
		for _, agentName := range agentNames(component.Component) {
			agent := component.Agents[agentName]
			entry, selected := selection.Agents[agentName]
			if !selected {
				continue
			}
			narrowed := agent
			if !entry.Resources {
				narrowed.Config = nil
				narrowed.MCPServers = nil
			}
			narrowed.Skills = nil
			for _, skill := range agent.Skills {
				if contains(entry.Skills, skill.Name) {
					narrowed.Skills = append(narrowed.Skills, skill)
				}
			}
			if !narrowed.ManagesAnything() {
				continue
			}
			if filtered.Agents == nil {
				filtered.Agents = map[string]Agent{}
			}
			filtered.Agents[agentName] = narrowed
		}
		copyResolved.Components = append(copyResolved.Components, filtered)
	}
	return &copyResolved, nil
}

func HasComponentResources(component Component) bool {
	if len(component.Packages) > 0 || len(component.Files) > 0 || len(component.Checks) > 0 {
		return true
	}
	return false
}

func validateSelectionShape(spec *SelectionSpec) error {
	if spec == nil {
		return errors.New("selection is required")
	}
	if spec.SchemaVersion != SelectionSchemaVersion {
		return fmt.Errorf("schemaVersion must be %q", SelectionSchemaVersion)
	}
	if strings.TrimSpace(spec.Profile) == "" {
		return errors.New("profile is required")
	}
	if len(spec.Components) == 0 {
		return errors.New("at least one component is required")
	}
	for name := range spec.Components {
		if strings.TrimSpace(name) == "" {
			return errors.New("component names must not be empty")
		}
	}
	return nil
}

func componentHasSkill(component NamedComponent, name string) bool {
	for _, agentName := range agentNames(component.Component) {
		if component.Agents[agentName].hasSkill(name) {
			return true
		}
	}
	return false
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
