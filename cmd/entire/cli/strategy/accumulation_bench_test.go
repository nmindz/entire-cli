package strategy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/entireio/cli/cmd/entire/cli/checkpoint/id"
	"github.com/entireio/cli/cmd/entire/cli/paths"
	"github.com/entireio/cli/cmd/entire/cli/session"
	"github.com/entireio/cli/cmd/entire/cli/trailers"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// BenchmarkPostCommit_EndedSessionAccumulation measures the PostCommit overhead
// caused by accumulated ENDED sessions whose files don't overlap with the committed
// files. This is the exact scenario from GitHub issue #591.
//
// On main (before fix): each ENDED session adds ~73-103ms per commit, forever.
// After fix: first commit force-condenses all, subsequent commits skip them.
func BenchmarkPostCommit_EndedSessionAccumulation(b *testing.B) {
	for _, count := range []int{10, 50, 100, 200} {
		b.Run(fmt.Sprintf("EndedSessions_%d", count), benchEndedSessionAccumulation(count, repoProfileSmall))
	}
}

// BenchmarkPostCommit_RepoProfiles measures PostCommit with 50 accumulated ENDED
// sessions across different repository profiles.
func BenchmarkPostCommit_RepoProfiles(b *testing.B) {
	for _, profile := range []struct {
		name string
		fn   repoProfile
	}{
		{"SmallRepo_100files", repoProfileSmall},
		{"LargeFiles_50x20MB", repoProfileLargeFiles},
		// 500k files takes too long to set up for benchmarks; use 1000 as proxy
		{"ManyFiles_1000", repoProfileManyFiles},
	} {
		b.Run(profile.name, benchEndedSessionAccumulation(50, profile.fn))
	}
}

// BenchmarkPostCommit_SecondCommitAfterFix measures the key metric: how fast is
// the SECOND commit after force-condensation cleaned up all ENDED sessions?
// On main: same as first commit (sessions persist). After fix: near-zero overhead.
func BenchmarkPostCommit_SecondCommitAfterFix(b *testing.B) {
	for _, count := range []int{50, 100, 200} {
		b.Run(fmt.Sprintf("SecondCommit_%dEndedSessions", count), func(b *testing.B) {
			for range b.N {
				b.StopTimer()
				dir := setupAccumulationRepo(b, count, repoProfileSmall)
				b.Chdir(dir)
				paths.ClearWorktreeRootCache()

				// First PostCommit: processes all ENDED sessions
				s := &ManualCommitStrategy{}
				if err := s.PostCommit(context.Background()); err != nil {
					b.Fatalf("PostCommit 1: %v", err)
				}

				// Create another commit for the second PostCommit
				repo, err := git.PlainOpen(dir)
				if err != nil {
					b.Fatalf("open: %v", err)
				}
				wt, err := repo.Worktree()
				if err != nil {
					b.Fatalf("worktree: %v", err)
				}
				newFile := filepath.Join(dir, "second_commit.txt")
				if err := os.WriteFile(newFile, []byte("second commit content"), 0o644); err != nil {
					b.Fatalf("write: %v", err)
				}
				if _, err := wt.Add("second_commit.txt"); err != nil {
					b.Fatalf("add: %v", err)
				}
				cpID, err := id.Generate()
				if err != nil {
					b.Fatalf("generate ID: %v", err)
				}
				commitMsg := fmt.Sprintf("second commit\n\n%s: %s\n", trailers.CheckpointTrailerKey, cpID)
				if _, err := wt.Commit(commitMsg, &git.CommitOptions{
					Author: &object.Signature{Name: "Bench", Email: "bench@test.com", When: time.Now()},
				}); err != nil {
					b.Fatalf("commit: %v", err)
				}
				paths.ClearWorktreeRootCache()

				b.StartTimer()

				// Second PostCommit: should be fast (all ENDED sessions FullyCondensed)
				s2 := &ManualCommitStrategy{}
				if err := s2.PostCommit(context.Background()); err != nil {
					b.Fatalf("PostCommit 2: %v", err)
				}
			}
		})
	}
}

type repoProfile func(b *testing.B, dir string, wt *git.Worktree)

// repoProfileSmall creates 100 small files (~1KB each).
func repoProfileSmall(b *testing.B, dir string, wt *git.Worktree) {
	b.Helper()
	for i := range 100 {
		name := fmt.Sprintf("src/file_%d.go", i)
		abs := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			b.Fatalf("mkdir: %v", err)
		}
		content := fmt.Sprintf("package main\n\nfunc f%d() {\n\treturn\n}\n", i)
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			b.Fatalf("write: %v", err)
		}
		if _, err := wt.Add(name); err != nil {
			b.Fatalf("add: %v", err)
		}
	}
}

// repoProfileLargeFiles creates 50 files at ~20MB each.
func repoProfileLargeFiles(b *testing.B, dir string, wt *git.Worktree) {
	b.Helper()
	// 20MB of repeated content
	chunk := make([]byte, 20*1024*1024)
	for i := range chunk {
		chunk[i] = byte('A' + (i % 26))
	}
	for i := range 50 {
		name := fmt.Sprintf("data/large_%d.bin", i)
		abs := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			b.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(abs, chunk, 0o644); err != nil {
			b.Fatalf("write: %v", err)
		}
		if _, err := wt.Add(name); err != nil {
			b.Fatalf("add: %v", err)
		}
	}
}

