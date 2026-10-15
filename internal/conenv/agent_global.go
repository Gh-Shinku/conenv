package conenv

import (
	"fmt"
	"path/filepath"
)

// globalAdapter links skills into the shared agent directory that every harness
// on the machine reads. It owns no configuration file: settings and MCP
// declarations belong to the harness that reads them, so this adapter accepts
// skills and nothing else.
type globalAdapter struct{}

func (globalAdapter) Harness() string { return "global" }

func (globalAdapter) SkillsOnly() bool { return true }

func (globalAdapter) DefaultSkillsDir(home string) string {
	return filepath.Join(home, ".agents", "skills")
}

// DefaultConfigPath satisfies the adapter contract and is unreachable: conenv
// resolves no configuration path for a skills-only harness.
func (globalAdapter) DefaultConfigPath(string) string { return "" }

// AllowedConfigKeys is empty because there is no file to hold keys.
func (globalAdapter) AllowedConfigKeys() []string { return nil }

func (globalAdapter) Validate(config map[string]any) error {
	if len(config) > 0 {
		return errGlobalSkillsOnly()
	}
	return nil
}

func (globalAdapter) Parse([]byte) (any, error) { return nil, errGlobalSkillsOnly() }

func (globalAdapter) Merge(any, map[string]any, map[string]MCP, []Skill, *Resolved) (any, error) {
	return nil, errGlobalSkillsOnly()
}

func (globalAdapter) Serialize(any) ([]byte, error) { return nil, errGlobalSkillsOnly() }

// errGlobalSkillsOnly fails loudly if a declaration bypasses validation, instead
// of writing a configuration file that no harness reads.
func errGlobalSkillsOnly() error {
	return fmt.Errorf("harness global manages skills only; declare settings and MCP servers under a harness that owns their configuration file")
}
