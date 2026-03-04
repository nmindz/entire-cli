//go:build integration

package integration

import (
	"testing"

	"github.com/entireio/cli/cmd/entire/cli/session"
)

// TestCarryForward_NewSessionCommitDoesNotCondenseOldSession verifies that when
// an old ENDED session has carry-forward files and a NEW session commits unrelated
// files, the old session IS force-condensed to prevent unbounded accumulation
// (GitHub issue #591).
//
// Without force-condensation, ENDED sessions that fail the file overlap check
// persist forever, re-processed on every future commit at ~73-103ms each.
//
// This integration test complements the unit tests in phase_postcommit_test.go by
// testing the full hook invocation path with multiple sessions interacting.
//
// Scenario:
// 1. Session 1 creates file1.txt and file2.txt
// 2. User commits only file1.txt (partial commit)
// 3. Session 1 gets carry-forward: FilesTouched = ["file2.txt"]
// 4. Session 1 ends
// 5. Make some unrelated commits (simulating time passing)
// 6. New session 2 creates and commits file6.txt
// 7. Verify: Session 1 WAS force-condensed and marked FullyCondensed
// 8. Verify: Session 2 WAS condensed normally
func TestCarryForward_NewSessionCommitDoesNotCondenseOldSession(t *testing.T) {
	t.Parallel()
	env := NewTestEnv(t)
	defer env.Cleanup()

	// ========================================
	// Setup
	// ========================================
	env.InitRepo()
	env.WriteFile("README.md", "# Test Repository")
	env.GitAdd("README.md")
	env.GitCommit("Initial commit")
	env.GitCheckoutNewBranch("feature/multi-session-carry-forward")
	env.InitEntire()

	// ========================================
	// Phase 1: Session 1 creates files, partial commit, ends with carry-forward
	// ========================================
	t.Log("Phase 1: Session 1 creates file1.txt and file2.txt")

	session1 := env.NewSession()
	if err := env.SimulateUserPromptSubmit(session1.ID); err != nil {
		t.Fatalf("SimulateUserPromptSubmit for session1 failed: %v", err)
	}

	env.WriteFile("file1.txt", "content from session 1 - file 1")
	env.WriteFile("file2.txt", "content from session 1 - file 2")
	session1.CreateTranscript(
		"Create file1 and file2",
		[]FileChange{
			{Path: "file1.txt", Content: "content from session 1 - file 1"},
			{Path: "file2.txt", Content: "content from session 1 - file 2"},
		},
	)
	if err := env.SimulateStop(session1.ID, session1.TranscriptPath); err != nil {
		t.Fatalf("SimulateStop for session1 failed: %v", err)
	}

	// Partial commit - only file1.txt
	t.Log("Phase 1b: Partial commit - only file1.txt")
	env.GitAdd("file1.txt")
	env.GitCommitWithShadowHooks("Partial commit: only file1", "file1.txt")

	// End session 1
	state1, err := env.GetSessionState(session1.ID)
	if err != nil {
		t.Fatalf("GetSessionState for session1 failed: %v", err)
	}
	state1.Phase = session.PhaseEnded
	if err := env.WriteSessionState(session1.ID, state1); err != nil {
		t.Fatalf("WriteSessionState for session1 failed: %v", err)
	}

	// Verify carry-forward
	state1, err = env.GetSessionState(session1.ID)
	if err != nil {
		t.Fatalf("GetSessionState for session1 failed: %v", err)
	}
	t.Logf("Session1 (ENDED) FilesTouched: %v", state1.FilesTouched)

	// ========================================
	// Phase 2: Make some unrelated commits (simulating time passing)
	// ========================================
	t.Log("Phase 2: Making unrelated commits")

	for _, fileName := range []string{"file3.txt", "file4.txt"} {
		env.WriteFile(fileName, "unrelated content")
		env.GitAdd(fileName)
		env.GitCommitWithShadowHooks("Add "+fileName, fileName)
	}

	// ========================================
	// Phase 3: NEW session 2 starts and creates file6.txt
	// ========================================
	t.Log("Phase 3: Session 2 starts and creates file6.txt")

	session2 := env.NewSession()
	if err := env.SimulateUserPromptSubmit(session2.ID); err != nil {
		t.Fatalf("SimulateUserPromptSubmit for session2 failed: %v", err)
	}

	env.WriteFile("file6.txt", "content from session 2")
	session2.CreateTranscript(
		"Create file6",
		[]FileChange{{Path: "file6.txt", Content: "content from session 2"}},
	)
	if err := env.SimulateStop(session2.ID, session2.TranscriptPath); err != nil {
		t.Fatalf("SimulateStop for session2 failed: %v", err)
	}

	// Set session2 to ACTIVE
	state2, err := env.GetSessionState(session2.ID)
	if err != nil {
		t.Fatalf("GetSessionState for session2 failed: %v", err)
	}
	state2.Phase = session.PhaseActive
	if err := env.WriteSessionState(session2.ID, state2); err != nil {
		t.Fatalf("WriteSessionState for session2 failed: %v", err)
	}

	// ========================================
	// Phase 4: Commit file6.txt (session 2's file)
	// ========================================
	t.Log("Phase 4: Committing file6.txt from session 2")

	env.GitAdd("file6.txt")
	env.GitCommitWithShadowHooks("Add file6 from session 2", "file6.txt")

	finalHead := env.GetHeadHash()

	// ========================================
	// Phase 5: Verify session 1 WAS force-condensed (no overlap, but ENDED)
	// ========================================
	t.Log("Phase 5: Verifying session 1 (ENDED, no overlap) was force-condensed")

	state1After, err := env.GetSessionState(session1.ID)
	if err != nil {
		t.Fatalf("GetSessionState for session1 after session2 commit failed: %v", err)
	}

	// StepCount should be reset to 0 (force-condensation happened)
	if state1After.StepCount != 0 {
		t.Errorf("Session 1 StepCount should be 0 after force-condensation, got %d", state1After.StepCount)
	}

	// FilesTouched should be empty (no carry-forward for force-condensed sessions)
	if len(state1After.FilesTouched) != 0 {
		t.Errorf("Session 1 FilesTouched should be empty after force-condensation, got: %v", state1After.FilesTouched)
	}

	// FullyCondensed should be true
	if !state1After.FullyCondensed {
		t.Errorf("Session 1 should be marked FullyCondensed after force-condensation")
	}

	t.Logf("Session 1 correctly force-condensed: StepCount=%d, FilesTouched=%v, FullyCondensed=%v",
		state1After.StepCount, state1After.FilesTouched, state1After.FullyCondensed)

	// ========================================
	// Phase 6: Verify session 2 WAS condensed
	// ========================================
	t.Log("Phase 6: Verifying session 2 WAS condensed")

	state2After, err := env.GetSessionState(session2.ID)
	if err != nil {
		t.Fatalf("GetSessionState for session2 after commit failed: %v", err)
	}

	if state2After.BaseCommit != finalHead {
		t.Errorf("Session 2 BaseCommit should be updated. Expected %s, got %s",
			finalHead[:7], state2After.BaseCommit[:7])
	}

	t.Log("Test completed successfully:")
	t.Log("  - Session 1 force-condensed into session 2's commit (ENDED, no overlap)")
	t.Log("  - Session 2 condensed normally")
}
