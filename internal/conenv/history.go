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
