package conenv

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// dshAdapter manages the DeepSeek Harness user-level loader patch stored as a
// YAML sequence in ~/.dsh/cordis.patch.yml, and links skills into DSH's private
// skill directory ~/.dsh/skills.
//
// DSH composes a profile from its bundle layers, the profile's own
// cordis.patch.yml, this home-level patch, and command-line overlays, in that
// order. The home-level file therefore applies to every DSH profile and is the
// smallest surface that can hold global MCP declarations: each server becomes
// an @deepseek-ai/dsh-mcp-client entry. Other entries and unknown keys in an
// entry or its config are preserved, and custom tags such as !!js survive the
// round trip.
//
// conenv manages no settings keys for DSH. A DSH setting is a plugin entry in a
// profile patch, which belongs to the profile that reads it, so a declared
// `config` is rejected instead of guessed into a generic document.
type dshAdapter struct{}

const (
	dshMCPEntryPackage = "@deepseek-ai/dsh-mcp-client"
	dshMCPEntryPrefix  = "mcp-"
)

// dshServerNamePattern is the serverName contract of dsh-mcp-client.
var dshServerNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

func (dshAdapter) Harness() string { return "dsh" }

func (dshAdapter) SkillsOnly() bool { return false }

func (dshAdapter) DefaultConfigPath(home string) string {
	return filepath.Join(home, ".dsh", "cordis.patch.yml")
}

func (dshAdapter) DefaultSkillsDir(home string) string {
	return filepath.Join(home, ".dsh", "skills")
}

// AllowedConfigKeys is empty by design: DSH settings are profile patch entries,
// not keys in the home-level patch that conenv renders.
func (dshAdapter) AllowedConfigKeys() []string { return nil }

func (dshAdapter) Validate(config map[string]any) error {
	if len(config) > 0 {
		return errDSHNoSettings()
	}
	return nil
}

func errDSHNoSettings() error {
	return fmt.Errorf("harness dsh manages no settings; declare MCP servers under mcpServers and plugin entries in the DSH profile patch under ~/.dsh/profiles/<name>/cordis.patch.yml")
}

func (dshAdapter) Parse(data []byte) (any, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return dshEmptyPatch(), nil
	}
	root := &yaml.Node{}
	if err := yaml.Unmarshal(data, root); err != nil {
		return nil, fmt.Errorf("parse DSH patch config: %w", err)
	}
	if root.Kind == 0 {
		return dshEmptyPatch(), nil
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) != 1 {
		return nil, fmt.Errorf("DSH patch config must be a YAML sequence of loader entries")
	}
	sequence := root.Content[0]
	if sequence.Kind == yaml.ScalarNode && sequence.Tag == "!!null" {
		root.Content[0] = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		return root, nil
	}
	if sequence.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("DSH patch config must be a YAML sequence of loader entries")
	}
	return root, nil
}

func (dshAdapter) Merge(doc any, config map[string]any, mcp map[string]MCP, _ []Skill, r *Resolved) (any, error) {
	if len(config) > 0 {
		return nil, errDSHNoSettings()
	}
	root, err := dshPatchDocument(doc)
	if err != nil {
		return nil, err
	}
	if len(mcp) == 0 {
		return root, nil
	}
	sequence := root.Content[0]
	for _, name := range sortedKeys(mcp) {
		if !dshServerNamePattern.MatchString(name) {
			return nil, fmt.Errorf("MCP server %q: DSH server names must match [A-Za-z0-9_-]{1,32}", name)
		}
		declaration := mcp[name]
		index, err := findDSHMCPEntry(sequence, name)
		if err != nil {
			return nil, err
		}
		if index < 0 {
			entry, err := newDSHMCPEntry(name, declaration, r)
			if err != nil {
				return nil, err
			}
			sequence.Content = append(sequence.Content, entry)
			continue
		}
		if err := mergeDSHMCPEntry(sequence.Content[index], name, declaration, r); err != nil {
			return nil, err
		}
	}
	return root, nil
}

