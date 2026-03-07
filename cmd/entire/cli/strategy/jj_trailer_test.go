package strategy

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/entireio/cli/cmd/entire/cli/checkpoint/id"
	"github.com/entireio/cli/cmd/entire/cli/paths"
	"github.com/entireio/cli/cmd/entire/cli/testutil"
	"github.com/entireio/cli/cmd/entire/cli/trailers"
	"github.com/entireio/cli/cmd/entire/cli/vcs"
)

// getHeadMessage reads the HEAD commit message from the given repo directory.
func getHeadMessage(t *testing.T, repoDir string) string {
	t.Helper()

	cmd := exec.CommandContext(context.Background(), "git", "log", "-1", "--format=%B")
	cmd.Dir = repoDir
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to read HEAD message: %v", err)
	}
	return out.String()
}

func TestAddTrailerViaGitAmend(t *testing.T) {
	// Uses t.Chdir — cannot be parallel
	tmpDir := t.TempDir()
	testutil.InitRepo(t, tmpDir)
	testutil.WriteFile(t, tmpDir, "file.txt", "hello")
	testutil.GitAdd(t, tmpDir, "file.txt")
	testutil.GitCommit(t, tmpDir, "initial commit")
	t.Chdir(tmpDir)
	paths.ClearWorktreeRootCache()

	cpID := id.MustCheckpointID("aabbccddeeff")
	ctx := context.Background()

	if err := AddTrailerViaGitAmend(ctx, cpID); err != nil {
		t.Fatalf("AddTrailerViaGitAmend failed: %v", err)
	}

	msg := getHeadMessage(t, tmpDir)
	if !strings.Contains(msg, trailers.CheckpointTrailerKey+": aabbccddeeff") {
		t.Errorf("expected trailer in HEAD message, got:\n%s", msg)
	}
}

func TestAddTrailerViaGitAmend_Idempotent(t *testing.T) {
	// Uses t.Chdir — cannot be parallel
	tmpDir := t.TempDir()
	testutil.InitRepo(t, tmpDir)
	testutil.WriteFile(t, tmpDir, "file.txt", "hello")
	testutil.GitAdd(t, tmpDir, "file.txt")
	testutil.GitCommit(t, tmpDir, "initial commit")
	t.Chdir(tmpDir)
	paths.ClearWorktreeRootCache()

	cpID := id.MustCheckpointID("112233445566")
	ctx := context.Background()

	// First call — should add trailer
	if err := AddTrailerViaGitAmend(ctx, cpID); err != nil {
		t.Fatalf("first AddTrailerViaGitAmend failed: %v", err)
	}

	// Second call — should be a no-op
	if err := AddTrailerViaGitAmend(ctx, cpID); err != nil {
		t.Fatalf("second AddTrailerViaGitAmend failed: %v", err)
	}

	msg := getHeadMessage(t, tmpDir)
	count := strings.Count(msg, trailers.CheckpointTrailerKey+":")
	if count != 1 {
		t.Errorf("expected exactly 1 trailer occurrence, found %d in:\n%s", count, msg)
	}
}

func TestAddTrailerViaGitAmend_PreservesOriginalMessage(t *testing.T) {
	// Uses t.Chdir — cannot be parallel
	tmpDir := t.TempDir()
	testutil.InitRepo(t, tmpDir)
	testutil.WriteFile(t, tmpDir, "file.txt", "hello")
	testutil.GitAdd(t, tmpDir, "file.txt")
	testutil.GitCommit(t, tmpDir, "feat: add hello feature\n\nThis is a detailed description.")
	t.Chdir(tmpDir)
	paths.ClearWorktreeRootCache()

	cpID := id.MustCheckpointID("aabbccddeeff")
	ctx := context.Background()

	if err := AddTrailerViaGitAmend(ctx, cpID); err != nil {
		t.Fatalf("AddTrailerViaGitAmend failed: %v", err)
	}

	msg := getHeadMessage(t, tmpDir)
	if !strings.Contains(msg, "feat: add hello feature") {
		t.Error("original subject line missing from amended message")
	}
	if !strings.Contains(msg, "This is a detailed description.") {
		t.Error("original body missing from amended message")
	}
	if !strings.Contains(msg, trailers.CheckpointTrailerKey+": aabbccddeeff") {
		t.Error("trailer missing from amended message")
	}
}

func TestAddTrailerToHead_GitOnly(t *testing.T) {
	// Uses t.Chdir — cannot be parallel
	tmpDir := t.TempDir()
	testutil.InitRepo(t, tmpDir)
	testutil.WriteFile(t, tmpDir, "file.txt", "hello")
	testutil.GitAdd(t, tmpDir, "file.txt")
	testutil.GitCommit(t, tmpDir, "test commit")
	t.Chdir(tmpDir)
	paths.ClearWorktreeRootCache()

	cpID := id.MustCheckpointID("ffeeddccbbaa")
	ctx := context.Background()

	if err := AddTrailerToHead(ctx, cpID, vcs.GitOnly, "@-"); err != nil {
		t.Fatalf("AddTrailerToHead with GitOnly failed: %v", err)
	}

	msg := getHeadMessage(t, tmpDir)
	if !strings.Contains(msg, trailers.CheckpointTrailerKey+": ffeeddccbbaa") {
		t.Errorf("expected trailer in HEAD message, got:\n%s", msg)
	}
}

func TestAddTrailerToHead_UnknownVCS(t *testing.T) {
	// Uses t.Chdir — cannot be parallel
	tmpDir := t.TempDir()
	testutil.InitRepo(t, tmpDir)
	testutil.WriteFile(t, tmpDir, "file.txt", "hello")
	testutil.GitAdd(t, tmpDir, "file.txt")
	testutil.GitCommit(t, tmpDir, "test commit")
	t.Chdir(tmpDir)
	paths.ClearWorktreeRootCache()

	cpID := id.MustCheckpointID("aabb11223344")
	ctx := context.Background()

	// Unknown VCS falls back to git amend
	if err := AddTrailerToHead(ctx, cpID, vcs.Unknown, "@-"); err != nil {
		t.Fatalf("AddTrailerToHead with Unknown VCS failed: %v", err)
	}

	msg := getHeadMessage(t, tmpDir)
	if !strings.Contains(msg, trailers.CheckpointTrailerKey+": aabb11223344") {
		t.Errorf("expected trailer in HEAD message, got:\n%s", msg)
	}
}
