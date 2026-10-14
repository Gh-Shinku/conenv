package conenv

import "time"

const SchemaVersion = "conenv/v2"

type Manifest struct {
	SchemaVersion  string               `yaml:"schemaVersion" json:"schemaVersion"`
	DefaultProfile string               `yaml:"defaultProfile,omitempty" json:"defaultProfile,omitempty"`
	Variables      map[string]string    `yaml:"variables,omitempty" json:"variables,omitempty"`
	Secrets        map[string]SecretRef `yaml:"secrets,omitempty" json:"secrets,omitempty"`
	Profiles       map[string]Profile   `yaml:"profiles" json:"profiles"`
	Components     map[string]Component `yaml:"components" json:"components"`
}

type Profile struct {
	Extends    []string          `yaml:"extends,omitempty" json:"extends,omitempty"`
	Components []string          `yaml:"components,omitempty" json:"components,omitempty"`
	Variables  map[string]string `yaml:"variables,omitempty" json:"variables,omitempty"`
}

type SecretRef struct {
	Env  string `yaml:"env,omitempty" json:"env,omitempty"`
	File string `yaml:"file,omitempty" json:"file,omitempty"`
}

type Component struct {
	Category      string           `yaml:"category,omitempty" json:"category,omitempty"`
	Description   string           `yaml:"description,omitempty" json:"description,omitempty"`
	Platforms     []string         `yaml:"platforms,omitempty" json:"platforms,omitempty"`
	Architectures []string         `yaml:"architectures,omitempty" json:"architectures,omitempty"`
	Packages      []Package        `yaml:"packages,omitempty" json:"packages,omitempty"`
	Files         []ManagedFile    `yaml:"files,omitempty" json:"files,omitempty"`
	Agents        map[string]Agent `yaml:"agents,omitempty" json:"agents,omitempty"`
	Checks        []Check          `yaml:"checks,omitempty" json:"checks,omitempty"`
}

type Package struct {
	Name    string `yaml:"name,omitempty" json:"name,omitempty"`
	Manager string `yaml:"manager" json:"manager"`
	ID      string `yaml:"id" json:"id"`
	Version string `yaml:"version,omitempty" json:"version,omitempty"`
}

type ManagedFile struct {
	Name      string `yaml:"name,omitempty" json:"name,omitempty"`
	Source    string `yaml:"source" json:"source"`
	Target    string `yaml:"target" json:"target"`
	Mode      string `yaml:"mode" json:"mode"`
	Sensitive bool   `yaml:"sensitive,omitempty" json:"sensitive,omitempty"`
}

type Skill struct {
	Name   string `yaml:"name" json:"name"`
	Source string `yaml:"source" json:"source"`
}

type MCP struct {
	Type    string            `yaml:"type" json:"type"`
	URL     string            `yaml:"url,omitempty" json:"url,omitempty"`
	Command string            `yaml:"command,omitempty" json:"command,omitempty"`
	Args    []string          `yaml:"args,omitempty" json:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	Enabled *bool             `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

type Check struct {
	Name    string   `yaml:"name" json:"name"`
	Type    string   `yaml:"type" json:"type"`
	Path    string   `yaml:"path,omitempty" json:"path,omitempty"`
	Command string   `yaml:"command,omitempty" json:"command,omitempty"`
	Args    []string `yaml:"args,omitempty" json:"args,omitempty"`
}

type Resolved struct {
	Root            string
	Profile         string
	Variables       map[string]string
	Secrets         map[string]SecretRef
	Components      []NamedComponent
	Skipped         []string
	SelectionSource string
}

type NamedComponent struct {
	Name string
	Component
}

type Action struct {
	Kind      string `json:"kind"`
	Component string `json:"component"`
	Summary   string `json:"summary"`
	Target    string `json:"target,omitempty"`
	Source    string `json:"source,omitempty"`
	Manager   string `json:"manager,omitempty"`
	PackageID string `json:"packageId,omitempty"`
	Version   string `json:"version,omitempty"`
	Sensitive bool   `json:"sensitive,omitempty"`
	Content   []byte `json:"-"`
}

type Plan struct {
	Repository string            `json:"repository"`
	Profile    string            `json:"profile"`
	Scope      []string          `json:"scope"`
	Platform   string            `json:"platform"`
	Skipped    []string          `json:"skipped,omitempty"`
	Actions    []Action          `json:"actions"`
	Resources  []ManagedResource `json:"resources,omitempty"`
}

type ManagedResource struct {
	Component string `json:"component"`
	Harness   string `json:"harness,omitempty"`
	Kind      string `json:"kind"`
	Target    string `json:"target"`
	Digest    string `json:"digest"`
}

type CheckResult struct {
	Name    string `json:"name"`
	Healthy bool   `json:"healthy"`
	Detail  string `json:"detail"`
}

type ComponentStatus struct {
	Name    string `json:"name"`
	State   string `json:"state"`
	Pending int    `json:"pending,omitempty"`
	Error   string `json:"error,omitempty"`
}

type StatusReport struct {
	Repository      string            `json:"repository"`
	Profile         string            `json:"profile"`
	Scope           []string          `json:"scope"`
	Platform        string            `json:"platform"`
	Skipped         []string          `json:"skipped,omitempty"`
	SelectionSource string            `json:"selectionSource,omitempty"`
	InSync          bool              `json:"inSync"`
	Components      []ComponentStatus `json:"components"`
	Healthy         bool              `json:"healthy,omitempty"`
	Checks          []CheckResult     `json:"checks,omitempty"`
}

type BackupRecord struct {
	Target  string `json:"target"`
	Backup  string `json:"backup,omitempty"`
	Existed bool   `json:"existed"`
}

type RunRecord struct {
	ID        string            `json:"id"`
	Profile   string            `json:"profile"`
	CreatedAt time.Time         `json:"createdAt"`
	Changes   []BackupRecord    `json:"changes"`
	Packages  []string          `json:"packages,omitempty"`
	Resources []ManagedResource `json:"resources,omitempty"`
	Previous  *StateSnapshot    `json:"previous,omitempty"`
}

type StateSnapshot struct {
	ActiveProfile string            `json:"activeProfile,omitempty"`
	LastRun       string            `json:"lastRun,omitempty"`
	Managed       []ManagedResource `json:"managed,omitempty"`
}

type RestoreAction struct {
	Target string `json:"target"`
	Action string `json:"action"`
	Backup string `json:"backup,omitempty"`
}

type RestorePlan struct {
	RunID            string          `json:"runId"`
	Profile          string          `json:"profile"`
	Actions          []RestoreAction `json:"actions"`
	PackagesRetained []string        `json:"packagesRetained,omitempty"`
}

type RestoreRecord struct {
	RunID      string    `json:"runId"`
	RestoredAt time.Time `json:"restoredAt"`
}

type State struct {
	ActiveProfile string            `json:"activeProfile,omitempty"`
	LastRun       string            `json:"lastRun,omitempty"`
	Managed       []ManagedResource `json:"managed,omitempty"`
	Runs          []RunRecord       `json:"runs,omitempty"`
	Restores      []RestoreRecord   `json:"restores,omitempty"`
}
