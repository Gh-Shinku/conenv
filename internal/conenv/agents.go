package conenv

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// AgentAdapter knows where one harness keeps its global configuration and how
// to merge declared settings into it. Everything harness specific lives behind
// this interface so adding a harness does not touch planning or execution.
type AgentAdapter interface {
	Harness() string
	// DefaultConfigPath and DefaultSkillsDir return absolute paths for the
	// machine home directory, without variable expansion.
	DefaultConfigPath(home string) string
	DefaultSkillsDir(home string) string
	// Parse reads an existing configuration file into a manageable document.
	Parse(data []byte) (doc any, err error)
	// Merge applies the collected settings to a parsed document and returns the
	// document that should be written back.
	Merge(doc any, config map[string]any, mcp map[string]MCP, skills []Skill, r *Resolved) (any, error)
	Serialize(doc any) ([]byte, error)
	// Validate rejects declared keys that this harness does not manage.
	Validate(config map[string]any) error
	// AllowedConfigKeys lists the settings this harness manages, for help and
	// error output.
	AllowedConfigKeys() []string
}

// Agent is one harness target declared by a component. The map key in
// Component.Agents is the agent name and is copied into Name after decoding.
type Agent struct {
	Name       string         `yaml:"-" json:"name"`
	Harness    string         `yaml:"harness" json:"harness"`
	ConfigPath string         `yaml:"configPath,omitempty" json:"configPath,omitempty"`
	SkillsDir  string         `yaml:"skillsDir,omitempty" json:"skillsDir,omitempty"`
	Config     map[string]any `yaml:"config,omitempty" json:"config,omitempty"`
	MCPServers map[string]MCP `yaml:"mcpServers,omitempty" json:"mcpServers,omitempty"`
	Skills     []Skill        `yaml:"skills,omitempty" json:"skills,omitempty"`
}

// namedAgents copies the map key of a declared agent into the value so later
// stages can report which agent they are working on.
func namedAgents(component *Component) {
	for name, agent := range component.Agents {
		agent.Name = name
		component.Agents[name] = agent
	}
}

// validAgentName keeps an agent or skill name usable as a single path element,
// so a declaration cannot escape the skills directory.
func validAgentName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	return !strings.ContainsAny(name, `/\`+"\x00")
}

// ManagesConfig reports whether the agent declares settings that are written
// into the harness configuration file.
func (a Agent) ManagesConfig() bool {
	return len(a.Config) > 0 || len(a.MCPServers) > 0
}

// ManagesAnything reports whether the agent contributes anything a machine has
// to change.
func (a Agent) ManagesAnything() bool {
	return a.ManagesConfig() || len(a.Skills) > 0
}

func (a Agent) hasSkill(name string) bool {
	for _, skill := range a.Skills {
		if skill.Name == name {
			return true
		}
	}
	return false
}

func agentEnvName(harness, suffix string) string {
	replacer := strings.NewReplacer("-", "_", ".", "_")
	return "CONENV_AGENT_" + strings.ToUpper(replacer.Replace(harness)) + "_" + suffix
}

// AgentConfigPath resolves where an agent's configuration file lives. The
// component declaration wins, then an environment override, then the adapter
// default for the harness.
func AgentConfigPath(r *Resolved, agent Agent) (string, error) {
	adapter, err := LookupAdapter(agent.Harness)
	if err != nil {
		return "", err
	}
	value := agent.ConfigPath
	if value == "" {
		if override := os.Getenv(agentEnvName(agent.Harness, "CONFIG")); override != "" {
			value = override
		}
	}
	if value == "" {
		path := adapter.DefaultConfigPath(r.Variables["home"])
		return absoluteAgentPath(agent, "config path", path)
	}
	path, err := expandPath(value, r)
	if err != nil {
		return "", fmt.Errorf("agent %s config path: %w", agent.Name, err)
	}
	return absoluteAgentPath(agent, "config path", path)
}

// AgentSkillsDir resolves where an agent's skills are linked. A harness that
// only reads a private directory returns that directory from its adapter.
func AgentSkillsDir(r *Resolved, agent Agent) (string, error) {
	adapter, err := LookupAdapter(agent.Harness)
	if err != nil {
		return "", err
	}
	value := agent.SkillsDir
	if value == "" {
		if override := os.Getenv(agentEnvName(agent.Harness, "SKILLS_DIR")); override != "" {
			value = override
		}
	}
	if value == "" {
		path := adapter.DefaultSkillsDir(r.Variables["home"])
		return absoluteAgentPath(agent, "skills dir", path)
	}
	path, err := expandPath(value, r)
	if err != nil {
		return "", fmt.Errorf("agent %s skills dir: %w", agent.Name, err)
	}
	return absoluteAgentPath(agent, "skills dir", path)
}

func absoluteAgentPath(agent Agent, label, path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("agent %s %s %q must resolve to an absolute path", agent.Name, label, path)
	}
	return path, nil
}

// agentNames returns the declared agent names in a stable order.
func agentNames(component Component) []string {
	names := make([]string, 0, len(component.Agents))
	for name := range component.Agents {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
