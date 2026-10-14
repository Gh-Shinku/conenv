package conenv

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func Apply(plan *Plan) (*RunRecord, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	state, err := LoadState()
	if err != nil {
		return nil, err
	}
	run := RunRecord{
		ID: time.Now().UTC().Format("20060102T150405.000000000Z"), Profile: plan.Profile,
		CreatedAt: time.Now().UTC(), Resources: plan.Resources,
		Previous: &StateSnapshot{ActiveProfile: state.ActiveProfile, LastRun: state.LastRun, Managed: append([]ManagedResource(nil), state.Managed...)},
	}
	backupRoot := filepath.Join(home, ".conenv", "backups", run.ID)

	for _, action := range plan.Actions {
		if action.Kind != "install_package" {
			continue
		}
		if err := installPackage(action); err != nil {
			return &run, err
		}
		run.Packages = append(run.Packages, action.Manager+":"+action.PackageID)
	}
	for _, action := range plan.Actions {
		if action.Kind == "install_package" {
			continue
		}
		record, err := backupTarget(action.Target, backupRoot)
		if err != nil {
			rollback(run.Changes)
			return &run, fmt.Errorf("backup %s: %w", action.Target, err)
		}
		run.Changes = append(run.Changes, record)
		if err := applyFilesystemAction(action); err != nil {
			rollback(run.Changes)
			return &run, fmt.Errorf("%s: %w", action.Summary, err)
		}
	}
	if err := saveRun(home, run); err != nil {
		rollback(run.Changes)
		return &run, err
	}
	return &run, nil
}

func applyFilesystemAction(action Action) error {
	if err := os.MkdirAll(filepath.Dir(action.Target), 0o755); err != nil {
		return err
	}
	switch action.Kind {
	case "link", "link_skill":
		return os.Symlink(action.Source, action.Target)
	case "render", "codex_config":
		mode := os.FileMode(0o644)
		if action.Sensitive {
			mode = 0o600
		}
		return atomicWrite(action.Target, action.Content, mode)
	default:
		return fmt.Errorf("unsupported action kind %q", action.Kind)
	}
}

func backupTarget(target, backupRoot string) (BackupRecord, error) {
	record := BackupRecord{Target: target}
	if _, err := os.Lstat(target); os.IsNotExist(err) {
		return record, nil
	} else if err != nil {
		return record, err
	}
	record.Existed = true
	hash := sha256.Sum256([]byte(target))
	record.Backup = filepath.Join(backupRoot, hex.EncodeToString(hash[:8])) + "-" + filepath.Base(target)
	if err := os.MkdirAll(filepath.Dir(record.Backup), 0o700); err != nil {
		return record, err
	}
	if err := copyEntry(target, record.Backup); err != nil {
		return record, err
	}
	if err := os.RemoveAll(target); err != nil {
		return record, err
	}
	return record, nil
}

func Restore(runID string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	run, err := loadRun(home, runID)
	if err != nil {
		return err
	}
	if err := validateRestoreBackups(run); err != nil {
		return err
	}
	if err := rollback(run.Changes); err != nil {
		return err
	}
	state, err := LoadState()
	if err != nil {
		return err
	}
	if run.Previous != nil && state.LastRun == run.ID {
		state.ActiveProfile = run.Previous.ActiveProfile
		state.LastRun = run.Previous.LastRun
		state.Managed = append([]ManagedResource(nil), run.Previous.Managed...)
	} else if run.Previous != nil {
		state.Managed = restoreManagedSubset(state.Managed, run.Resources, run.Previous.Managed)
	} else {
		state.Managed = removeManaged(state.Managed, run.Resources)
		if state.LastRun == run.ID {
			state.LastRun = ""
		}
	}
	state.Restores = append(state.Restores, RestoreRecord{RunID: run.ID, RestoredAt: time.Now().UTC()})
	state.Runs = removeRun(state.Runs, run.ID)
	return saveState(home, state)
}

func PlanRestore(runID string) (*RestorePlan, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	run, err := loadRun(home, runID)
	if err != nil {
		return nil, err
	}
	if err := validateRestoreBackups(run); err != nil {
		return nil, err
	}
	plan := &RestorePlan{RunID: run.ID, Profile: run.Profile, PackagesRetained: append([]string(nil), run.Packages...)}
	for index := len(run.Changes) - 1; index >= 0; index-- {
		record := run.Changes[index]
		action := RestoreAction{Target: record.Target, Action: "remove"}
		if record.Existed {
			action.Action = "restore"
			action.Backup = record.Backup
		}
		plan.Actions = append(plan.Actions, action)
	}
	return plan, nil
}

