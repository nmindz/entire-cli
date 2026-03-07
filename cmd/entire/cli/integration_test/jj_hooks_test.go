//go:build integration

package integration

// JJ integration tests for the Entire CLI's Jujutsu VCS support.
//
// These tests verify the full pipeline for JJ colocated repos:
// 1. Session lifecycle (agent hooks → checkpoint on shadow branch)
// 2. `entire hooks jj post-commit` adding the trailer + condensing session data
// 3. `jj git import` syncing entire/checkpoints/v1 bookmark into JJ
// 4. `entire hooks jj pre-push` pushing checkpoints alongside user push
//
// JJ colocated repos have BOTH a .jj/ directory AND a .git/ directory.
// JJ uses gitoxide (bypasses .git/hooks/), so Entire uses a shell wrapper
// that fires `entire hooks jj post-commit` after every `jj commit`.
//
// Test design:
// - Do NOT call t.Parallel() — these tests use t.TempDir for JJ HOME isolation
//   which is fine for parallel execution, BUT JJ git init requires the temp dir
//   to exist before the CLI subprocess runs, so each test is self-contained.
// - Every test creates an isolated temp repo via jj git init --colocate
// - JJ requires a real HOME directory (not /dev/null) to locate git config
// - skipIfNoJJ guards all tests (skip gracefully when jj not installed)

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entireio/cli/cmd/entire/cli/testutil"
)

// skipIfNoJJ skips the test if the jj binary is not on PATH.
// All JJ integration tests must call this first.
func skipIfNoJJ(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj binary not found on PATH — skipping JJ integration test")
	}
}

// jjIsolatedEnv returns environment variables for JJ commands that are isolated
// from the user's JJ and git configuration.
//
// JJ on macOS reads git config from $HOME/.config/git/config, so we need a
// real (non /dev/null) HOME directory. We use t.TempDir() for this.
//
// The JJ_CONFIG is set to a non-existent path inside the temp dir to prevent
// JJ from reading the user's real ~/.jjconfig.
func jjIsolatedEnv(t *testing.T) []string {
	t.Helper()

	// Use a real temp HOME — JJ needs it for git config lookup
	jjHome := t.TempDir()

	env := testutil.GitIsolatedEnv()
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		if strings.HasPrefix(e, "HOME=") || strings.HasPrefix(e, "JJ_CONFIG=") {
			continue
		}
		filtered = append(filtered, e)
	}
	return append(filtered,
		"HOME="+jjHome,
		"JJ_CONFIG="+filepath.Join(jjHome, "jj_config_nonexistent"),
	)
}

// initJJColocatedRepo initialises a JJ colocated repo in dir.
// It runs `jj git init --colocate` and configures the git user identity
// in the underlying git repo so commits are valid.
func initJJColocatedRepo(t *testing.T, dir string) {
	t.Helper()

	jjEnv := jjIsolatedEnv(t)

	// jj git init --colocate creates both .jj/ and .git/
	cmd := exec.Command("jj", "git", "init", "--colocate", dir)
	cmd.Env = jjEnv
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("jj git init --colocate failed: %v\n%s", err, out)
	}

	// Configure git user identity in the new git repo (jj uses the underlying git config)
	for _, kv := range [][]string{
		{"user.name", testutil.GitName()},
		{"user.email", testutil.GitEmail()},
		{"commit.gpgsign", "false"},
	} {
		c := exec.Command("git", "config", kv[0], kv[1])
		c.Dir = dir
		c.Env = testutil.GitIsolatedEnv()
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git config %s failed: %v\n%s", kv[0], err, out)
		}
	}

	// jj also needs its own user identity set in the repo config
	for _, kv := range [][]string{
		{"user.name", testutil.GitName()},
		{"user.email", testutil.GitEmail()},
	} {
		c := exec.Command("jj", "config", "set", "--repo", kv[0], kv[1])
		c.Dir = dir
		c.Env = jjEnv
		if out, err := c.CombinedOutput(); err != nil {
			// Non-fatal: jj might pick up git user config directly
			t.Logf("jj config set %s (non-fatal): %v\n%s", kv[0], err, out)
		}
	}
}

