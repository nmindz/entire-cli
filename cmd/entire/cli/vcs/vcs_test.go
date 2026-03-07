package vcs

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/entireio/cli/cmd/entire/cli/paths"
	"github.com/entireio/cli/cmd/entire/cli/testutil"
)

// setupGitRepo creates a git repo with an initial commit so that
// paths.WorktreeRoot can resolve it.
func setupGitRepo(t *testing.T, dir string) {
	t.Helper()
	testutil.InitRepo(t, dir)
	testutil.WriteFile(t, dir, "init.txt", "init")
	testutil.GitAdd(t, dir, "init.txt")
	testutil.GitCommit(t, dir, "initial commit")
}

// addJJDir creates a .jj/ directory in the given path.
func addJJDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".jj"), 0o755); err != nil {
		t.Fatalf("failed to create .jj directory: %v", err)
	}
}

func TestDetect_GitOnly(t *testing.T) {
	// Cannot use t.Parallel() with t.Chdir()
	tmpDir := t.TempDir()
	setupGitRepo(t, tmpDir)
	paths.ClearWorktreeRootCache()
	t.Chdir(tmpDir)

	got := Detect(context.Background())
	if got != GitOnly {
		t.Errorf("Detect() = %v, want %v", got, GitOnly)
	}
}

func TestDetect_JJColocated(t *testing.T) {
	// Cannot use t.Parallel() with t.Chdir()
	tmpDir := t.TempDir()
	setupGitRepo(t, tmpDir)
	addJJDir(t, tmpDir)
	paths.ClearWorktreeRootCache()
	t.Chdir(tmpDir)

	got := Detect(context.Background())
	if got != JJColocated {
		t.Errorf("Detect() = %v, want %v", got, JJColocated)
	}
}

func TestDetect_JJOnly(t *testing.T) {
	// Cannot use t.Parallel() with t.Chdir()
	tmpDir := t.TempDir()
	addJJDir(t, tmpDir)
	paths.ClearWorktreeRootCache()
	t.Chdir(tmpDir)

	got := Detect(context.Background())
	if got != JJOnly {
		t.Errorf("Detect() = %v, want %v", got, JJOnly)
	}
}

func TestDetect_Unknown(t *testing.T) {
	// Cannot use t.Parallel() with t.Chdir()
	tmpDir := t.TempDir()
	paths.ClearWorktreeRootCache()
	t.Chdir(tmpDir)

	got := Detect(context.Background())
	if got != Unknown {
		t.Errorf("Detect() = %v, want %v", got, Unknown)
	}
}

func TestIsColocated(t *testing.T) {
	// Cannot use t.Parallel() with t.Chdir()
	tmpDir := t.TempDir()
	setupGitRepo(t, tmpDir)
	addJJDir(t, tmpDir)
	paths.ClearWorktreeRootCache()
	t.Chdir(tmpDir)

	if !IsColocated(context.Background()) {
		t.Error("IsColocated() = false, want true")
	}
}

func TestIsColocated_GitOnly(t *testing.T) {
	// Cannot use t.Parallel() with t.Chdir()
	tmpDir := t.TempDir()
	setupGitRepo(t, tmpDir)
	paths.ClearWorktreeRootCache()
	t.Chdir(tmpDir)

	if IsColocated(context.Background()) {
		t.Error("IsColocated() = true, want false for git-only repo")
	}
}

func TestRequireColocated_Success(t *testing.T) {
	// Cannot use t.Parallel() with t.Chdir()
	tmpDir := t.TempDir()
	setupGitRepo(t, tmpDir)
	addJJDir(t, tmpDir)
	paths.ClearWorktreeRootCache()
	t.Chdir(tmpDir)

	err := RequireColocated(context.Background())
	if err != nil {
		t.Errorf("RequireColocated() error = %v, want nil", err)
	}
}

func TestRequireColocated_Failure(t *testing.T) {
	// Cannot use t.Parallel() with t.Chdir()
	tmpDir := t.TempDir()
	setupGitRepo(t, tmpDir)
	paths.ClearWorktreeRootCache()
	t.Chdir(tmpDir)

	err := RequireColocated(context.Background())
	if err == nil {
		t.Fatal("RequireColocated() error = nil, want error for git-only repo")
	}

	want := "JJ-only mode is not supported. Please use colocated mode (jj git init --colocate)"
	if err.Error() != want {
		t.Errorf("RequireColocated() error = %q, want %q", err.Error(), want)
	}
}

func TestVCSType_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		vcsType Type
		want    string
	}{
		{GitOnly, "Git"},
		{JJColocated, "Git + JJ (colocated)"},
		{JJOnly, "JJ (no Git)"},
		{Unknown, "Unknown"},
		{Type("other"), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(string(tt.vcsType), func(t *testing.T) {
			t.Parallel()
			got := tt.vcsType.String()
			if got != tt.want {
				t.Errorf("Type(%q).String() = %q, want %q", tt.vcsType, got, tt.want)
			}
		})
	}
}

func TestHasJJ_Colocated(t *testing.T) {
	// Cannot use t.Parallel() with t.Chdir()
	tmpDir := t.TempDir()
	setupGitRepo(t, tmpDir)
	addJJDir(t, tmpDir)
	paths.ClearWorktreeRootCache()
	t.Chdir(tmpDir)

	if !HasJJ(context.Background()) {
		t.Error("HasJJ() = false, want true for colocated repo")
	}
}

func TestHasJJ_JJOnly(t *testing.T) {
	// Cannot use t.Parallel() with t.Chdir()
	tmpDir := t.TempDir()
	addJJDir(t, tmpDir)
	paths.ClearWorktreeRootCache()
	t.Chdir(tmpDir)

	if !HasJJ(context.Background()) {
		t.Error("HasJJ() = false, want true for JJ-only repo")
	}
}

func TestHasJJ_GitOnly(t *testing.T) {
	// Cannot use t.Parallel() with t.Chdir()
	tmpDir := t.TempDir()
	setupGitRepo(t, tmpDir)
	paths.ClearWorktreeRootCache()
	t.Chdir(tmpDir)

	if HasJJ(context.Background()) {
		t.Error("HasJJ() = true, want false for git-only repo")
	}
}
