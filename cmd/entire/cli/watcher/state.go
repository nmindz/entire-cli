package watcher

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/entireio/cli/cmd/entire/cli/paths"
)

// stateFileName is the file within .entire/ that persists watcher state.
const stateFileName = "watcher-state.json"

// State holds persistent state for the watcher between runs.
type State struct {
	// LastOpID is the most recently processed JJ operation ID.
	LastOpID string `json:"last_op_id"`
	// LastRun is the time the watcher last processed an operation.
	LastRun time.Time `json:"last_run"`
}

// statePath returns the full path to the watcher state file within the repo.
func statePath(dir string) string {
	return filepath.Join(dir, paths.EntireDir, stateFileName)
}

// LoadState reads persisted watcher state from .entire/watcher-state.json.
// Returns a zero-value State (not an error) if the file does not exist.
func LoadState(dir string) (*State, error) {
	data, err := os.ReadFile(statePath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return &State{}, nil
		}
		return nil, fmt.Errorf("load watcher state: %w", err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("load watcher state: parse: %w", err)
	}

	return &state, nil
}

// SaveState writes watcher state to .entire/watcher-state.json.
// Creates the .entire/ directory if it doesn't exist.
func SaveState(dir string, state *State) error {
	entireDir := filepath.Join(dir, paths.EntireDir)
	if err := os.MkdirAll(entireDir, 0o750); err != nil {
		return fmt.Errorf("save watcher state: mkdir: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("save watcher state: marshal: %w", err)
	}

	if err := os.WriteFile(statePath(dir), data, 0o600); err != nil {
		return fmt.Errorf("save watcher state: write: %w", err)
	}

	return nil
}