func validateRestoreBackups(run RunRecord) error {
	for _, record := range run.Changes {
		if record.Existed {
			if _, err := os.Lstat(record.Backup); err != nil {
				return fmt.Errorf("restore backup for %s: %w", record.Target, err)
			}
		}
	}
	return nil
}

func loadRun(home, runID string) (RunRecord, error) {
	data, err := os.ReadFile(filepath.Join(home, ".conenv", "backups", runID, "run.json"))
	if err != nil {
		return RunRecord{}, err
	}
	var run RunRecord
	if err := json.Unmarshal(data, &run); err != nil {
		return RunRecord{}, err
	}
	return run, nil
}

func rollback(records []BackupRecord) error {
	var errs []error
	for i := len(records) - 1; i >= 0; i-- {
		record := records[i]
		if err := os.RemoveAll(record.Target); err != nil {
			errs = append(errs, err)
			continue
		}
		if record.Existed {
			if err := os.MkdirAll(filepath.Dir(record.Target), 0o755); err != nil {
				errs = append(errs, err)
				continue
			}
			if err := copyEntry(record.Backup, record.Target); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func saveRun(home string, run RunRecord) error {
	runData, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return err
	}
	runPath := filepath.Join(home, ".conenv", "backups", run.ID, "run.json")
	if err := atomicWrite(runPath, append(runData, '\n'), 0o600); err != nil {
		return err
	}
	statePath := filepath.Join(home, ".conenv", "state.json")
	state := State{}
	if data, err := os.ReadFile(statePath); err == nil {
		_ = json.Unmarshal(data, &state)
	}
	state.ActiveProfile = run.Profile
	state.LastRun = run.ID
	state.Managed = run.Resources
	state.Runs = append(state.Runs, run)
	if len(state.Runs) > 50 {
		state.Runs = state.Runs[len(state.Runs)-50:]
	}
	return saveState(home, state)
}

func saveState(home string, state State) error {
	stateData, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	statePath := filepath.Join(home, ".conenv", "state.json")
	return atomicWrite(statePath, append(stateData, '\n'), 0o600)
}

// removeRun drops a run from the recorded history, which keeps an undone run
// out of the default undo target and out of the run listing.
func removeRun(current []RunRecord, runID string) []RunRecord {
	filtered := make([]RunRecord, 0, len(current))
	for _, run := range current {
		if run.ID != runID {
			filtered = append(filtered, run)
		}
	}
	return filtered
}

func removeManaged(current, removed []ManagedResource) []ManagedResource {
	targets := make(map[string]bool, len(removed))
	for _, resource := range removed {
		targets[resource.Kind+"\x00"+resource.Target] = true
	}
	filtered := make([]ManagedResource, 0, len(current))
	for _, resource := range current {
		if !targets[resource.Kind+"\x00"+resource.Target] {
			filtered = append(filtered, resource)
		}
	}
	return filtered
}

func restoreManagedSubset(current, removed, previous []ManagedResource) []ManagedResource {
	filtered := removeManaged(current, removed)
	affected := make(map[string]bool, len(removed))
	for _, resource := range removed {
		affected[resource.Kind+"\x00"+resource.Target] = true
	}
	for _, resource := range previous {
		if affected[resource.Kind+"\x00"+resource.Target] {
			filtered = append(filtered, resource)
		}
	}
	return filtered
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".conenv-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempName, path); err == nil {
		return nil
	}
	// Windows does not replace an existing file with os.Rename. The temporary
	// file is already fully written and synced, so remove only a regular target
	// and retry the final rename.
	info, statErr := os.Lstat(path)
	if statErr != nil {
		return statErr
	}
	if info.IsDir() {
		return fmt.Errorf("cannot replace directory %s with a file", path)
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}

func copyEntry(source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		link, err := os.Readlink(source)
		if err != nil {
			return err
		}
		return os.Symlink(link, target)
	}
	if info.IsDir() {
		if err := os.MkdirAll(target, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			childSource := filepath.Join(source, entry.Name())
			// ReadDir follows a directory symlink far enough to list it, so
			// preserve nested links as links instead of copying the target.
			if entry.Type()&os.ModeSymlink != 0 {
				link, err := os.Readlink(childSource)
				if err != nil {
					return err
				}
				if err := os.Symlink(link, filepath.Join(target, entry.Name())); err != nil {
					return err
				}
				continue
			}
			if err := copyEntry(childSource, filepath.Join(target, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	return errors.Join(copyErr, closeErr)
}
