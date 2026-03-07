package jj

import (
	"strings"
	"testing"
)

func TestClassifyOperation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		description string
		want        OperationType
	}{
		{"snapshot working copy", OpSnapshot},
		{"describe commit abc123def456", OpDescribe},
		{"new empty commit", OpNew},
		{"push bookmark main to origin", OpGitPush},
		{"push bookmarks feature-x, feature-y to origin", OpGitPush},
		{"fetch from git remote(s) origin", OpGitFetch},
		{"commit working copy", OpCommit},
		{"squash commit abc into def", OpSquash},
		{"amend commit abc123", OpAmend},
		{"rebase 3 commits", OpRebase},
		{"abandon commit abc123", OpAbandon},
		{"edit commit abc123", OpEdit},
		{"undo operation abc123", OpOther},
		{"create bookmark feature-x", OpOther},
		{"", OpOther},
		// Prefix-based matches.
		{"new ", OpNew},
		{"describe xyz", OpDescribe},
		{"edit xyz", OpEdit},
		// Git push with branch keyword.
		{"push branch main to remote", OpGitPush},
		// Git push with git remote keyword.
		{"push to git remote origin", OpGitPush},
		// Fetch with git keyword.
		{"git fetch origin", OpGitFetch},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			t.Parallel()

			got := ClassifyOperation(tt.description)
			if got != tt.want {
				t.Errorf("ClassifyOperation(%q) = %q, want %q", tt.description, got, tt.want)
			}
		})
	}
}

func TestClassifyOperation_CaseInsensitive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		description string
		want        OperationType
	}{
		{"SNAPSHOT WORKING COPY", OpSnapshot},
		{"Snapshot Working Copy", OpSnapshot},
		{"Describe Commit abc123", OpDescribe},
		{"NEW EMPTY COMMIT", OpNew},
		{"Push Bookmark main to origin", OpGitPush},
		{"FETCH from GIT remote(s) origin", OpGitFetch},
		{"Commit Working Copy", OpCommit},
		{"SQUASH commit abc", OpSquash},
		{"AMEND commit abc123", OpAmend},
		{"REBASE 3 commits", OpRebase},
		{"ABANDON commit abc123", OpAbandon},
		{"Edit Commit abc123", OpEdit},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			t.Parallel()

			got := ClassifyOperation(tt.description)
			if got != tt.want {
				t.Errorf("ClassifyOperation(%q) = %q, want %q", tt.description, got, tt.want)
			}
		})
	}
}

func TestParseOpLogLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		line     string
		wantID   string
		wantDesc string
		wantType OperationType
		wantUser string
		wantTags string
		wantYear int // expected year (0 means zero time)
		wantZero bool
	}{
		{
			name:     "snapshot operation",
			line:     "abc123def456abcd\x00snapshot working copy\x002025-03-07 10:30:45.123 -08:00\x00testuser@host\x00",
			wantID:   "abc123def456abcd",
			wantDesc: "snapshot working copy",
			wantType: OpSnapshot,
			wantUser: "testuser@host",
			wantTags: "",
			wantYear: 2025,
		},
		{
			name:     "describe operation with tags",
			line:     "fedcba9876543210\x00describe commit abc123\x002025-06-15 14:22:33.000 +00:00\x00alice\x00verbose",
			wantID:   "fedcba9876543210",
			wantDesc: "describe commit abc123",
			wantType: OpDescribe,
			wantUser: "alice",
			wantTags: "verbose",
			wantYear: 2025,
		},
		{
			name:     "new empty commit",
			line:     "1111222233334444\x00new empty commit\x002025-01-01T00:00:00Z\x00bob\x00",
			wantID:   "1111222233334444",
			wantDesc: "new empty commit",
			wantType: OpNew,
			wantUser: "bob",
			wantTags: "",
			wantYear: 2025,
		},
		{
			name:     "push bookmark",
			line:     "aaaa111122223333\x00push bookmark main to origin\x002025-02-10 09:15:00.000 -05:00\x00deployer\x00",
			wantID:   "aaaa111122223333",
			wantDesc: "push bookmark main to origin",
			wantType: OpGitPush,
			wantUser: "deployer",
			wantTags: "",
			wantYear: 2025,
		},
		{
			name:     "unparseable time defaults to zero",
			line:     "deadbeefcafe1234\x00rebase 3 commits\x00not-a-time\x00user\x00",
			wantID:   "deadbeefcafe1234",
			wantDesc: "rebase 3 commits",
			wantType: OpRebase,
			wantUser: "user",
			wantTags: "",
			wantZero: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			op, err := parseOpLogLine(tt.line)
			if err != nil {
				t.Fatalf("parseOpLogLine() unexpected error: %v", err)
			}

			if op.ID != tt.wantID {
				t.Errorf("ID = %q, want %q", op.ID, tt.wantID)
			}
			if op.Description != tt.wantDesc {
				t.Errorf("Description = %q, want %q", op.Description, tt.wantDesc)
			}
			if op.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", op.Type, tt.wantType)
			}
			if op.User != tt.wantUser {
				t.Errorf("User = %q, want %q", op.User, tt.wantUser)
			}
			if op.Tags != tt.wantTags {
				t.Errorf("Tags = %q, want %q", op.Tags, tt.wantTags)
			}

			if tt.wantZero {
				if !op.Timestamp.IsZero() {
					t.Errorf("Timestamp = %v, want zero time", op.Timestamp)
				}
			} else {
				if op.Timestamp.IsZero() {
					t.Error("Timestamp is zero, want non-zero")
				}
				if op.Timestamp.Year() != tt.wantYear {
					t.Errorf("Timestamp.Year() = %d, want %d", op.Timestamp.Year(), tt.wantYear)
				}
			}
		})
	}
}

