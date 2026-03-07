package jj

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// defaultTimeout is the maximum time to wait for a jj command to complete.
const defaultTimeout = 30 * time.Second

// IsAvailable reports whether the jj binary is on the system PATH.
func IsAvailable() bool {
	_, err := exec.LookPath("jj")
	return err == nil
}

// RunJJ executes the jj binary with the given arguments in the specified directory.
// It returns the trimmed stdout, stderr, and any error.
// A 30-second timeout is applied via context if the parent context has no earlier deadline.
func RunJJ(ctx context.Context, dir string, args ...string) (stdout string, stderr string, err error) {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "jj", args...)
	cmd.Dir = dir

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return strings.TrimSpace(outBuf.String()),
			strings.TrimSpace(errBuf.String()),
			fmt.Errorf("jj %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errBuf.String()))
	}

	return strings.TrimSpace(outBuf.String()), strings.TrimSpace(errBuf.String()), nil
}

// GetCurrentCommitID returns the full commit ID (40 hex chars) for the working copy (@).
func GetCurrentCommitID(ctx context.Context, dir string) (string, error) {
	stdout, _, err := RunJJ(ctx, dir,
		"log", "--no-graph", "-T", "commit_id.short(40)", "-r", "@", "--limit", "1",
	)
	if err != nil {
		return "", fmt.Errorf("get current commit ID: %w", err)
	}

	if stdout == "" {
		return "", errors.New("get current commit ID: empty output from jj log")
	}

	return stdout, nil
}

// GetCurrentChangeID returns the short change ID (16 hex chars) for the working copy (@).
func GetCurrentChangeID(ctx context.Context, dir string) (string, error) {
	stdout, _, err := RunJJ(ctx, dir,
		"log", "--no-graph", "-T", "change_id.short(16)", "-r", "@", "--limit", "1",
	)
	if err != nil {
		return "", fmt.Errorf("get current change ID: %w", err)
	}

	if stdout == "" {
		return "", errors.New("get current change ID: empty output from jj log")
	}

	return stdout, nil
}

// DescribeCommit sets the description of a commit identified by the given revset.
func DescribeCommit(ctx context.Context, dir string, revset string, message string) error {
	_, _, err := RunJJ(ctx, dir, "describe", revset, "-m", message)
	if err != nil {
		return fmt.Errorf("describe commit %s: %w", revset, err)
	}

	return nil
}

// AppendToDescription reads the current description of the commit identified by revset,
// appends the trailer (if not already present), and writes the updated description back.
// This is used to add Entire-Checkpoint trailers to JJ commits.
func AppendToDescription(ctx context.Context, dir string, revset string, trailer string) error {
	// Read the current description
	currentDesc, _, err := RunJJ(ctx, dir,
		"log", "--no-graph", "-T", "description", "-r", revset,
	)
	if err != nil {
		return fmt.Errorf("read description for %s: %w", revset, err)
	}

	// Check if trailer is already present
	if strings.Contains(currentDesc, trailer) {
		return nil
	}

	// Append trailer with blank line separator
	newDesc := strings.TrimRight(currentDesc, "\n") + "\n\n" + trailer

	if err := DescribeCommit(ctx, dir, revset, newDesc); err != nil {
		return fmt.Errorf("append to description for %s: %w", revset, err)
	}

	return nil
}
