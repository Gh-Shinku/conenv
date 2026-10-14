package conenv

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

	content, target, err := desiredCodexConfig(r)
	if err != nil {
		return nil, err
	}
	if content != nil {
		plan.Resources = append(plan.Resources, ManagedResource{Component: "codex", Kind: "codex_config", Target: target, Digest: digestBytes(content)})
		equal, err := sameContent(target, content)
		if err != nil {
			return nil, err
		}
		if !equal {
			plan.Actions = append(plan.Actions, Action{Kind: "codex_config", Component: "codex", Target: target, Content: content, Sensitive: true, Summary: "merge Codex config"})
		}
	}
	for _, component := range r.Components {
		if component.Codex == nil {
			continue
		}
		for _, skill := range component.Codex.Skills {
			source, err := expandPath(skill.Source, r)
			if err != nil {
				return nil, err
			}
			if !filepath.IsAbs(source) {
				source = filepath.Join(r.Root, source)
			}
			target := filepath.Join(r.Variables["home"], ".codex", "skills", skill.Name)
			plan.Resources = append(plan.Resources, ManagedResource{Component: component.Name, Kind: "codex_skill", Target: target, Digest: digestString(source)})
			equal, err := sameLink(target, source)
			if err != nil {
				return nil, err
			}
			if !equal {
				plan.Actions = append(plan.Actions, Action{Kind: "link_skill", Component: component.Name, Source: source, Target: target, Summary: fmt.Sprintf("link Codex skill %s", skill.Name)})
			}
		}
	}
	return plan, nil
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
