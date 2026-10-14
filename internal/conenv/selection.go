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

type SelectionSpec struct {
	SchemaVersion string                        `yaml:"schemaVersion" json:"schemaVersion"`
	Profile       string                        `yaml:"profile" json:"profile"`
	Components    map[string]ComponentSelection `yaml:"components" json:"components"`
}

type ComponentSelection struct {
	Resources bool     `yaml:"resources,omitempty" json:"resources,omitempty"`
	Skills    []string `yaml:"skills,omitempty" json:"skills,omitempty"`
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
	if err := atomicWrite(absolute, data, 0o644); err != nil {
		return err
	}
	return nil
}

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
			selected.Resources = HasComponentResources(component.Component)
			if component.Codex != nil {
				selected.Skills = selected.Skills[:0]
				for _, skill := range component.Codex.Skills {
					selected.Skills = append(selected.Skills, skill.Name)
				}
			}
			if !selected.Resources && len(selected.Skills) == 0 {
				return nil, fmt.Errorf("component %q has no selectable resources", component.Name)
			}
		case suffix == "resources":
			if !HasComponentResources(component.Component) {
				return nil, fmt.Errorf("component %q has no component resources", component.Name)
			}
			selected.Resources = true
		case strings.HasPrefix(suffix, "skills/") && strings.TrimPrefix(suffix, "skills/") != "":
			skillName := strings.TrimPrefix(suffix, "skills/")
			if !componentHasSkill(component, skillName) {
				return nil, fmt.Errorf("component %q has no Codex skill %q", component.Name, skillName)
			}
			if !contains(selected.Skills, skillName) {
				selected.Skills = append(selected.Skills, skillName)
			}
		default:
			return nil, fmt.Errorf("invalid selection target %q; use COMPONENT, COMPONENT/resources, or COMPONENT/skills/SKILL", target)
		}
		spec.Components[component.Name] = selected
	}
	if len(spec.Components) == 0 {
		return nil, errors.New("selection must include at least one resource")
	}
	return spec, nil
}

func matchSelectionTarget(components map[string]NamedComponent, target string) (NamedComponent, string, bool) {
	if component, ok := components[target]; ok {
		return component, "", true
	}
	var names []string
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
	componentNames := make([]string, 0, len(spec.Components))
	for name := range spec.Components {
		componentNames = append(componentNames, name)
	}
	sort.Strings(componentNames)
	for _, name := range componentNames {
		component, ok := available[name]
		if !ok {
			return nil, fmt.Errorf("selection references unknown or unsupported component %q", name)
		}
		selected := spec.Components[name]
		if selected.Resources && !HasComponentResources(component.Component) {
			return nil, fmt.Errorf("component %q has no component resources", name)
		}
		seenSkills := map[string]bool{}
		for _, skill := range selected.Skills {
			if seenSkills[skill] {
				return nil, fmt.Errorf("component %q repeats Codex skill %q", name, skill)
			}
			seenSkills[skill] = true
			if !componentHasSkill(component, skill) {
				return nil, fmt.Errorf("component %q has no Codex skill %q", name, skill)
			}
		}
		if !selected.Resources && len(selected.Skills) == 0 {
			return nil, fmt.Errorf("component %q selects no resources", name)
		}
	}

	copyResolved := *resolved
	copyResolved.Components = make([]NamedComponent, 0, len(spec.Components))
	for _, component := range resolved.Components {
		selected, ok := spec.Components[component.Name]
		if !ok {
			continue
		}
		filtered := component
		if !selected.Resources {
			filtered.Packages = nil
			filtered.Files = nil
			filtered.Checks = nil
		}
		if component.Codex != nil {
			codex := *component.Codex
			codex.Skills = nil
			for _, skill := range component.Codex.Skills {
				if contains(selected.Skills, skill.Name) {
					codex.Skills = append(codex.Skills, skill)
				}
			}
			if !selected.Resources {
				codex.Config = nil
				codex.MCPServers = nil
			}
			filtered.Codex = &codex
		}
		copyResolved.Components = append(copyResolved.Components, filtered)
	}
	return &copyResolved, nil
}

func HasComponentResources(component Component) bool {
	if len(component.Packages) > 0 || len(component.Files) > 0 || len(component.Checks) > 0 {
		return true
	}
	if component.Codex == nil {
		return true
	}
	return len(component.Codex.Config) > 0 || len(component.Codex.MCPServers) > 0 || len(component.Codex.Skills) == 0
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
	if component.Codex == nil {
		return false
	}
	for _, skill := range component.Codex.Skills {
		if skill.Name == name {
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
