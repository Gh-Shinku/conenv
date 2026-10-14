package conenv

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

func Load(root, profile string) (*Resolved, error) {
	resolved, err := LoadProfile(root, profile)
	if err != nil {
		return nil, err
	}
	return resolved, ValidateResolved(resolved)
}

// LoadProfile parses and resolves a profile without checking runtime
// prerequisites such as source files and secret availability. Callers that
// narrow the profile first can apply their selection and then call
// ValidateScope, so resources outside the requested scope cannot block them.
func LoadProfile(root, profile string) (*Resolved, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	base, err := LoadManifest(absRoot)
	if err != nil {
		return nil, err
	}
	if profile == "" {
		profile = base.DefaultProfile
	}
	if profile == "" {
		return nil, errors.New("no profile selected and defaultProfile is empty")
	}
	if _, ok := base.Profiles[profile]; !ok {
		return nil, fmt.Errorf("unknown profile %q", profile)
	}

	vars := copyStrings(base.Variables)
	var componentNames []string
	seenComponents := map[string]bool{}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(string) error
	visit = func(name string) error {
		if visiting[name] {
			return fmt.Errorf("profile inheritance cycle at %q", name)
		}
		if visited[name] {
			return nil
		}
		p, ok := base.Profiles[name]
		if !ok {
			return fmt.Errorf("profile %q extends unknown profile", name)
		}
		visiting[name] = true
		for _, parent := range p.Extends {
			if err := visit(parent); err != nil {
				return err
			}
		}
		for k, v := range p.Variables {
			vars[k] = v
		}
		for _, component := range p.Components {
			if !seenComponents[component] {
				componentNames = append(componentNames, component)
				seenComponents[component] = true
			}
		}
		visiting[name] = false
		visited[name] = true
		return nil
	}
	if err := visit(profile); err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	vars["home"] = home
	vars["repo"] = absRoot
	vars["profile"] = profile
	vars["os"] = runtime.GOOS
	vars["arch"] = runtime.GOARCH

	resolved := &Resolved{Root: absRoot, Profile: profile, Variables: vars, Secrets: base.Secrets}
	for _, name := range componentNames {
		component, ok := base.Components[name]
		if !ok {
			return nil, fmt.Errorf("profile %q references unknown component %q", profile, name)
		}
		if matches(runtime.GOOS, component.Platforms) && matches(runtime.GOARCH, component.Architectures) {
			resolved.Components = append(resolved.Components, NamedComponent{Name: name, Component: component})
		} else {
			resolved.Skipped = append(resolved.Skipped, name)
		}
	}
	return resolved, nil
}

// LoadManifest reads the base and machine-local manifests, merges them, and
// validates their structure without resolving a profile or runtime resources.
func LoadManifest(root string) (*Manifest, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	base, err := decodeManifest(filepath.Join(absRoot, "conenv.yaml"), true)
	if err != nil {
		return nil, err
	}
	localPath := filepath.Join(absRoot, "conenv.local.yaml")
	if _, err := os.Stat(localPath); err == nil {
		local, decodeErr := decodeManifest(localPath, false)
		if decodeErr != nil {
			return nil, decodeErr
		}
		mergeManifest(base, local)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := validateManifest(base, absRoot); err != nil {
		return nil, err
	}
	return base, nil
}

func decodeManifest(path string, required bool) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && required {
			return nil, fmt.Errorf("manifest not found: %s", path)
		}
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var manifest Manifest
	if err := dec.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("%s must contain one YAML document", path)
	}
	return &manifest, nil
}

func mergeManifest(base, overlay *Manifest) {
	if overlay.DefaultProfile != "" {
		base.DefaultProfile = overlay.DefaultProfile
	}
	if base.Variables == nil {
		base.Variables = map[string]string{}
	}
	for k, v := range overlay.Variables {
		base.Variables[k] = v
	}
	if base.Secrets == nil {
		base.Secrets = map[string]SecretRef{}
	}
	for k, v := range overlay.Secrets {
		base.Secrets[k] = v
	}
	if base.Profiles == nil {
		base.Profiles = map[string]Profile{}
	}
	for name, incoming := range overlay.Profiles {
		current := base.Profiles[name]
		current.Extends = append(current.Extends, incoming.Extends...)
		current.Components = append(current.Components, incoming.Components...)
		if current.Variables == nil {
			current.Variables = map[string]string{}
		}
		for k, v := range incoming.Variables {
			current.Variables[k] = v
		}
		base.Profiles[name] = current
	}
	if base.Components == nil {
		base.Components = map[string]Component{}
	}
	for k, v := range overlay.Components {
		base.Components[k] = v
	}
}

