package strategy

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/entireio/cli/cmd/entire/cli/checkpoint/id"
	"github.com/entireio/cli/cmd/entire/cli/jj"
	"github.com/entireio/cli/cmd/entire/cli/logging"
	"github.com/entireio/cli/cmd/entire/cli/paths"
	"github.com/entireio/cli/cmd/entire/cli/trailers"
	"github.com/entireio/cli/cmd/entire/cli/vcs"
)

// AddTrailerViaJJ adds an Entire-Checkpoint trailer to a JJ commit identified
// by the given revset. It reads the current description, appends the trailer
// (if not already present), and writes it back via jj describe.
//
// The revset parameter determines which commit to annotate:
//   - "@-" after jj new (the frozen commit is the parent of working copy)
//   - "@" after jj describe (the current working copy commit)
func AddTrailerViaJJ(ctx context.Context, checkpointID id.CheckpointID, revset string) error {
	worktreeRoot, err := paths.WorktreeRoot(ctx)
	if err != nil {
		return fmt.Errorf("get worktree root: %w", err)
	}

	trailer := trailers.CheckpointTrailerKey + ": " + checkpointID.String()
	if err := jj.AppendToDescription(ctx, worktreeRoot, revset, trailer); err != nil {
		return fmt.Errorf("add trailer via jj describe: %w", err)
	}

	logging.Debug(logging.WithComponent(ctx, "checkpoint"), "added trailer via jj",
		slog.String("checkpoint_id", checkpointID.String()),
		slog.String("revset", revset),
	)

	return nil
}

// AddTrailerViaGitAmend adds an Entire-Checkpoint trailer to the current HEAD
// commit by amending it with the updated message. This is used as a fallback
// when JJ is not available or when operating in Git-only mode.
//
// The function is idempotent: if the trailer is already present, it's a no-op.
// Uses --no-verify to skip hooks and prevent recursion.
func AddTrailerViaGitAmend(ctx context.Context, checkpointID id.CheckpointID) error {
	worktreeRoot, err := paths.WorktreeRoot(ctx)
	if err != nil {
		return fmt.Errorf("get worktree root: %w", err)
	}

	// Read current HEAD commit message
	logCmd := exec.CommandContext(ctx, "git", "log", "-1", "--format=%B")
	logCmd.Dir = worktreeRoot
	var logOut bytes.Buffer
	logCmd.Stdout = &logOut
	if err := logCmd.Run(); err != nil {
		return fmt.Errorf("read HEAD commit message: %w", err)
	}

	message := strings.TrimRight(logOut.String(), "\n")

	// Check if trailer already exists (idempotent)
	if _, found := trailers.ParseCheckpoint(message); found {
		logging.Debug(logging.WithComponent(ctx, "checkpoint"), "trailer already present in HEAD, skipping git amend",
			slog.String("checkpoint_id", checkpointID.String()),
		)
		return nil
	}

	// Add trailer using the existing formatting function
	newMessage := addCheckpointTrailer(message, checkpointID)

	// Amend HEAD with the updated message. --no-verify prevents hook recursion.

	amendCmd := exec.CommandContext(ctx, "git", "commit", "--amend", "--no-verify", "-m", newMessage)
	amendCmd.Dir = worktreeRoot
	if output, err := amendCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit --amend: %w: %s", err, strings.TrimSpace(string(output)))
	}

	logging.Debug(logging.WithComponent(ctx, "checkpoint"), "added trailer via git amend",
		slog.String("checkpoint_id", checkpointID.String()),
	)

	return nil
}

// AddTrailerToHead adds an Entire-Checkpoint trailer to the most recent commit
// using the appropriate method for the detected VCS type.
//
// For JJ colocated repos: tries jj describe first, falls back to git amend.
// For Git-only repos: uses git commit --amend directly.
//
// The revset parameter specifies which JJ revision to annotate when using JJ
// (e.g., "@-" for the parent of working copy after jj new).
func AddTrailerToHead(ctx context.Context, checkpointID id.CheckpointID, vcsType vcs.Type, revset string) error {
	if vcsType == vcs.JJColocated && jj.IsAvailable() {
		if err := AddTrailerViaJJ(ctx, checkpointID, revset); err != nil {
			// Fall back to git amend on JJ failure
			logging.Warn(logging.WithComponent(ctx, "checkpoint"), "jj trailer failed, falling back to git amend",
				slog.String("error", err.Error()),
				slog.String("checkpoint_id", checkpointID.String()),
			)
			return AddTrailerViaGitAmend(ctx, checkpointID)
		}
		return nil
	}

	// Git-only or JJ not available
	return AddTrailerViaGitAmend(ctx, checkpointID)
}