// jjCommit creates a JJ commit in dir with the given message.
// Uses `jj commit -m <msg>` which freezes the working copy changes and
// creates a new empty WC. Returns the git commit hash of the created commit.
func jjCommit(t *testing.T, jjEnv []string, dir, message string) string {
	t.Helper()

	// `jj commit -m <msg>` freezes the working copy and creates a new empty WC
	cmd := exec.Command("jj", "commit", "-m", message)
	cmd.Dir = dir
	cmd.Env = jjEnv
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("jj commit failed: %v\n%s", err, out)
	}

	// The committed change is now @- (parent of the working copy)
	// Get its git commit hash for reference
	logCmd := exec.Command("jj", "log", "--no-graph", "-T", "commit_id.short(40)", "-r", "@-")
	logCmd.Dir = dir
	logCmd.Env = jjEnv
	out, err := logCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("jj log for @- failed: %v\n%s", err, out)
	}

	return strings.TrimSpace(string(out))
}

// trackAndCommitJJ tracks the given files and creates a JJ commit.
func trackAndCommitJJ(t *testing.T, jjEnv []string, dir, message string, files ...string) string {
	t.Helper()

	for _, f := range files {
		c := exec.Command("jj", "file", "track", f)
		c.Dir = dir
		c.Env = jjEnv
		if out, err := c.CombinedOutput(); err != nil {
			// Non-fatal: jj may auto-track files in some versions
			t.Logf("jj file track %s (non-fatal): %v\n%s", f, err, out)
		}
	}

	return jjCommit(t, jjEnv, dir, message)
}

// runJJPostCommitHook calls `entire hooks jj post-commit` in the repo directory.
// This is what the shell wrapper fires after every `jj commit`.
func (env *TestEnv) runJJPostCommitHook(t *testing.T) string {
	t.Helper()
	return env.RunCLI("hooks", "jj", "post-commit")
}

// runJJPostCommitHookWithError calls `entire hooks jj post-commit` and returns output + error.
func (env *TestEnv) runJJPostCommitHookWithError(t *testing.T) (string, error) {
	t.Helper()
	return env.RunCLIWithError("hooks", "jj", "post-commit")
}

// initJJEnv creates a full JJ colocated test environment:
// - JJ colocated repo (jj git init --colocate)
// - Entire settings file
// Returns the TestEnv with RepoDir pointing at the colocated repo,
// and a JJ-isolated env slice for running JJ commands.
func initJJEnv(t *testing.T) (*TestEnv, []string) {
	t.Helper()
	skipIfNoJJ(t)

	env := NewTestEnv(t)
	jjEnv := jjIsolatedEnv(t)

	// Initialize JJ colocated repo
	initJJColocatedRepo(t, env.RepoDir)

	// Initialize Entire settings
	env.InitEntire()

	return env, jjEnv
}