func TestParseOpLogLine_InvalidFieldCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
	}{
		{"too few fields", "abc123\x00snapshot working copy"},
		{"no separators", "abc123 snapshot working copy"},
		{"empty string", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseOpLogLine(tt.line)
			if err == nil {
				t.Fatal("expected error for invalid line format")
			}
			if !strings.Contains(err.Error(), "expected") {
				t.Errorf("error should mention 'expected', got: %v", err)
			}
		})
	}
}

func TestGetOperationsSince_StopsAtID(t *testing.T) {
	t.Parallel()

	// Simulate jj op log output with 4 operations (newest first).
	// We expect GetOperationsSince to stop at "op2222" and return op3333 and op4444
	// in chronological order (op3333 first, then op4444).
	lines := []string{
		"op4444aabbccddee\x00describe commit xyz\x002025-03-07 12:00:04.000 +00:00\x00user\x00",
		"op3333aabbccddee\x00new empty commit\x002025-03-07 12:00:03.000 +00:00\x00user\x00",
		"op2222aabbccddee\x00snapshot working copy\x002025-03-07 12:00:02.000 +00:00\x00user\x00",
		"op1111aabbccddee\x00commit working copy\x002025-03-07 12:00:01.000 +00:00\x00user\x00",
	}
	simulatedOutput := strings.Join(lines, "\n")

	// Parse directly to test the stopping logic (without calling RunJJ).
	ops := parseOperationsFromOutput(t, simulatedOutput, "op2222aabbccddee")

	if len(ops) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(ops))
	}

	// Chronological order: oldest first.
	if ops[0].ID != "op3333aabbccddee" {
		t.Errorf("first op ID = %q, want %q", ops[0].ID, "op3333aabbccddee")
	}
	if ops[0].Type != OpNew {
		t.Errorf("first op Type = %q, want %q", ops[0].Type, OpNew)
	}

	if ops[1].ID != "op4444aabbccddee" {
		t.Errorf("second op ID = %q, want %q", ops[1].ID, "op4444aabbccddee")
	}
	if ops[1].Type != OpDescribe {
		t.Errorf("second op Type = %q, want %q", ops[1].Type, OpDescribe)
	}
}

