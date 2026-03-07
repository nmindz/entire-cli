package jj

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// fieldSeparator is the null byte used to separate fields in jj op log template output.
// Using \x00 avoids ambiguity with operation descriptions that may contain spaces,
// commas, or other printable characters.
const fieldSeparator = "\x00"

// opLogTemplate is the jj template for structured op log output.
// Fields: id, description, start time, user, tags — separated by null bytes.
const opLogTemplate = `self.id().short(16) ++ "\x00" ++ description ++ "\x00" ++ self.time().start() ++ "\x00" ++ self.user() ++ "\x00" ++ self.tags()`

// opIDTemplate is a lightweight template that returns only the operation ID.
const opIDTemplate = `self.id().short(16)`

// expectedFieldCount is the number of fields expected in each parsed op log line.
const expectedFieldCount = 5

// knownTimeFormats are the time formats JJ may use for self.time().start().
// JJ's time formatting can vary by version, so we try multiple formats.
var knownTimeFormats = []string{
	"2006-01-02 15:04:05.000 -07:00",
	"2006-01-02 15:04:05.000 -0700",
	"2006-01-02 15:04:05 -07:00",
	"2006-01-02 15:04:05 -0700",
	"2006-01-02T15:04:05.000-07:00",
	"2006-01-02T15:04:05-07:00",
	"2006-01-02T15:04:05Z",
	time.RFC3339,
	time.RFC3339Nano,
}

// GetLatestOperation runs `jj op log` and returns the most recent operation.
func GetLatestOperation(ctx context.Context, dir string) (*Operation, error) {
	stdout, _, err := RunJJ(ctx, dir,
		"op", "log", "--no-graph", "--limit", "1", "-T", opLogTemplate,
	)
	if err != nil {
		return nil, fmt.Errorf("get latest operation: %w", err)
	}

	if stdout == "" {
		return nil, errors.New("get latest operation: empty output from jj op log")
	}

	// Take only the first line in case of trailing newlines.
	line := strings.SplitN(stdout, "\n", 2)[0]

	op, err := parseOpLogLine(line)
	if err != nil {
		return nil, fmt.Errorf("get latest operation: %w", err)
	}

	return op, nil
}

// GetOperationsSince returns all operations newer than sinceOpID, in chronological
// order (oldest first). It fetches up to 50 recent operations and stops when it
// encounters sinceOpID. The operation matching sinceOpID is not included in the result.
func GetOperationsSince(ctx context.Context, dir string, sinceOpID string) ([]Operation, error) {
	stdout, _, err := RunJJ(ctx, dir,
		"op", "log", "--no-graph", "--limit", "50", "-T", opLogTemplate,
	)
	if err != nil {
		return nil, fmt.Errorf("get operations since %s: %w", sinceOpID, err)
	}

	if stdout == "" {
		return nil, nil
	}

	lines := strings.Split(stdout, "\n")
	// ops will be collected in reverse-chronological order, then reversed.
	var ops []Operation

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		op, err := parseOpLogLine(line)
		if err != nil {
			return nil, fmt.Errorf("get operations since %s: %w", sinceOpID, err)
		}

		// Stop when we reach the sentinel operation.
		if op.ID == sinceOpID {
			break
		}

		ops = append(ops, *op)
	}

	// Reverse to chronological order (oldest first).
	reverseOperations(ops)

	return ops, nil
}

// GetLatestOperationID returns only the operation ID of the most recent operation.
// This is a lightweight alternative to GetLatestOperation when only the ID is needed.
func GetLatestOperationID(ctx context.Context, dir string) (string, error) {
	stdout, _, err := RunJJ(ctx, dir,
		"op", "log", "--no-graph", "--limit", "1", "-T", opIDTemplate,
	)
	if err != nil {
		return "", fmt.Errorf("get latest operation ID: %w", err)
	}

	id := strings.TrimSpace(stdout)
	if id == "" {
		return "", errors.New("get latest operation ID: empty output from jj op log")
	}

	return id, nil
}

// ClassifyOperation maps a JJ operation description string to an OperationType.
// The description is matched case-insensitively using substring matching,
// since JJ's description wording may vary slightly across versions.
func ClassifyOperation(description string) OperationType {
	lower := strings.ToLower(description)

	// Check most specific patterns first to avoid false matches.

	// Git push: contains "push" AND ("bookmark" OR "branch" OR "git remote").
	if strings.Contains(lower, "push") &&
		(strings.Contains(lower, "bookmark") || strings.Contains(lower, "branch") || strings.Contains(lower, "git remote")) {
		return OpGitPush
	}

	// Git fetch: contains "fetch" AND "git".
	if strings.Contains(lower, "fetch") && strings.Contains(lower, "git") {
		return OpGitFetch
	}

	// Snapshot working copy.
	if strings.Contains(lower, "snapshot working copy") {
		return OpSnapshot
	}

	// New empty commit (or just "new ..." prefix).
	if strings.Contains(lower, "new empty commit") || strings.HasPrefix(lower, "new ") {
		return OpNew
	}

	// Describe commit.
	if strings.Contains(lower, "describe commit") || strings.HasPrefix(lower, "describe ") {
		return OpDescribe
	}

	// Commit working copy.
	if strings.Contains(lower, "commit working copy") {
		return OpCommit
	}

	// Squash.
	if strings.Contains(lower, "squash") {
		return OpSquash
	}

	// Amend.
	if strings.Contains(lower, "amend") {
		return OpAmend
	}

	// Rebase.
	if strings.Contains(lower, "rebase") {
		return OpRebase
	}

	// Abandon.
	if strings.Contains(lower, "abandon") {
		return OpAbandon
	}

	// Edit commit.
	if strings.Contains(lower, "edit commit") || strings.HasPrefix(lower, "edit ") {
		return OpEdit
	}

	return OpOther
}

// parseOpLogLine parses a single null-byte-separated line from jj op log output
// into an Operation struct.
func parseOpLogLine(line string) (*Operation, error) {
	fields := strings.SplitN(line, fieldSeparator, expectedFieldCount)
	if len(fields) != expectedFieldCount {
		return nil, fmt.Errorf("parse op log line: expected %d fields, got %d in %q", expectedFieldCount, len(fields), line)
	}

	id := strings.TrimSpace(fields[0])
	description := strings.TrimSpace(fields[1])
	timeStr := strings.TrimSpace(fields[2])
	user := strings.TrimSpace(fields[3])
	tags := strings.TrimSpace(fields[4])

	ts := parseTime(timeStr)

	return &Operation{
		ID:          id,
		Description: description,
		Type:        ClassifyOperation(description),
		Timestamp:   ts,
		User:        user,
		Tags:        tags,
	}, nil
}

// parseTime attempts to parse a time string using known JJ time formats.
// Returns the zero time if no format matches.
func parseTime(s string) time.Time {
	for _, format := range knownTimeFormats {
		if t, err := time.Parse(format, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// reverseOperations reverses a slice of operations in place.
func reverseOperations(ops []Operation) {
	for i, j := 0, len(ops)-1; i < j; i, j = i+1, j-1 {
		//nolint:gosec // loop bounds guarantee i < j < len(ops), indices are safe
		ops[i], ops[j] = ops[j], ops[i]
	}
}
