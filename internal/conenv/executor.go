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
	run := RunRecord{ID: time.Now().UTC().Format("20060102T150405.000000000Z"), Profile: plan.Profile, CreatedAt: time.Now().UTC(), Resources: plan.Resources}
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
	data, err := os.ReadFile(filepath.Join(home, ".conenv", "backups", runID, "run.json"))
	if err != nil {
		return err
	}
	var run RunRecord
	if err := json.Unmarshal(data, &run); err != nil {
		return err
	}
	return rollback(run.Changes)
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
	stateData, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(statePath, append(stateData, '\n'), 0o600)
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
			if err := copyEntry(filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name())); err != nil {
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