func TestGetOperationsSince_NoMatchReturnsAll(t *testing.T) {
	t.Parallel()

	lines := []string{
		"op2222aabbccddee\x00new empty commit\x002025-03-07 12:00:02.000 +00:00\x00user\x00",
		"op1111aabbccddee\x00snapshot working copy\x002025-03-07 12:00:01.000 +00:00\x00user\x00",
	}
	simulatedOutput := strings.Join(lines, "\n")

	ops := parseOperationsFromOutput(t, simulatedOutput, "nonexistent-id")

	// When the sentinel is not found, all ops are returned.
	if len(ops) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(ops))
	}

	// Chronological order: oldest first.
	if ops[0].ID != "op1111aabbccddee" {
		t.Errorf("first op ID = %q, want %q", ops[0].ID, "op1111aabbccddee")
	}
	if ops[1].ID != "op2222aabbccddee" {
		t.Errorf("second op ID = %q, want %q", ops[1].ID, "op2222aabbccddee")
	}
}

func TestGetOperationsSince_EmptyOutput(t *testing.T) {
	t.Parallel()

	ops := parseOperationsFromOutput(t, "", "any-id")

	if len(ops) != 0 {
		t.Fatalf("expected 0 operations, got %d", len(ops))
	}
}

func TestParseTime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantZero bool
		wantYear int
		wantHour int
	}{
		{
			name:     "space-separated with millis and colon offset",
			input:    "2025-03-07 10:30:45.123 -08:00",
			wantYear: 2025,
			wantHour: 10,
		},
		{
			name:     "space-separated with millis and numeric offset",
			input:    "2025-03-07 10:30:45.000 -0800",
			wantYear: 2025,
			wantHour: 10,
		},
		{
			name:     "space-separated without millis and colon offset",
			input:    "2025-03-07 10:30:45 -08:00",
			wantYear: 2025,
			wantHour: 10,
		},
		{
			name:     "RFC3339",
			input:    "2025-03-07T10:30:45Z",
			wantYear: 2025,
			wantHour: 10,
		},
		{
			name:     "ISO8601 with millis",
			input:    "2025-03-07T10:30:45.123-08:00",
			wantYear: 2025,
			wantHour: 10,
		},
		{
			name:     "unparseable returns zero",
			input:    "not-a-time",
			wantZero: true,
		},
		{
			name:     "empty returns zero",
			input:    "",
			wantZero: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := parseTime(tt.input)

			if tt.wantZero {
				if !got.IsZero() {
					t.Errorf("parseTime(%q) = %v, want zero time", tt.input, got)
				}
				return
			}

			if got.IsZero() {
				t.Fatalf("parseTime(%q) returned zero time, want non-zero", tt.input)
			}
			if got.Year() != tt.wantYear {
				t.Errorf("parseTime(%q).Year() = %d, want %d", tt.input, got.Year(), tt.wantYear)
			}
			if got.Hour() != tt.wantHour {
				t.Errorf("parseTime(%q).Hour() = %d, want %d", tt.input, got.Hour(), tt.wantHour)
			}
		})
	}
}

func TestReverseOperations(t *testing.T) {
	t.Parallel()

	ops := []Operation{
		{ID: "a"}, {ID: "b"}, {ID: "c"},
	}
	reverseOperations(ops)

	if ops[0].ID != "c" || ops[1].ID != "b" || ops[2].ID != "a" {
		t.Errorf("reverseOperations produced unexpected order: %v", ops)
	}

	// Single element.
	single := []Operation{{ID: "x"}}
	reverseOperations(single)
	if single[0].ID != "x" {
		t.Error("reverseOperations single element should be unchanged")
	}

	// Empty slice should not panic.
	reverseOperations(nil)
	reverseOperations([]Operation{})
}

// parseOperationsFromOutput is a test helper that replicates the parsing
// and stopping logic of GetOperationsSince without invoking RunJJ.
func parseOperationsFromOutput(t *testing.T, output string, sinceOpID string) []Operation {
	t.Helper()

	if output == "" {
		return nil
	}

	lines := strings.Split(output, "\n")
	var ops []Operation

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		op, err := parseOpLogLine(line)
		if err != nil {
			t.Fatalf("parseOpLogLine(%q) unexpected error: %v", line, err)
		}

		if op.ID == sinceOpID {
			break
		}

		ops = append(ops, *op)
	}

	reverseOperations(ops)

	return ops
}