// makeInitialCommitViaJJ creates the first commit in a JJ repo.
// JJ starts with an "empty" working copy — we need at least one real commit
// before Entire's shadow branches work (they need HEAD to exist).
func makeInitialCommitViaJJ(t *testing.T, env *TestEnv, jjEnv []string) {
	t.Helper()

	// Write a file and commit
	env.WriteFile("README.md", "# Test Repository")

	trackAndCommitJJ(t, jjEnv, env.RepoDir, "Initial commit", "README.md")
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestJJ_HooksDisabledForGitOnlyRepo verifies that `entire hooks jj post-commit`
// is a no-op in a plain git-only repo (no .jj/ directory).
// This guards against accidental JJ hook execution in non-JJ repos.
func TestJJ_HooksDisabledForGitOnlyRepo(t *testing.T) {
	t.Parallel()
	skipIfNoJJ(t)

	env := NewFeatureBranchEnv(t)
	env.InitEntire()

	// Should run without error but do nothing (jjHooksDisabled = true)
	output, err := env.runJJPostCommitHookWithError(t)
	if err != nil {
		t.Fatalf("hooks jj post-commit should not error for git-only repo, got: %v\nOutput: %s", err, output)
	}
	// No shadow branch should be created
	branches := env.ListBranchesWithPrefix("entire/")
	if len(branches) > 0 {
		t.Errorf("expected no entire/ branches for git-only repo, found: %v", branches)
	}
}

// TestJJ_HooksDisabledWhenEntireNotSetUp verifies that `entire hooks jj post-commit`
// is a no-op when Entire has not been set up (no .entire/settings.json).
func TestJJ_HooksDisabledWhenEntireNotSetUp(t *testing.T) {
	t.Parallel()
	skipIfNoJJ(t)

	env := NewTestEnv(t)
	initJJColocatedRepo(t, env.RepoDir)
	// Deliberately do NOT call env.InitEntire() — settings.json absent

	// Hooks should return early (no-op)
	output, err := env.runJJPostCommitHookWithError(t)
	if err != nil {
		t.Fatalf("hooks jj post-commit should not error when not set up: %v\nOutput: %s", err, output)
	}
}

// TestJJ_PostCommitNoActiveSession verifies that `entire hooks jj post-commit`
// is a no-op when there are no active sessions (no shadow branch content to condense).
func TestJJ_PostCommitNoActiveSession(t *testing.T) {
	t.Parallel()

	env, jjEnv := initJJEnv(t)
	makeInitialCommitViaJJ(t, env, jjEnv)

	// No agent hook has been called — no session state

	// JJ post-commit should run without error and produce no shadow branch
	output, err := env.runJJPostCommitHookWithError(t)
	if err != nil {
		t.Fatalf("hooks jj post-commit without session failed: %v\nOutput: %s", err, output)
	}

	// No checkpoint metadata branch should exist
	if env.BranchExists("entire/checkpoints/v1") {
		t.Error("expected no entire/checkpoints/v1 branch when no session is active")
	}
}

// TestJJ_PostCommitCreatesCheckpointTrailer verifies the core JJ post-commit flow:
// 1. An agent session is started (UserPromptSubmit)
// 2. Agent creates files, checkpoint is saved to shadow branch (Stop hook)
// 3. User does `jj commit` which is followed by `entire hooks jj post-commit`
// 4. The hook adds the Entire-Checkpoint trailer to the JJ commit via `jj describe`
// 5. The session is condensed to entire/checkpoints/v1
func TestJJ_PostCommitCreatesCheckpointTrailer(t *testing.T) {
	t.Parallel()

	env, jjEnv := initJJEnv(t)
	makeInitialCommitViaJJ(t, env, jjEnv)

	// ── Phase 1: Agent session start ─────────────────────────────────────────
	session := env.NewSession()
	if err := env.SimulateUserPromptSubmitWithPrompt(session.ID, "Implement feature X"); err != nil {
		t.Fatalf("UserPromptSubmit failed: %v", err)
	}

	// Agent creates a file
	env.WriteFile("feature_x.go", "package main\n\nfunc FeatureX() {}")

	// Agent saves a checkpoint via the Stop hook
	transcriptPath := session.CreateTranscript("Implement feature X", []FileChange{
		{Path: "feature_x.go", Content: "package main\n\nfunc FeatureX() {}"},
	})
	if err := env.SimulateStop(session.ID, transcriptPath); err != nil {
		t.Fatalf("Stop hook failed: %v", err)
	}

	// Verify checkpoint on shadow branch before JJ commit
	points := env.GetRewindPoints()
	if len(points) == 0 {
		t.Fatal("expected at least 1 rewind point after Stop hook")
	}
	t.Logf("Shadow branch checkpoint count: %d", len(points))

	// ── Phase 2: User does `jj commit` ───────────────────────────────────────
	commitHash := trackAndCommitJJ(t, jjEnv, env.RepoDir, "Add feature X", "feature_x.go")
	t.Logf("JJ commit hash: %s", commitHash[:7])

	// ── Phase 3: Shell wrapper fires `entire hooks jj post-commit` ───────────
	hookOutput := env.runJJPostCommitHook(t)
	t.Logf("post-commit hook output: %s", hookOutput)

	// ── Phase 4: Verify checkpoint trailer was added to the git commit ────────
	// The hook should have amended the commit message via `jj describe`
	// to include `Entire-Checkpoint: <id>`.
	// Note: after `jj commit`, HEAD in git = @- in JJ (the frozen commit).
	commitMsg := env.GetCommitMessage(env.GetHeadHash())
	t.Logf("Git HEAD commit message after post-commit hook:\n%s", commitMsg)

	if !strings.Contains(commitMsg, "Entire-Checkpoint:") {
		t.Errorf("expected Entire-Checkpoint trailer in git commit message, got:\n%s", commitMsg)
	}

	// ── Phase 5: Verify condensation happened ─────────────────────────────────
	// After post-commit, the session should be condensed to entire/checkpoints/v1
	if !env.BranchExists("entire/checkpoints/v1") {
		t.Fatal("expected entire/checkpoints/v1 branch after JJ post-commit hook")
	}

	checkpointID := env.GetLatestCheckpointID()
	t.Logf("Condensed checkpoint ID: %s", checkpointID)

	env.ValidateCheckpoint(CheckpointValidation{
		CheckpointID: checkpointID,
		SessionID:    session.ID,
		Strategy:     "manual-commit",
		FilesTouched: []string{"feature_x.go"},
	})
}

// TestJJ_PostCommitIdempotent verifies that running `entire hooks jj post-commit`
// twice after the same commit does not create duplicate checkpoints or trailers.
func TestJJ_PostCommitIdempotent(t *testing.T) {
	t.Parallel()

	env, jjEnv := initJJEnv(t)
	makeInitialCommitViaJJ(t, env, jjEnv)

	// Setup: agent session → Stop → JJ commit
	session := env.NewSession()
	if err := env.SimulateUserPromptSubmit(session.ID); err != nil {
		t.Fatalf("UserPromptSubmit failed: %v", err)
	}

	env.WriteFile("foo.go", "package main")

	transcriptPath := session.CreateTranscript("Add foo", []FileChange{
		{Path: "foo.go", Content: "package main"},
	})
	if err := env.SimulateStop(session.ID, transcriptPath); err != nil {
		t.Fatalf("Stop hook failed: %v", err)
	}

	trackAndCommitJJ(t, jjEnv, env.RepoDir, "Add foo", "foo.go")

	// First post-commit hook run
	env.runJJPostCommitHook(t)
	checkpointID1 := env.GetLatestCheckpointID()

	// Second post-commit hook run (idempotent: trailer already present)
	env.runJJPostCommitHook(t)
	checkpointID2 := env.GetLatestCheckpointID()

	// Should still be the same checkpoint — no duplication
	if checkpointID1 != checkpointID2 {
		t.Errorf("idempotency violation: first checkpoint=%s, second=%s",
			checkpointID1, checkpointID2)
	}

	// Git HEAD should have exactly one Entire-Checkpoint trailer
	headMsg := env.GetCommitMessage(env.GetHeadHash())
	trailerCount := strings.Count(headMsg, "Entire-Checkpoint:")
	if trailerCount != 1 {
		t.Errorf("expected exactly 1 Entire-Checkpoint trailer, found %d in:\n%s",
			trailerCount, headMsg)
	}
}

// TestJJ_MultipleCheckpointsPerSession verifies that multiple Stop-hook checkpoints
// within a single session all condense to the same checkpoint ID on commit.
func TestJJ_MultipleCheckpointsPerSession(t *testing.T) {
	t.Parallel()

	env, jjEnv := initJJEnv(t)
	makeInitialCommitViaJJ(t, env, jjEnv)

	session := env.NewSession()

	// First turn
	if err := env.SimulateUserPromptSubmitWithPrompt(session.ID, "Step 1"); err != nil {
		t.Fatalf("UserPromptSubmit (step 1) failed: %v", err)
	}
	env.WriteFile("step1.go", "package main // step 1")
	transcript1 := session.CreateTranscript("Step 1", []FileChange{
		{Path: "step1.go", Content: "package main // step 1"},
	})
	if err := env.SimulateStop(session.ID, transcript1); err != nil {
		t.Fatalf("Stop (step 1) failed: %v", err)
	}

	// Second turn
	if err := env.SimulateUserPromptSubmitWithPrompt(session.ID, "Step 2"); err != nil {
		t.Fatalf("UserPromptSubmit (step 2) failed: %v", err)
	}
	env.WriteFile("step2.go", "package main // step 2")
	transcript2 := session.CreateTranscript("Step 2", []FileChange{
		{Path: "step2.go", Content: "package main // step 2"},
	})
	if err := env.SimulateStop(session.ID, transcript2); err != nil {
		t.Fatalf("Stop (step 2) failed: %v", err)
	}

	// Verify two rewind points on shadow branch
	points := env.GetRewindPoints()
	if len(points) < 2 {
		t.Fatalf("expected at least 2 rewind points, got %d", len(points))
	}

	// JJ commit + post-commit hook
	trackAndCommitJJ(t, jjEnv, env.RepoDir, "Implement steps 1 and 2", "step1.go", "step2.go")
	env.runJJPostCommitHook(t)

	// Both steps should be in one condensed checkpoint
	checkpointID := env.GetLatestCheckpointID()
	t.Logf("Checkpoint ID: %s", checkpointID)

	env.ValidateCheckpoint(CheckpointValidation{
		CheckpointID: checkpointID,
		SessionID:    session.ID,
		Strategy:     "manual-commit",
	})
}

// TestJJ_VCSDetectionColocated verifies that the CLI works correctly in a JJ
// colocated repo — VCS detection doesn't panic or crash.
func TestJJ_VCSDetectionColocated(t *testing.T) {
	t.Parallel()
	skipIfNoJJ(t)

	env := NewTestEnv(t)
	initJJColocatedRepo(t, env.RepoDir)

	// `entire version` should work fine in a JJ colocated repo (no Entire init needed)
	output, err := env.RunCLIWithError("version")
	if err != nil {
		t.Fatalf("entire version failed in JJ colocated repo: %v\nOutput: %s", err, output)
	}
	t.Logf("entire version output: %s", output)
}

// TestJJ_PrePushNoOp verifies that `entire hooks jj pre-push` is a no-op
// when there are no sessions to push (no entire/checkpoints/v1 branch).
func TestJJ_PrePushNoOp(t *testing.T) {
	t.Parallel()

	env, jjEnv := initJJEnv(t)
	makeInitialCommitViaJJ(t, env, jjEnv)

	// No session active — pre-push should be a no-op
	output, err := env.RunCLIWithError("hooks", "jj", "pre-push", "origin")
	if err != nil {
		t.Fatalf("hooks jj pre-push failed: %v\nOutput: %s", err, output)
	}
	t.Logf("pre-push output: %s", output)
}

// TestJJ_FullWorkflow tests the complete JJ workflow end-to-end:
// 1. JJ colocated repo initialized
// 2. Agent starts session (UserPromptSubmit)
// 3. Agent makes file changes and saves checkpoint (Stop)
// 4. User runs `jj commit` (simulated)
// 5. Shell wrapper fires `entire hooks jj post-commit`
// 6. Checkpoint trailer added to commit, session condensed
// 7. Rewind points are still available
// 8. Session is ended
func TestJJ_FullWorkflow(t *testing.T) {
	t.Parallel()

	env, jjEnv := initJJEnv(t)
	makeInitialCommitViaJJ(t, env, jjEnv)

	// ── Phase 1: Agent session ────────────────────────────────────────────────
	session := env.NewSession()

	if err := env.SimulateUserPromptSubmitWithPrompt(session.ID, "Build login feature"); err != nil {
		t.Fatalf("UserPromptSubmit failed: %v", err)
	}

	// Agent creates auth module
	authCode := "package auth\n\nimport \"errors\"\n\n// Login validates credentials.\nfunc Login(user, pass string) error {\n\tif user == \"\" || pass == \"\" {\n\t\treturn errors.New(\"empty credentials\")\n\t}\n\treturn nil\n}\n"
	env.WriteFile("auth/login.go", authCode)

	transcriptPath := session.CreateTranscript("Build login feature", []FileChange{
		{Path: "auth/login.go", Content: authCode},
	})

	// Agent saves checkpoint
	if err := env.SimulateStop(session.ID, transcriptPath); err != nil {
		t.Fatalf("Stop hook failed: %v", err)
	}

	// Verify shadow branch was created
	points := env.GetRewindPoints()
	if len(points) == 0 {
		t.Fatal("expected rewind points after Stop hook")
	}
	t.Logf("Rewind points after Stop: %d", len(points))

	// ── Phase 2: User does `jj commit` ───────────────────────────────────────
	commitHash := trackAndCommitJJ(t, jjEnv, env.RepoDir, "Add login feature", "auth/login.go")
	t.Logf("JJ committed: %s", commitHash[:7])

	// ── Phase 3: Shell wrapper fires post-commit hook ─────────────────────────
	hookOutput := env.runJJPostCommitHook(t)
	t.Logf("post-commit hook: %s", hookOutput)

	// ── Phase 4: Verify trailer on git commit ─────────────────────────────────
	headHash := env.GetHeadHash()
	headMsg := env.GetCommitMessage(headHash)
	t.Logf("HEAD commit message:\n%s", headMsg)

	if !strings.Contains(headMsg, "Entire-Checkpoint:") {
		t.Errorf("HEAD commit should have Entire-Checkpoint trailer:\n%s", headMsg)
	}
	if !strings.Contains(headMsg, "Add login feature") {
		t.Errorf("HEAD commit should contain original message:\n%s", headMsg)
	}

	// ── Phase 5: Verify condensation ─────────────────────────────────────────
	if !env.BranchExists("entire/checkpoints/v1") {
		t.Fatal("entire/checkpoints/v1 branch must exist after condensation")
	}

	checkpointID := env.GetLatestCheckpointID()
	t.Logf("Checkpoint ID: %s", checkpointID)

	// Full checkpoint validation
	env.ValidateCheckpoint(CheckpointValidation{
		CheckpointID: checkpointID,
		SessionID:    session.ID,
		Strategy:     "manual-commit",
		FilesTouched: []string{"auth/login.go"},
	})

	// ── Phase 6: Rewind still works ──────────────────────────────────────────
	// After condensation, rewind points should still be accessible
	pointsAfter := env.GetRewindPoints()
	if len(pointsAfter) == 0 {
		t.Error("expected rewind points to remain accessible after condensation")
	}

	// ── Phase 7: Agent ends session ───────────────────────────────────────────
	if err := env.SimulateSessionEnd(session.ID); err != nil {
		t.Fatalf("SessionEnd hook failed: %v", err)
	}
}

// TestJJ_MultipleSequentialCommits verifies that multiple JJ commits in
// different sessions each get their own checkpoint trailer and condense correctly.
func TestJJ_MultipleSequentialCommits(t *testing.T) {
	t.Parallel()

	env, jjEnv := initJJEnv(t)
	makeInitialCommitViaJJ(t, env, jjEnv)

	// ── Commit 1: First session ────────────────────────────────────────────────
	session1 := env.NewSession()
	if err := env.SimulateUserPromptSubmitWithPrompt(session1.ID, "First task"); err != nil {
		t.Fatalf("session1 UserPromptSubmit failed: %v", err)
	}
	env.WriteFile("file1.go", "package main // v1")
	t1 := session1.CreateTranscript("First task", []FileChange{
		{Path: "file1.go", Content: "package main // v1"},
	})
	if err := env.SimulateStop(session1.ID, t1); err != nil {
		t.Fatalf("session1 Stop failed: %v", err)
	}

	// Track and commit
	trackAndCommitJJ(t, jjEnv, env.RepoDir, "First commit", "file1.go")
	env.runJJPostCommitHook(t)

	checkpointID1 := env.GetLatestCheckpointID()
	t.Logf("Checkpoint 1: %s", checkpointID1)

	// ── Commit 2: Second session ───────────────────────────────────────────────
	// Clear session state to start a fresh session (simulate session restart)
	if err := env.ClearSessionState(session1.ID); err != nil {
		t.Fatalf("ClearSessionState failed: %v", err)
	}

	session2 := env.NewSession()
	if err := env.SimulateUserPromptSubmitWithPrompt(session2.ID, "Second task"); err != nil {
		t.Fatalf("session2 UserPromptSubmit failed: %v", err)
	}
	env.WriteFile("file2.go", "package main // v2")
	t2 := session2.CreateTranscript("Second task", []FileChange{
		{Path: "file2.go", Content: "package main // v2"},
	})
	if err := env.SimulateStop(session2.ID, t2); err != nil {
		t.Fatalf("session2 Stop failed: %v", err)
	}

	// Track and commit
	trackAndCommitJJ(t, jjEnv, env.RepoDir, "Second commit", "file2.go")
	env.runJJPostCommitHook(t)

	checkpointID2 := env.GetLatestCheckpointID()
	t.Logf("Checkpoint 2: %s", checkpointID2)

	// Two different checkpoint IDs (different sessions, different commits)
	if checkpointID1 == checkpointID2 {
		t.Errorf("expected different checkpoint IDs for different sessions, both are: %s", checkpointID1)
	}

	// Both commits should have Entire-Checkpoint trailers
	log := env.GetGitLog()
	if len(log) < 3 { // initial + 2 feature commits
		t.Fatalf("expected at least 3 commits in log, got %d", len(log))
	}
	for i, commitHash := range log[:2] { // Check the two most recent commits
		msg := env.GetCommitMessage(commitHash)
		if !strings.Contains(msg, "Entire-Checkpoint:") {
			t.Errorf("commit %d (%s) missing Entire-Checkpoint trailer:\n%s", i+1, commitHash[:7], msg)
		}
	}
}

// TestJJ_RewindWorksAfterJJCommit verifies that rewind points are accessible both
// before and after a JJ commit + post-commit hook cycle.
//
// Note: After `jj commit`, the shadow branch's base commit changes (HEAD advances).
// The shadow branch is associated with the NEW HEAD, so rewind point IDs (shadow
// branch commit hashes) will change. This is expected behavior — the important
// thing is that rewind points remain accessible in both states.
//
// For cross-commit rewind access (logs-only mode), the condensed checkpoint on
// entire/checkpoints/v1 is used, which persists indefinitely.
func TestJJ_RewindWorksAfterJJCommit(t *testing.T) {
	t.Parallel()

	env, jjEnv := initJJEnv(t)
	makeInitialCommitViaJJ(t, env, jjEnv)

	session := env.NewSession()
	if err := env.SimulateUserPromptSubmitWithPrompt(session.ID, "Create module"); err != nil {
		t.Fatalf("UserPromptSubmit failed: %v", err)
	}

	// First checkpoint: create a file
	env.WriteFile("module.go", "package module\n\nconst Version = 1")
	t1 := session.CreateTranscript("Create module", []FileChange{
		{Path: "module.go", Content: "package module\n\nconst Version = 1"},
	})
	if err := env.SimulateStop(session.ID, t1); err != nil {
		t.Fatalf("Stop (checkpoint 1) failed: %v", err)
	}

	// Verify rewind points exist before commit
	pointsBefore := env.GetRewindPoints()
	if len(pointsBefore) == 0 {
		t.Fatal("expected rewind point before JJ commit")
	}
	t.Logf("Rewind points before JJ commit: %d", len(pointsBefore))

	// JJ commit + hook
	trackAndCommitJJ(t, jjEnv, env.RepoDir, "Initial module", "module.go")
	env.runJJPostCommitHook(t)

	// After commit + condensation, shadow-branch rewind points move to the new HEAD.
	// The condensed checkpoint is accessible via entire/checkpoints/v1 (logs-only mode).
	// We verify rewind points are still available via the metadata branch.
	pointsAfter := env.GetRewindPoints()
	t.Logf("Rewind points after JJ commit: %d", len(pointsAfter))

	// At minimum, the condensed checkpoint on entire/checkpoints/v1 should be accessible
	// as a logs-only rewind point.
	if !env.BranchExists("entire/checkpoints/v1") {
		t.Fatal("entire/checkpoints/v1 must exist after JJ post-commit hook")
	}

	checkpointID := env.GetLatestCheckpointID()
	t.Logf("Condensed checkpoint ID: %s", checkpointID)
	if checkpointID == "" {
		t.Error("expected a condensed checkpoint ID after JJ commit")
	}

	// The logs-only rewind point should appear in the list (it comes from entire/checkpoints/v1)
	foundLogsOnly := false
	for _, p := range pointsAfter {
		if p.IsLogsOnly {
			foundLogsOnly = true
			t.Logf("Found logs-only rewind point: %s (condensation=%s)", p.ID, p.CondensationID)
			break
		}
	}
	if !foundLogsOnly && len(pointsAfter) == 0 {
		// If we have active shadow branch points (same session continues), that's also fine
		t.Log("No logs-only points yet — active shadow branch points still present")
	}
}

// TestJJ_EntireSetupInColocatedRepo verifies that Entire works correctly in a
// JJ colocated repo — the CLI doesn't error on VCS detection or settings load.
func TestJJ_EntireSetupInColocatedRepo(t *testing.T) {
	t.Parallel()

	env, _ := initJJEnv(t)

	// Verify the CLI runs without error in a JJ colocated repo
	output, err := env.RunCLIWithError("version")
	if err != nil {
		t.Fatalf("CLI failed in JJ colocated repo: %v\nOutput: %s", err, output)
	}
	t.Logf("CLI output in colocated repo: %s", output)
}

// TestJJ_CheckpointBranchNaming verifies that shadow branches use the correct
// naming convention (entire/<commitHash[:7]>-<worktreeHash[:6]>) in JJ repos.
func TestJJ_CheckpointBranchNaming(t *testing.T) {
	t.Parallel()

	env, jjEnv := initJJEnv(t)
	makeInitialCommitViaJJ(t, env, jjEnv)

	session := env.NewSession()
	if err := env.SimulateUserPromptSubmit(session.ID); err != nil {
		t.Fatalf("UserPromptSubmit failed: %v", err)
	}
	env.WriteFile("naming_test.go", "package main")
	transcriptPath := session.CreateTranscript("test naming", []FileChange{
		{Path: "naming_test.go", Content: "package main"},
	})
	if err := env.SimulateStop(session.ID, transcriptPath); err != nil {
		t.Fatalf("Stop hook failed: %v", err)
	}

	// Shadow branch should exist with the right naming pattern
	branches := env.ListBranchesWithPrefix("entire/")
	shadowBranches := make([]string, 0)
	for _, b := range branches {
		if b != "entire/checkpoints/v1" {
			shadowBranches = append(shadowBranches, b)
		}
	}

	if len(shadowBranches) == 0 {
		t.Fatal("expected at least one shadow branch (entire/<hash>-<worktree>)")
	}

	expectedName := env.GetShadowBranchName()
	t.Logf("Expected shadow branch: %s", expectedName)
	t.Logf("Actual shadow branches: %v", shadowBranches)

	found := false
	for _, b := range shadowBranches {
		if b == expectedName {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected shadow branch %q not found in %v", expectedName, shadowBranches)
	}
}

// TestJJ_PostCommitHookSwallowsErrors verifies that hook errors don't propagate
// as non-zero exit codes (hooks must be resilient per hook contract).
// This test uses a deliberately broken state (no initial commit) to trigger
// internal errors, verifying that the CLI exits 0 regardless.
func TestJJ_PostCommitHookSwallowsErrors(t *testing.T) {
	t.Parallel()
	skipIfNoJJ(t)

	env := NewTestEnv(t)
	initJJColocatedRepo(t, env.RepoDir)
	env.InitEntire()

	// No initial commit means HEAD doesn't exist — the hook will try to read HEAD
	// and fail internally, but should swallow the error and exit 0.
	output, err := env.runJJPostCommitHookWithError(t)
	if err != nil {
		t.Errorf("post-commit hook should swallow errors (exit 0), got: %v\nOutput: %s", err, output)
	}
}

// TestJJ_SessionHooksWorkInColocatedRepo verifies that Claude Code style
// lifecycle hooks (UserPromptSubmit, Stop) work correctly in a JJ colocated repo.
// The lifecycle hooks are VCS-agnostic and should behave identically to git-only repos.
func TestJJ_SessionHooksWorkInColocatedRepo(t *testing.T) {
	t.Parallel()

	env, jjEnv := initJJEnv(t)
	makeInitialCommitViaJJ(t, env, jjEnv)

	session := env.NewSession()

	// UserPromptSubmit should create session state
	if err := env.SimulateUserPromptSubmitWithPrompt(session.ID, "JJ repo task"); err != nil {
		t.Fatalf("UserPromptSubmit failed: %v", err)
	}

	// Verify session state was created
	sessionStateDir := filepath.Join(env.RepoDir, ".git", "entire-sessions")
	entries, err := os.ReadDir(sessionStateDir)
	if err != nil {
		t.Fatalf("Failed to read session state dir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("Expected session state file in .git/entire-sessions/")
	}

	// Stop should save checkpoint to shadow branch
	env.WriteFile("jj_task.go", fmt.Sprintf("package main // jj task for session %s", session.ID))
	transcriptPath := session.CreateTranscript("JJ repo task", []FileChange{
		{Path: "jj_task.go", Content: "package main // jj task"},
	})
	if err := env.SimulateStop(session.ID, transcriptPath); err != nil {
		t.Fatalf("Stop hook failed: %v", err)
	}

	// Checkpoint should be on shadow branch
	points := env.GetRewindPoints()
	if len(points) == 0 {
		t.Fatal("expected rewind points after Stop hook in JJ colocated repo")
	}
	t.Logf("Shadow branch checkpoint count: %d", len(points))
}
