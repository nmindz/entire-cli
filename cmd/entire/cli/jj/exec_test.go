package jj

import (
	"context"
	"testing"
)

func skipIfNoJJ(t *testing.T) {
	t.Helper()
	if !IsAvailable() {
		t.Skip("jj binary not available")
	}
}

func TestIsAvailable(t *testing.T) {
	t.Parallel()

	// Just verify it doesn't panic — result depends on CI environment.
	_ = IsAvailable()
}

func TestRunJJ_InvalidCommand(t *testing.T) {
	t.Parallel()
	skipIfNoJJ(t)

	// Run jj with a subcommand that does not exist.
	_, stderr, err := RunJJ(context.Background(), t.TempDir(), "this-subcommand-does-not-exist")
	if err == nil {
		t.Fatal("expected error for invalid jj subcommand")
	}

	// stderr should contain some diagnostic output from jj.
	if stderr == "" {
		t.Error("expected non-empty stderr for invalid subcommand")
	}
}

func TestRunJJ_InvalidDir(t *testing.T) {
	t.Parallel()
	skipIfNoJJ(t)

	// Run jj in a directory that is not a jj repo — should fail.
	_, _, err := RunJJ(context.Background(), t.TempDir(), "log", "--limit", "1")
	if err == nil {
		t.Fatal("expected error for non-jj directory")
	}
}

func TestGitImport_NoJJ(t *testing.T) {
	t.Parallel()
	// GitImport should be a no-op when jj is not available
	// We can't easily test with a real jj binary in unit tests,
	// but we can verify it doesn't panic on non-jj repos
	ctx := context.Background()
	err := GitImport(ctx, t.TempDir())
	// May return nil (jj not found) or error (not a jj repo) - both are acceptable
	_ = err
}

func TestGitExport_NoJJ(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	err := GitExport(ctx, t.TempDir())
	// May return nil (jj not found) or error (not a jj repo) - both are acceptable
	_ = err
}

func TestOperationType_IsCheckpointTrigger(t *testing.T) {
	t.Parallel()

	tests := []struct {
		op   OperationType
		want bool
	}{
		{OpSnapshot, false},
		{OpNew, true},
		{OpDescribe, true},
		{OpCommit, true},
		{OpAmend, true},
		{OpSquash, true},
		{OpRebase, false},
		{OpAbandon, false},
		{OpGitPush, false},
		{OpGitFetch, false},
		{OpEdit, false},
		{OpOther, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.op), func(t *testing.T) {
			t.Parallel()
			got := tt.op.IsCheckpointTrigger()
			if got != tt.want {
				t.Errorf("OperationType(%q).IsCheckpointTrigger() = %v, want %v", tt.op, got, tt.want)
			}
		})
	}
}

func TestOperationType_IsPushTrigger(t *testing.T) {
	t.Parallel()

	tests := []struct {
		op   OperationType
		want bool
	}{
		{OpSnapshot, false},
		{OpNew, false},
		{OpDescribe, false},
		{OpCommit, false},
		{OpAmend, false},
		{OpSquash, false},
		{OpRebase, false},
		{OpAbandon, false},
		{OpGitPush, true},
		{OpGitFetch, false},
		{OpEdit, false},
		{OpOther, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.op), func(t *testing.T) {
			t.Parallel()
			got := tt.op.IsPushTrigger()
			if got != tt.want {
				t.Errorf("OperationType(%q).IsPushTrigger() = %v, want %v", tt.op, got, tt.want)
			}
		})
	}
}

func TestOperationType_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		op   OperationType
		want string
	}{
		{OpSnapshot, "snapshot working copy"},
		{OpNew, "new empty commit"},
		{OpDescribe, "describe commit"},
		{OpCommit, "commit working copy"},
		{OpAmend, "amend/squash changes"},
		{OpSquash, "squash commits"},
		{OpRebase, "rebase commits"},
		{OpAbandon, "abandon commit"},
		{OpGitPush, "push to git remote"},
		{OpGitFetch, "fetch from git remote"},
		{OpEdit, "edit commit"},
		{OpOther, "other"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			got := tt.op.String()
			if got != tt.want {
				t.Errorf("OperationType(%q).String() = %q, want %q", tt.op, got, tt.want)
			}
		})
	}
}