// repoProfileManyFiles creates 1000 tiny files (~100 bytes each).
// (Proxy for 500k files — full 500k is too slow for benchmarks.)
func repoProfileManyFiles(b *testing.B, dir string, wt *git.Worktree) {
	b.Helper()
	for i := range 1000 {
		subdir := fmt.Sprintf("pkg/d%d", i/100)
		name := fmt.Sprintf("%s/f%d.go", subdir, i)
		abs := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			b.Fatalf("mkdir: %v", err)
		}
		content := fmt.Sprintf("package d%d\nvar x%d = %d\n", i/100, i, i)
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			b.Fatalf("write: %v", err)
		}
		if _, err := wt.Add(name); err != nil {
			b.Fatalf("add: %v", err)
		}
	}
}

func benchEndedSessionAccumulation(sessionCount int, profile repoProfile) func(*testing.B) {
	return func(b *testing.B) {
		for range b.N {
			b.StopTimer()
			dir := setupAccumulationRepo(b, sessionCount, profile)
			b.Chdir(dir)
			paths.ClearWorktreeRootCache()
			b.StartTimer()

			s := &ManualCommitStrategy{}
			if err := s.PostCommit(context.Background()); err != nil {
				b.Fatalf("PostCommit: %v", err)
			}
		}
	}
}

// setupAccumulationRepo creates a repo with the given profile, then creates N
// ENDED sessions whose files DON'T overlap with the final committed file.
// This is the exact accumulation scenario from issue #591.
func setupAccumulationRepo(b *testing.B, sessionCount int, profile repoProfile) string {
	b.Helper()

	dir := b.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}

	repo, err := git.PlainInit(dir, false)
	if err != nil {
		b.Fatalf("git init: %v", err)
	}

	cfg, err := repo.Config()
	if err != nil {
		b.Fatalf("config: %v", err)
	}
	cfg.User.Name = "Bench User"
	cfg.User.Email = "bench@example.com"
	if err := repo.SetConfig(cfg); err != nil {
		b.Fatalf("set config: %v", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		b.Fatalf("worktree: %v", err)
	}

	// Apply repo profile (creates files and adds them)
	profile(b, dir, wt)

	if _, err := wt.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{Name: "Bench", Email: "bench@test.com", When: time.Now()},
	}); err != nil {
		b.Fatalf("commit: %v", err)
	}

	s := &ManualCommitStrategy{}
	b.Chdir(dir)
	paths.ClearWorktreeRootCache()

	// Create N ENDED sessions, each touching a UNIQUE file that won't be in the
	// final commit. This simulates sessions that ended without their files being committed.
	for i := range sessionCount {
		sessionID := fmt.Sprintf("ended-session-%d", i)
		agentFile := fmt.Sprintf("agent_work_%d.txt", i)
		agentFileAbs := filepath.Join(dir, agentFile)

		// Create the file the agent "modified"
		if err := os.WriteFile(agentFileAbs, []byte(fmt.Sprintf("agent work %d", i)), 0o644); err != nil {
			b.Fatalf("write: %v", err)
		}

		// Create metadata with transcript
		metadataDir := ".entire/metadata/" + sessionID
		metadataDirAbs := filepath.Join(dir, metadataDir)
		if err := os.MkdirAll(metadataDirAbs, 0o755); err != nil {
			b.Fatalf("mkdir: %v", err)
		}
		transcript := fmt.Sprintf(`{"type":"human","message":{"content":"do task %d"}}
{"type":"assistant","message":{"content":"Done with task %d."}}
`, i, i)
		if err := os.WriteFile(filepath.Join(metadataDirAbs, paths.TranscriptFileName), []byte(transcript), 0o644); err != nil {
			b.Fatalf("write transcript: %v", err)
		}

		paths.ClearWorktreeRootCache()

		if err := s.SaveStep(context.Background(), StepContext{
			SessionID:      sessionID,
			ModifiedFiles:  []string{},
			NewFiles:       []string{agentFile},
			DeletedFiles:   []string{},
			MetadataDir:    metadataDir,
			MetadataDirAbs: metadataDirAbs,
			CommitMessage:  fmt.Sprintf("Checkpoint: task %d", i),
			AuthorName:     "Agent",
			AuthorEmail:    "agent@test.com",
		}); err != nil {
			b.Fatalf("SaveStep: %v", err)
		}

		// Set session to ENDED with FilesTouched = the agent's file (NOT in final commit)
		state, err := s.loadSessionState(context.Background(), sessionID)
		if err != nil {
			b.Fatalf("load state: %v", err)
		}
		state.Phase = session.PhaseEnded
		now := time.Now()
		state.EndedAt = &now
		state.FilesTouched = []string{agentFile}
		state.CheckpointTranscriptStart = 0 // so sessionHasNewContent returns true
		if err := s.saveSessionState(context.Background(), state); err != nil {
			b.Fatalf("save state: %v", err)
		}

		// Clean up the agent file from worktree (user discarded changes)
		os.Remove(agentFileAbs)
	}

	// Create the final commit that PostCommit will process.
	// This commit touches an UNRELATED file — no overlap with any session's files.
	unrelatedFile := filepath.Join(dir, "user_commit.txt")
	if err := os.WriteFile(unrelatedFile, []byte("user's own work"), 0o644); err != nil {
		b.Fatalf("write: %v", err)
	}
	if _, err := wt.Add("user_commit.txt"); err != nil {
		b.Fatalf("add: %v", err)
	}

	cpID, err := id.Generate()
	if err != nil {
		b.Fatalf("generate ID: %v", err)
	}
	commitMsg := fmt.Sprintf("user commit\n\n%s: %s\n", trailers.CheckpointTrailerKey, cpID)
	if _, err := wt.Commit(commitMsg, &git.CommitOptions{
		Author: &object.Signature{Name: "User", Email: "user@test.com", When: time.Now()},
	}); err != nil {
		b.Fatalf("commit: %v", err)
	}

	return dir
}
