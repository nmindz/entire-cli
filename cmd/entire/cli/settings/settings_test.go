package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_RejectsUnknownKeys(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()

	// Create .entire directory
	entireDir := filepath.Join(tmpDir, ".entire")
	if err := os.MkdirAll(entireDir, 0755); err != nil {
		t.Fatalf("failed to create .entire directory: %v", err)
	}

	// Create settings.json with an unknown key
	settingsFile := filepath.Join(entireDir, "settings.json")
	settingsContent := `{"strategy": "manual-commit", "unknown_key": "value"}`
	if err := os.WriteFile(settingsFile, []byte(settingsContent), 0644); err != nil {
		t.Fatalf("failed to write settings file: %v", err)
	}

	// Initialize a git repo (required by paths.AbsPath)
	if err := os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755); err != nil {
		t.Fatalf("failed to create .git directory: %v", err)
	}

	// Change to the temp directory
	t.Chdir(tmpDir)

	// Try to load settings - should fail due to unknown key
	_, err := Load()
	if err == nil {
		t.Error("expected error for unknown key, got nil")
	} else if !containsUnknownField(err.Error()) {
		t.Errorf("expected unknown field error, got: %v", err)
	}
}

func TestLoad_AcceptsValidKeys(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()

	// Create .entire directory
	entireDir := filepath.Join(tmpDir, ".entire")
	if err := os.MkdirAll(entireDir, 0755); err != nil {
		t.Fatalf("failed to create .entire directory: %v", err)
	}

	// Create settings.json with all valid keys
	settingsFile := filepath.Join(entireDir, "settings.json")
	settingsContent := `{
		"strategy": "auto-commit",
		"enabled": true,
		"local_dev": false,
		"log_level": "debug",
		"strategy_options": {"key": "value"},
		"telemetry": true,
		"redaction": {"pii": {"enabled": true, "email": true, "phone": false}}
	}`
	if err := os.WriteFile(settingsFile, []byte(settingsContent), 0644); err != nil {
		t.Fatalf("failed to write settings file: %v", err)
	}

	// Initialize a git repo (required by paths.AbsPath)
	if err := os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755); err != nil {
		t.Fatalf("failed to create .git directory: %v", err)
	}

	// Change to the temp directory
	t.Chdir(tmpDir)

	// Load settings - should succeed
	settings, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify values
	if settings.Strategy != "auto-commit" {
		t.Errorf("expected strategy 'auto-commit', got %q", settings.Strategy)
	}
	if !settings.Enabled {
		t.Error("expected enabled to be true")
	}
	if settings.LogLevel != "debug" {
		t.Errorf("expected log_level 'debug', got %q", settings.LogLevel)
	}
	if settings.Telemetry == nil || !*settings.Telemetry {
		t.Error("expected telemetry to be true")
	}
	if settings.Redaction == nil {
		t.Fatal("expected redaction to be non-nil")
	}
	if settings.Redaction.PII == nil {
		t.Fatal("expected redaction.pii to be non-nil")
	}
	if !settings.Redaction.PII.Enabled {
		t.Error("expected redaction.pii.enabled to be true")
	}
	if settings.Redaction.PII.Email == nil || !*settings.Redaction.PII.Email {
		t.Error("expected redaction.pii.email to be true")
	}
	if settings.Redaction.PII.Phone == nil || *settings.Redaction.PII.Phone {
		t.Error("expected redaction.pii.phone to be false")
	}
}

func TestLoad_LocalSettingsRejectsUnknownKeys(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()

	// Create .entire directory
	entireDir := filepath.Join(tmpDir, ".entire")
	if err := os.MkdirAll(entireDir, 0755); err != nil {
		t.Fatalf("failed to create .entire directory: %v", err)
	}

	// Create valid settings.json
	settingsFile := filepath.Join(entireDir, "settings.json")
	settingsContent := `{"strategy": "manual-commit"}`
	if err := os.WriteFile(settingsFile, []byte(settingsContent), 0644); err != nil {
		t.Fatalf("failed to write settings file: %v", err)
	}

	// Create settings.local.json with an unknown key
	localSettingsFile := filepath.Join(entireDir, "settings.local.json")
	localSettingsContent := `{"bad_key": true}`
	if err := os.WriteFile(localSettingsFile, []byte(localSettingsContent), 0644); err != nil {
		t.Fatalf("failed to write local settings file: %v", err)
	}

	// Initialize a git repo (required by paths.AbsPath)
	if err := os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755); err != nil {
		t.Fatalf("failed to create .git directory: %v", err)
	}

	// Change to the temp directory
	t.Chdir(tmpDir)

	// Try to load settings - should fail due to unknown key in local settings
	_, err := Load()
	if err == nil {
		t.Error("expected error for unknown key in local settings, got nil")
	} else if !containsUnknownField(err.Error()) {
		t.Errorf("expected unknown field error, got: %v", err)
	}
}

func TestLoad_MissingRedactionIsNil(t *testing.T) {
	tmpDir := t.TempDir()
	entireDir := filepath.Join(tmpDir, ".entire")
	if err := os.MkdirAll(entireDir, 0755); err != nil {
		t.Fatalf("failed to create .entire directory: %v", err)
	}

	settingsFile := filepath.Join(entireDir, "settings.json")
	if err := os.WriteFile(settingsFile, []byte(`{"strategy": "manual-commit"}`), 0644); err != nil {
		t.Fatalf("failed to write settings file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755); err != nil {
		t.Fatalf("failed to create .git directory: %v", err)
	}
	t.Chdir(tmpDir)

	settings, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if settings.Redaction != nil {
		t.Error("expected redaction to be nil when not in settings")
	}
}

func TestLoad_LocalOverridesRedaction(t *testing.T) {
	tmpDir := t.TempDir()
	entireDir := filepath.Join(tmpDir, ".entire")
	if err := os.MkdirAll(entireDir, 0755); err != nil {
		t.Fatalf("failed to create .entire directory: %v", err)
	}

	// Base settings: PII disabled
	settingsFile := filepath.Join(entireDir, "settings.json")
	if err := os.WriteFile(settingsFile, []byte(`{"strategy": "manual-commit", "redaction": {"pii": {"enabled": false}}}`), 0644); err != nil {
		t.Fatalf("failed to write settings file: %v", err)
	}

	// Local override: PII enabled with custom patterns
	localFile := filepath.Join(entireDir, "settings.local.json")
	localContent := `{"redaction": {"pii": {"enabled": true, "custom_patterns": {"employee_id": "EMP-\\d{6}"}}}}`
	if err := os.WriteFile(localFile, []byte(localContent), 0644); err != nil {
		t.Fatalf("failed to write local settings file: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755); err != nil {
		t.Fatalf("failed to create .git directory: %v", err)
	}
	t.Chdir(tmpDir)

	settings, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if settings.Redaction == nil || settings.Redaction.PII == nil {
		t.Fatal("expected redaction.pii to be non-nil after local override")
	}
	if !settings.Redaction.PII.Enabled {
		t.Error("expected local override to enable PII")
	}
	if settings.Redaction.PII.CustomPatterns == nil {
		t.Fatal("expected custom_patterns to be non-nil")
	}
	if settings.Redaction.PII.CustomPatterns["employee_id"] != `EMP-\d{6}` {
		t.Errorf("expected employee_id pattern, got %v", settings.Redaction.PII.CustomPatterns)
	}
}

// containsUnknownField checks if the error message indicates an unknown field
func containsUnknownField(msg string) bool {
	// Go's json package reports unknown fields with this message format
	return strings.Contains(msg, "unknown field")
}
