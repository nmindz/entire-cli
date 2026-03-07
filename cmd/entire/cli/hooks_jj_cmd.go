package cli

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/entireio/cli/cmd/entire/cli/checkpoint/id"
	"github.com/entireio/cli/cmd/entire/cli/jj"
	"github.com/entireio/cli/cmd/entire/cli/logging"
	"github.com/entireio/cli/cmd/entire/cli/paths"
	"github.com/entireio/cli/cmd/entire/cli/settings"
	"github.com/entireio/cli/cmd/entire/cli/strategy"
	"github.com/entireio/cli/cmd/entire/cli/trailers"
	"github.com/entireio/cli/cmd/entire/cli/vcs"
	"github.com/entireio/cli/perf"

	"github.com/spf13/cobra"
)

// jjHooksDisabled is set by PersistentPreRunE when Entire is not set up,
// disabled, or the repo is not a JJ colocated repo.
// When true, all JJ hook commands return early without doing any work.
var jjHooksDisabled bool

// jjHookContext holds common state for JJ hook logging.
type jjHookContext struct {
	hookName string
	ctx      context.Context
	span     *perf.Span
	strategy *strategy.ManualCommitStrategy
}

// newJJHookContext creates a new JJ hook context with logging and a root perf span.
// The perf span ensures all perf.Start calls in strategy methods become child spans,
// producing a single perf log line per hook with a full timing breakdown.
// Callers must defer g.span.End() to emit the perf log.
func newJJHookContext(ctx context.Context, hookName string) *jjHookContext {
	ctx = logging.WithComponent(ctx, "hooks")
	ctx, span := perf.Start(ctx, hookName,
		slog.String("hook_type", "jj"))
	g := &jjHookContext{
		hookName: hookName,
		ctx:      ctx,
		span:     span,
	}
	g.strategy = GetStrategy(ctx)
	return g
}

// logInvoked logs that the hook was invoked.
func (g *jjHookContext) logInvoked(extraAttrs ...any) {
	attrs := []any{
		slog.String("hook", g.hookName),
		slog.String("hook_type", "jj"),
		slog.String("strategy", strategy.StrategyNameManualCommit),
	}
	logging.Debug(g.ctx, g.hookName+" hook invoked", append(attrs, extraAttrs...)...)
}

// logCompleted records the error on the perf span.
func (g *jjHookContext) logCompleted(err error) {
	g.span.RecordError(err)
}

func newHooksJJCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "jj",
		Short:  "JJ (Jujutsu) hook handlers",
		Long:   "Commands called after JJ operations. Use manually or via shell wrapper.",
		Hidden: true, // Internal command, not for direct user use
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			// Check if Entire is set up and enabled before doing any work.
			// This prevents hooks from running in repos where Entire was
			// never enabled or has been disabled.
			if !settings.IsSetUpAndEnabled(ctx) {
				jjHooksDisabled = true
				return nil
			}
			// JJ hooks only apply to colocated repos (both .jj/ and .git/).
			// JJ-only repos are not supported yet.
			if !vcs.IsColocated(ctx) {
				jjHooksDisabled = true
				return nil
			}
			hookLogCleanup = initHookLogging(ctx)
			return nil
		},
		PersistentPostRunE: func(_ *cobra.Command, _ []string) error {
			if hookLogCleanup != nil {
				hookLogCleanup()
			}
			return nil
		},
	}

	cmd.AddCommand(newHooksJJPostCommitCmd())
	cmd.AddCommand(newHooksJJPrePushCmd())

	return cmd
}

// newHooksJJPostCommitCmd creates the `entire hooks jj post-commit` command.
// This combines the work of prepare-commit-msg + post-commit for JJ:
// JJ bypasses git hooks entirely (commits via gitoxide), so we must both
// add the checkpoint trailer AND trigger condensation in a single step.
func newHooksJJPostCommitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "post-commit",
		Short: "Handle post-commit for JJ",
		Long:  "Adds checkpoint trailer to HEAD and condenses session data. Call after jj commit/new/amend.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if jjHooksDisabled {
				return nil
			}

			g := newJJHookContext(cmd.Context(), "jj-post-commit")
			defer g.span.End()
			g.logInvoked()

			hookErr := runJJPostCommit(g)
			g.logCompleted(hookErr)

			// Hooks must be resilient — swallow errors
			return nil
		},
	}
}