func (dshAdapter) Serialize(doc any) ([]byte, error) {
	root, err := dshPatchDocument(doc)
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(root); err != nil {
		return nil, fmt.Errorf("encode DSH patch config: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("encode DSH patch config: %w", err)
	}
	data := buffer.Bytes()
	if len(data) == 0 || data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	return data, nil
}

// dshPatchDocument accepts the parsed document, or the empty map that the
// planner passes when the configuration file does not exist yet.
func dshPatchDocument(doc any) (*yaml.Node, error) {
	switch typed := doc.(type) {
	case *yaml.Node:
		if typed.Kind != yaml.DocumentNode || len(typed.Content) != 1 || typed.Content[0].Kind != yaml.SequenceNode {
			return nil, fmt.Errorf("DSH patch config must be a YAML sequence of loader entries")
		}
		return typed, nil
	case map[string]any:
		if len(typed) != 0 {
			return nil, fmt.Errorf("DSH patch config document has unexpected type %T", doc)
		}
		return dshEmptyPatch(), nil
	default:
		return nil, fmt.Errorf("DSH patch config document has unexpected type %T", doc)
	}
}

func dshEmptyPatch() *yaml.Node {
	return &yaml.Node{
		Kind:    yaml.DocumentNode,
		Content: []*yaml.Node{{Kind: yaml.SequenceNode, Tag: "!!seq"}},
	}
}

// findDSHMCPEntry locates the entry that already represents one declared
// server, so repeated applies update it instead of appending duplicates.
func findDSHMCPEntry(sequence *yaml.Node, name string) (int, error) {
	id := dshMCPEntryPrefix + name
	idIndex, serverIndex := -1, -1
	for i, entry := range sequence.Content {
		if entry.Kind != yaml.MappingNode {
			continue
		}
		if idNode := mappingValue(entry, "id"); idNode != nil && idNode.Value == id {
			if idIndex >= 0 {
				return -1, fmt.Errorf("DSH patch repeats entry id %q", id)
			}
			idIndex = i
		}
		nameNode := mappingValue(entry, "name")
		if nameNode == nil || nameNode.Value != dshMCPEntryPackage {
			continue
		}
		config := mappingValue(entry, "config")
		if config == nil || config.Kind != yaml.MappingNode {
			continue
		}
		if server := mappingValue(config, "serverName"); server != nil && server.Value == name {
			if serverIndex >= 0 {
				return -1, fmt.Errorf("DSH patch declares MCP server %q twice", name)
			}
			serverIndex = i
		}
	}
	if idIndex >= 0 && serverIndex >= 0 && idIndex != serverIndex {
		return -1, fmt.Errorf("DSH patch entry id %q and MCP server %q are declared by different entries", id, name)
	}
	if idIndex >= 0 {
		entry := sequence.Content[idIndex]
		nameNode := mappingValue(entry, "name")
		if nameNode == nil || nameNode.Value != dshMCPEntryPackage {
			return -1, fmt.Errorf("DSH patch entry id %q is already used by %q", id, nodeValue(nameNode))
		}
		config := mappingValue(entry, "config")
		if config == nil {
			return -1, fmt.Errorf("DSH patch entry id %q has no config mapping", id)
		}
		if server := mappingValue(config, "serverName"); server != nil && server.Value != name {
			return -1, fmt.Errorf("DSH patch entry id %q names MCP server %q", id, server.Value)
		}
		return idIndex, nil
	}
	return serverIndex, nil
}

func newDSHMCPEntry(name string, declaration MCP, r *Resolved) (*yaml.Node, error) {
	config, err := dshMCPConfig(name, declaration, r)
	if err != nil {
		return nil, err
	}
	entry := &yaml.Node{
		Kind: yaml.MappingNode,
		Tag:  "!!map",
		Content: []*yaml.Node{
			dshScalar("id"), dshScalar(dshMCPEntryPrefix + name),
			dshScalar("name"), dshScalar(dshMCPEntryPackage),
			dshScalar("config"), config,
		},
	}
	if declaration.Enabled != nil && !*declaration.Enabled {
		entry.Content = append(entry.Content, dshScalar("disabled"), dshBool(true))
	}
	return entry, nil
}

func mergeDSHMCPEntry(entry *yaml.Node, name string, declaration MCP, r *Resolved) error {
	if entry.Kind != yaml.MappingNode {
		return fmt.Errorf("DSH patch entry for MCP server %q is not a mapping", name)
	}
	nameNode := mappingValue(entry, "name")
	if nameNode == nil || nameNode.Value != dshMCPEntryPackage {
		return fmt.Errorf("DSH patch entry id %q does not belong to %s", dshMCPEntryPrefix+name, dshMCPEntryPackage)
	}
	config := mappingValue(entry, "config")
	if config == nil {
		config = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setMappingNode(entry, "config", config)
	}
	if config.Kind != yaml.MappingNode {
		return fmt.Errorf("DSH patch entry for MCP server %q has a non-mapping config", name)
	}
	if err := applyDSHMCPConfig(config, name, declaration, r); err != nil {
		return err
	}
	if declaration.Enabled != nil {
		if *declaration.Enabled {
			removeMappingNode(entry, "disabled")
		} else {
			setMappingNode(entry, "disabled", dshBool(true))
		}
	}
	return nil
}

// dshMCPConfig builds a config mapping for a new dsh-mcp-client entry.
func dshMCPConfig(name string, declaration MCP, r *Resolved) (*yaml.Node, error) {
	config := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	if err := applyDSHMCPConfig(config, name, declaration, r); err != nil {
		return nil, err
	}
	return config, nil
}

// applyDSHMCPConfig renders the declared transport into a config mapping,
// preserving unknown keys. A transport switch removes the keys of the previous
// shape so dsh-mcp-client does not read both.
func applyDSHMCPConfig(config *yaml.Node, name string, declaration MCP, r *Resolved) error {
	if config.Kind != yaml.MappingNode {
		return fmt.Errorf("DSH patch entry for MCP server %q has a non-mapping config", name)
	}
	setMappingScalar(config, "serverName", name)
	if declaration.Type == "http" {
		url, err := expandString(declaration.URL, r.Variables, r.Secrets)
		if err != nil {
			return fmt.Errorf("MCP server %s: %w", name, err)
		}
		setMappingScalar(config, "transport", "streamable-http")
		setMappingScalar(config, "url", url)
		removeMappingNode(config, "command")
		removeMappingNode(config, "args")
		removeMappingNode(config, "env")
		return nil
	}
	command, err := expandString(declaration.Command, r.Variables, r.Secrets)
	if err != nil {
		return fmt.Errorf("MCP server %s: %w", name, err)
	}
	setMappingScalar(config, "transport", "stdio")
	setMappingScalar(config, "command", command)
	if len(declaration.Args) > 0 {
		args := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for i, arg := range declaration.Args {
			expanded, err := expandString(arg, r.Variables, r.Secrets)
			if err != nil {
				return fmt.Errorf("MCP server %s arg %d: %w", name, i, err)
			}
			args.Content = append(args.Content, dshScalar(expanded))
		}
		setMappingNode(config, "args", args)
	}
	if len(declaration.Env) > 0 {
		env := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, key := range sortedKeys(declaration.Env) {
			expanded, err := expandString(declaration.Env[key], r.Variables, r.Secrets)
			if err != nil {
				return fmt.Errorf("MCP server %s env %s: %w", name, key, err)
			}
			env.Content = append(env.Content, dshScalar(key), dshScalar(expanded))
		}
		setMappingNode(config, "env", env)
	}
	removeMappingNode(config, "url")
	removeMappingNode(config, "headers")
	return nil
}

func dshScalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func dshBool(value bool) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(value)}
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func nodeValue(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	return node.Value
}

// setMappingScalar keeps an existing scalar node so its comments survive a
// managed value update.
func setMappingScalar(mapping *yaml.Node, key, value string) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value != key {
			continue
		}
		target := mapping.Content[i+1]
		if target.Kind == yaml.ScalarNode {
			target.Tag = "!!str"
			target.Value = value
			target.Style = 0
			return
		}
		mapping.Content[i+1] = dshScalar(value)
		return
	}
	mapping.Content = append(mapping.Content, dshScalar(key), dshScalar(value))
}

func setMappingNode(mapping *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = value
			return
		}
	}
	mapping.Content = append(mapping.Content, dshScalar(key), value)
}

func removeMappingNode(mapping *yaml.Node, key string) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
			return
		}
	}
}
