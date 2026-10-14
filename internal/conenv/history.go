package conenv

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func LoadState() (State, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return State{}, err
	}
	path := filepath.Join(home, ".conenv", "state.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("decode %s: %w", path, err)
	}
	return state, nil
}

func FindRun(state State, id string) (RunRecord, error) {
	for i := len(state.Runs) - 1; i >= 0; i-- {
		if state.Runs[i].ID == id {
			return state.Runs[i], nil
		}
	}
	return RunRecord{}, fmt.Errorf("unknown run %q", id)
}

// LatestRunID returns the most recent apply that has not been undone, which is
// the default undo target.
func LatestRunID() (string, error) {
	state, err := LoadState()
	if err != nil {
		return "", err
	}
	if len(state.Runs) == 0 {
		return "", errors.New("no recorded runs to undo")
	}
	return state.Runs[len(state.Runs)-1].ID, nil
}

// RunBackupExists reports whether a run still has its backup directory, which
// is required before its changes can be undone.
func RunBackupExists(runID string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(home, ".conenv", "backups", runID, "run.json"))
	return err == nil
}
