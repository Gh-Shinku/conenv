package conenv

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

func packageInstalled(pkg Package) (bool, error) {
	if _, err := exec.LookPath(pkg.Manager); err != nil {
		return false, fmt.Errorf("package manager %q is not available", pkg.Manager)
	}
	var command *exec.Cmd
	switch pkg.Manager {
	case "winget":
		command = exec.Command("winget", "list", "--id", pkg.ID, "--exact", "--accept-source-agreements", "--disable-interactivity")
	case "scoop":
		command = exec.Command("scoop", "list", pkg.ID)
	default:
		return false, fmt.Errorf("unsupported package manager %q", pkg.Manager)
	}
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return false, nil
		}
		return false, err
	}
	text := strings.ToLower(output.String())
	if !strings.Contains(text, strings.ToLower(pkg.ID)) {
		return false, nil
	}
	if pkg.Version != "" && !strings.Contains(text, strings.ToLower(pkg.Version)) {
		return false, nil
	}
	return true, nil
}

func installPackage(action Action) error {
	var command *exec.Cmd
	switch action.Manager {
	case "winget":
		args := []string{"install", "--id", action.PackageID, "--exact", "--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity"}
		if action.Version != "" {
			args = append(args, "--version", action.Version)
		}
		command = exec.Command("winget", args...)
	case "scoop":
		identifier := action.PackageID
		if action.Version != "" {
			identifier += "@" + action.Version
		}
		command = exec.Command("scoop", "install", identifier)
	default:
		return fmt.Errorf("unsupported package manager %q", action.Manager)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s install %s: %w: %s", action.Manager, action.PackageID, err, strings.TrimSpace(string(output)))
	}
	return nil
}
