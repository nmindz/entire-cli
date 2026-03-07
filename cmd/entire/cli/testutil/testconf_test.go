package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGitName_ReturnsDefault(t *testing.T) {
	t.Parallel()
	name := GitName()
	if name == "" {
		t.Error("GitName() should not be empty")
	}
}

func TestGitEmail_ReturnsDefault(t *testing.T) {
	t.Parallel()
	email := GitEmail()
	if email == "" {
		t.Error("GitEmail() should not be empty")
	}
}

func TestTestAuthor_ReturnsValidSignature(t *testing.T) {
	t.Parallel()
	author := TestAuthor()
	if author.Name == "" {
		t.Error("TestAuthor().Name should not be empty")
	}
	if author.Email == "" {
		t.Error("TestAuthor().Email should not be empty")
	}
	if author.When.IsZero() {
		t.Error("TestAuthor().When should not be zero")
	}
}

func TestParseConfFile_ValidFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.conf")
	content := "# comment\n\nKEY1=value1\nKEY2 = value2\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	parsed, ok := parseConfFile(path)
	if !ok {
		t.Fatal("parseConfFile should return ok=true")
	}
	if parsed["KEY1"] != "value1" {
		t.Errorf("KEY1 = %q, want %q", parsed["KEY1"], "value1")
	}
	if parsed["KEY2"] != "value2" {
		t.Errorf("KEY2 = %q, want %q", parsed["KEY2"], "value2")
	}
}

func TestParseConfFile_MissingFile(t *testing.T) {
	t.Parallel()
	_, ok := parseConfFile("/nonexistent/file")
	if ok {
		t.Error("parseConfFile should return ok=false for missing file")
	}
}

func TestParseConfFile_IgnoresComments(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.conf")
	content := "# This is a comment\nKEY=value\n# Another comment\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	parsed, ok := parseConfFile(path)
	if !ok {
		t.Fatal("parseConfFile should return ok=true")
	}
	if len(parsed) != 1 {
		t.Errorf("parsed should have 1 entry, got %d", len(parsed))
	}
}
