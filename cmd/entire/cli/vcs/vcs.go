// Package vcs provides version control system detection for the Entire CLI.
// It identifies whether the current repository uses Git only, JJ colocated
// (both .jj/ and .git/), JJ only, or is unknown.
package vcs

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/entireio/cli/cmd/entire/cli/paths"
)

// Type represents the type of version control system in use.
type Type string

const (
	// GitOnly indicates a repository with only Git (.git/ present, no .jj/).
	GitOnly Type = "git"

	// JJColocated indicates a repository with both JJ and Git (.jj/ and .git/ present).
	JJColocated Type = "jj-colocated"

	// JJOnly indicates a repository with only JJ (.jj/ present, no .git/).
	JJOnly Type = "jj-only"

	// Unknown indicates no recognized VCS was detected.
	Unknown Type = "unknown"
)

// String returns a human-readable description of the VCS type.
func (v Type) String() string {
	switch v {
	case GitOnly:
		return "Git"
	case JJColocated:
		return "Git + JJ (colocated)"
	case JJOnly:
		return "JJ (no Git)"
	case Unknown:
		return "Unknown"
	default:
		return "Unknown"
	}
}

// Detect identifies the VCS type for the current repository.
// It checks for the presence of .jj/ and .git/ (directory or file) at the
// repository root. If the git worktree root cannot be determined (e.g., no
// .git/ exists), it falls back to checking the current working directory.
func Detect(ctx context.Context) Type {
	root, err := paths.WorktreeRoot(ctx)
	if err != nil {
		// WorktreeRoot failed — no git repo. Fall back to cwd for JJ-only detection.
		//nolint:forbidigo // Intentional: need cwd as fallback when no git repo exists (JJ-only detection)
		root, err = os.Getwd()
		if err != nil {
			return Unknown
		}
	}

	hasGit := exists(filepath.Join(root, ".git"))
	hasJJ := exists(filepath.Join(root, ".jj"))

	switch {
	case hasGit && hasJJ:
		return JJColocated
	case hasGit:
		return GitOnly
	case hasJJ:
		return JJOnly
	default:
		return Unknown
	}
}

// IsColocated returns true if the current repository uses JJ in colocated mode
// (both .jj/ and .git/ are present).
func IsColocated(ctx context.Context) bool {
	return Detect(ctx) == JJColocated
}

// RequireColocated returns an error if the repository is not in JJ colocated mode.
// Use this to guard operations that require both JJ and Git to be present.
func RequireColocated(ctx context.Context) error {
	if !IsColocated(ctx) {
		return errors.New("JJ-only mode is not supported. Please use colocated mode (jj git init --colocate)")
	}
	return nil
}

// HasJJ returns true if a .jj/ directory exists at the repository root,
// regardless of whether Git is also present (colocated or JJ-only).
func HasJJ(ctx context.Context) bool {
	vcsType := Detect(ctx)
	return vcsType == JJColocated || vcsType == JJOnly
}

// exists reports whether the given path exists (file or directory).
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
