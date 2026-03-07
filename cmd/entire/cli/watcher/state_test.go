package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/entireio/cli/cmd/entire/cli/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadState_FileNotExist(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	state, err := LoadState(tmpDir)
	require.NoError(t, err)
	assert.Empty(t, state.LastOpID)
	assert.True(t, state.LastRun.IsZero())
}

func TestSaveAndLoadState(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	now := time.Now().Truncate(time.Second)
	original := &State{
		LastOpID: "abc123def456",
		LastRun:  now,
	}

	err := SaveState(tmpDir, original)
	require.NoError(t, err)

	// Verify file was created.
	expectedPath := filepath.Join(tmpDir, paths.EntireDir, stateFileName)
	_, statErr := os.Stat(expectedPath)
	require.NoError(t, statErr)

	// Reload and compare.
	loaded, err := LoadState(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, original.LastOpID, loaded.LastOpID)
	assert.True(t, original.LastRun.Equal(loaded.LastRun),
		"expected %v, got %v", original.LastRun, loaded.LastRun)
}

func TestSaveState_CreatesEntireDir(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	entireDir := filepath.Join(tmpDir, paths.EntireDir)

	// Ensure .entire doesn't exist yet.
	_, err := os.Stat(entireDir)
	require.True(t, os.IsNotExist(err))

	err = SaveState(tmpDir, &State{LastOpID: "test"})
	require.NoError(t, err)

	// Now it should exist.
	info, err := os.Stat(entireDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestLoadState_InvalidJSON(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	entireDir := filepath.Join(tmpDir, paths.EntireDir)
	require.NoError(t, os.MkdirAll(entireDir, 0o750))

	err := os.WriteFile(filepath.Join(entireDir, stateFileName), []byte("not json"), 0o600)
	require.NoError(t, err)

	_, err = LoadState(tmpDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}