func validateManifest(m *Manifest, root string) error {
	var problems []string
	if m.SchemaVersion != SchemaVersion {
		problems = append(problems, fmt.Sprintf("schemaVersion must be %q", SchemaVersion))
	}
	if len(m.Profiles) == 0 {
		problems = append(problems, "at least one profile is required")
	}
	profileNames := make([]string, 0, len(m.Profiles))
	for name := range m.Profiles {
		profileNames = append(profileNames, name)
	}
	sort.Strings(profileNames)
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visitProfile func(string)
	visitProfile = func(name string) {
		if visiting[name] {
			problems = append(problems, fmt.Sprintf("profile inheritance cycle at %q", name))
			return
		}
		if visited[name] {
			return
		}
		profile, ok := m.Profiles[name]
		if !ok {
			problems = append(problems, fmt.Sprintf("unknown profile %q", name))
			return
		}
		visiting[name] = true
		for _, parent := range profile.Extends {
			visitProfile(parent)
		}
		for _, component := range profile.Components {
			if _, ok := m.Components[component]; !ok {
				problems = append(problems, fmt.Sprintf("profile %q references unknown component %q", name, component))
			}
		}
		visiting[name] = false
		visited[name] = true
	}
	for _, name := range profileNames {
		visitProfile(name)
	}
	for name, secret := range m.Secrets {
		if (secret.Env == "") == (secret.File == "") {
			problems = append(problems, fmt.Sprintf("secret %q must set exactly one of env or file", name))
		}
	}
	componentNames := make([]string, 0, len(m.Components))
	for name := range m.Components {
		componentNames = append(componentNames, name)
	}
	sort.Strings(componentNames)
	for _, name := range componentNames {
		c := m.Components[name]
		if c.Category != "" && c.Category != "program" && c.Category != "ai" {
			problems = append(problems, fmt.Sprintf("component %q category must be program or ai", name))
		}
		for i, pkg := range c.Packages {
			if pkg.ID == "" || (pkg.Manager != "winget" && pkg.Manager != "scoop") {
				problems = append(problems, fmt.Sprintf("component %q package %d requires id and manager winget or scoop", name, i))
			}
		}
		for i, file := range c.Files {
			if file.Source == "" || file.Target == "" || (file.Mode != "link" && file.Mode != "render") {
				problems = append(problems, fmt.Sprintf("component %q file %d requires source, target, and mode link or render", name, i))
			}
		}
		if c.Codex != nil {
			seenSkills := map[string]bool{}
			for _, skill := range c.Codex.Skills {
				if skill.Name == "" || skill.Source == "" {
					problems = append(problems, fmt.Sprintf("component %q has an invalid Codex skill", name))
					continue
				}
				if seenSkills[skill.Name] {
					problems = append(problems, fmt.Sprintf("component %q repeats Codex skill %q", name, skill.Name))
				}
				seenSkills[skill.Name] = true
			}
			for serverName, server := range c.Codex.MCPServers {
				if server.Type == "http" && server.URL == "" {
					problems = append(problems, fmt.Sprintf("MCP server %q requires url", serverName))
				} else if server.Type == "stdio" && server.Command == "" {
					problems = append(problems, fmt.Sprintf("MCP server %q requires command", serverName))
				} else if server.Type != "http" && server.Type != "stdio" {
					problems = append(problems, fmt.Sprintf("MCP server %q type must be http or stdio", serverName))
				}
			}
		}
		for _, check := range c.Checks {
			if check.Name == "" || (check.Type != "path" && check.Type != "command") || (check.Type == "path" && check.Path == "") || (check.Type == "command" && check.Command == "") {
				problems = append(problems, fmt.Sprintf("component %q has an invalid health check", name))
			}
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func ValidateResolved(r *Resolved) error {
	for name, ref := range r.Secrets {
		if ref.Env != "" {
			if _, ok := os.LookupEnv(ref.Env); !ok {
				return fmt.Errorf("secret %q references unset environment variable %s", name, ref.Env)
			}
		}
		if ref.File != "" {
			path, err := expandString(ref.File, r.Variables, nil)
			if err != nil {
				return fmt.Errorf("secret %q: %w", name, err)
			}
			if _, err := os.Stat(path); err != nil {
				return fmt.Errorf("secret %q file: %w", name, err)
			}
		}
	}
	return ValidateScope(r)
}

// ValidateScope checks runtime prerequisites and ownership conflicts for the
// components present in a resolved profile. Secret values are resolved lazily
// while planning, which keeps unused declarations outside a narrowed scope
// from blocking the command.
func ValidateScope(r *Resolved) error {
	targets := map[string]string{}
	skills := map[string]string{}
	servers := map[string]string{}
	for _, component := range r.Components {
		for _, file := range component.Files {
			source, err := expandPath(file.Source, r)
			if err != nil {
				return fmt.Errorf("component %q source: %w", component.Name, err)
			}
			if !filepath.IsAbs(source) {
				source = filepath.Join(r.Root, source)
			}
			if _, err := os.Stat(source); err != nil {
				return fmt.Errorf("component %q source %q: %w", component.Name, file.Source, err)
			}
			target, err := expandPath(file.Target, r)
			if err != nil {
				return fmt.Errorf("component %q target: %w", component.Name, err)
			}
			if !filepath.IsAbs(target) {
				return fmt.Errorf("component %q target %q must resolve to an absolute path", component.Name, file.Target)
			}
			if owner, exists := targets[target]; exists {
				return fmt.Errorf("target %q is managed by both %q and %q", target, owner, component.Name)
			}
			targets[target] = component.Name
		}
		if component.Codex == nil {
			continue
		}
		for _, skill := range component.Codex.Skills {
			if owner, exists := skills[skill.Name]; exists {
				return fmt.Errorf("Codex skill %q is managed by both %q and %q", skill.Name, owner, component.Name)
			}
			skills[skill.Name] = component.Name
			source, err := expandPath(skill.Source, r)
			if err != nil {
				return fmt.Errorf("Codex skill %q: %w", skill.Name, err)
			}
			if !filepath.IsAbs(source) {
				source = filepath.Join(r.Root, source)
			}
			if _, err := os.Stat(filepath.Join(source, "SKILL.md")); err != nil {
				return fmt.Errorf("Codex skill %q: %w", skill.Name, err)
			}
		}
		for name := range component.Codex.MCPServers {
			if owner, exists := servers[name]; exists {
				return fmt.Errorf("MCP server %q is managed by both %q and %q", name, owner, component.Name)
			}
			servers[name] = component.Name
		}
	}
	return nil
}

func matches(value string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, candidate := range allowed {
		if candidate == value {
			return true
		}
	}
	return false
}

func copyStrings(source map[string]string) map[string]string {
	dest := make(map[string]string, len(source))
	for k, v := range source {
		dest[k] = v
	}
	return dest
}