// runJJPostCommit performs the JJ post-commit logic:
// 1. Opens the git repository and reads HEAD
// 2. Checks for active sessions with content
// 3. Generates a checkpoint ID and adds the trailer via jj describe
// 4. Delegates to strategy.PostCommit for condensation
func runJJPostCommit(g *jjHookContext) error { //nolint:unparam // error return is part of the hook contract; hooks must be resilient and swallow errors
	ctx := g.ctx

	// Get worktree root for jj CLI operations
	repoRoot, err := paths.WorktreeRoot(ctx)
	if err != nil {
		logging.Warn(ctx, "jj-post-commit: failed to get worktree root",
			slog.String("error", err.Error()))
		return nil
	}

	// Open the git repository to read HEAD
	repo, err := strategy.OpenRepository(ctx)
	if err != nil {
		logging.Warn(ctx, "jj-post-commit: failed to open repository",
			slog.String("error", err.Error()))
		return nil
	}

	// Get HEAD commit
	head, err := repo.Head()
	if err != nil {
		logging.Warn(ctx, "jj-post-commit: failed to read HEAD",
			slog.String("error", err.Error()))
		return nil
	}

	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		logging.Warn(ctx, "jj-post-commit: failed to read HEAD commit",
			slog.String("error", err.Error()))
		return nil
	}

	// Check if trailer already exists (e.g., user already ran this command)
	if _, found := trailers.ParseCheckpoint(commit.Message); found {
		logging.Debug(ctx, "jj-post-commit: trailer already exists, skipping to PostCommit")
		// Trailer exists — still run PostCommit for condensation
		hookErr := g.strategy.PostCommit(ctx)
		g.logCompleted(hookErr)
		// Sync git ref changes into JJ after condensation
		if importErr := jj.GitImport(ctx, repoRoot); importErr != nil {
			logging.Warn(ctx, "jj-post-commit: failed to import git refs into jj",
				slog.String("error", importErr.Error()))
		}
		return nil
	}

	// Find active sessions for this worktree
	sessions, err := g.strategy.FindSessionsForWorktree(ctx, repoRoot)
	if err != nil || len(sessions) == 0 {
		logging.Debug(ctx, "jj-post-commit: no active sessions",
			slog.Int("sessions_found", len(sessions)))
		return nil //nolint:nilerr // No sessions — nothing to do
	}

	// Generate a fresh checkpoint ID
	checkpointID, err := id.Generate()
	if err != nil {
		logging.Warn(ctx, "jj-post-commit: failed to generate checkpoint ID",
			slog.String("error", err.Error()))
		return nil
	}

	// Build the trailer string
	trailer := fmt.Sprintf("%s: %s", trailers.CheckpointTrailerKey, checkpointID.String())

	// Add trailer to the HEAD commit via jj describe.
	// Use the git commit hash as the JJ revset — JJ accepts full git hashes.
	headHash := head.Hash().String()
	if err := jj.AppendToDescription(ctx, repoRoot, headHash, trailer); err != nil {
		logging.Warn(ctx, "jj-post-commit: failed to add trailer via jj describe",
			slog.String("error", err.Error()),
			slog.String("revset", headHash))
		return nil
	}

	logging.Info(ctx, "jj-post-commit: trailer added via jj describe",
		slog.String("checkpoint_id", checkpointID.String()),
		slog.String("revset", headHash))

	// Run PostCommit which re-reads HEAD (now with updated hash after describe)
	// and performs condensation.
	hookErr := g.strategy.PostCommit(ctx)
	g.logCompleted(hookErr)

	// Sync git ref changes (shadow branches, entire/checkpoints/v1) into JJ.
	// PostCommit may create/update shadow branches and condense to the metadata
	// branch via go-git, which bypasses JJ's bookmark tracking.
	if importErr := jj.GitImport(ctx, repoRoot); importErr != nil {
		logging.Warn(ctx, "jj-post-commit: failed to import git refs into jj",
			slog.String("error", importErr.Error()))
	}

	return nil
}

func newHooksJJPrePushCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pre-push [remote]",
		Short: "Handle pre-push for JJ",
		Long:  "Pushes entire/checkpoints/v1 branch alongside user push. Call before or after jj git push.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if jjHooksDisabled {
				return nil
			}

			remote := "origin"
			if len(args) > 0 {
				remote = args[0]
			}

			g := newJJHookContext(cmd.Context(), "jj-pre-push")
			defer g.span.End()
			g.logInvoked(slog.String("remote", remote))

			hookErr := g.strategy.PrePush(g.ctx, remote)
			g.logCompleted(hookErr)

			// Sync git ref changes into JJ after push operations.
			// PrePush may fetch/merge the metadata branch, updating refs.
			repoRoot, rootErr := paths.WorktreeRoot(g.ctx)
			if rootErr == nil {
				if importErr := jj.GitImport(g.ctx, repoRoot); importErr != nil {
					logging.Warn(g.ctx, "jj-pre-push: failed to import git refs into jj",
						slog.String("error", importErr.Error()))
				}
			}

			return nil
		},
	}
}
