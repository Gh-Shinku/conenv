package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigInterspersesOptionsAndOperand(t *testing.T) {
	registerTestEnvironment(t)
	repository := testRepository(t)
	for _, args := range [][]string{
		{"config", "--check", "--json", repository},
		{"config", repository, "--check", "--json"},
		{"config", "--profile", "base", repository, "--check=true", "--json=true"},
		{"config", repository, "--profile=base", "--check", "--json"},
	} {
		code, stdout, stderr := runCLI(t, "", args...)
		if code != 0 {
			t.Fatalf("%v exit = %d\n%s\n%s", args, code, stdout, stderr)
		}
		report := decodeReport(t, stdout)
		if report.Repository != repository || report.Valid == nil || !*report.Valid {
			t.Fatalf("interspersed options were not parsed: %s", stdout)
		}
	}
}

func TestRemovedAndUnknownOptionsFailBeforeOrAfterOperand(t *testing.T) {
	home := registerTestEnvironment(t)
	for _, option := range []string{"--dry-run", "--only", "--unknown"} {
		for _, args := range [][]string{
			{"config", option, "directory"},
			{"config", "directory", option},
			{"apply", option},
			{"apply", "--undo", "run-1", option},
			{"status", option},
		} {
			if code, _, stderr := runCLI(t, "", args...); code != 2 || !strings.Contains(stderr, "unknown flag: "+option) {
				t.Fatalf("%v must reject unknown option: exit=%d, stderr=%s", args, code, stderr)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".conenv", "selection.yaml")); !os.IsNotExist(err) {
		t.Fatalf("invalid arguments changed selection: %v", err)
	}
}

func TestOptionTerminatorAndOptionValuesDoNotTriggerHelp(t *testing.T) {
	registerTestEnvironment(t)
	for _, args := range [][]string{
		{"config", "--", "--help"},
		{"config", "--repo", "--help"},
		{"config", "--repo=--help"},
		{"apply", "--undo", "--", "--help"},
	} {
		code, _, stderr := runCLI(t, "", args...)
		if code == 0 || strings.Contains(stderr, "Usage of") || strings.Contains(stderr, "manages a declarative") {
			t.Fatalf("%v treated an operand or value as help: exit=%d, stderr=%s", args, code, stderr)
		}
	}
}

func TestSubcommandHelpAfterOperand(t *testing.T) {
	home := registerTestEnvironment(t)
	for _, args := range [][]string{
		{"config", "directory", "--help"},
		{"config", "directory", "-h"},
		{"apply", "--undo", "run-1", "--help"},
		{"status", "--help"},
	} {
		if code, _, stderr := runCLI(t, "", args...); code != 0 || !strings.Contains(stderr, "Usage of") {
			t.Fatalf("%v help failed: exit=%d, stderr=%s", args, code, stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".conenv", "config.json")); !os.IsNotExist(err) {
		t.Fatalf("help saved the positional repository: %v", err)
	}
}
