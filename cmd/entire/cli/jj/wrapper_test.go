package jj

import (
	"strings"
	"testing"
)

func TestGenerateWrapper_Zsh(t *testing.T) {
	t.Parallel()

	output := GenerateWrapper("zsh")

	checks := []string{
		"jj()",
		"command jj",
		`case "$1" in`,
		"commit|new|describe|amend|squash)",
		"entire hooks jj post-commit",
		"entire hooks jj pre-push",
		`"$2" = "push"`,
		"return $_entire_jj_exit",
	}
	for _, want := range checks {
		if !strings.Contains(output, want) {
			t.Errorf("zsh wrapper missing %q", want)
		}
	}
}

func TestGenerateWrapper_Bash(t *testing.T) {
	t.Parallel()

	bash := GenerateWrapper("bash")
	zsh := GenerateWrapper("zsh")

	if bash != zsh {
		t.Error("bash and zsh wrappers should be identical")
	}
}

func TestGenerateWrapper_Fish(t *testing.T) {
	t.Parallel()

	output := GenerateWrapper("fish")

	checks := []string{
		"function jj --wraps jj",
		"command jj $argv",
		"switch $argv[1]",
		"case commit new describe amend squash",
		"entire hooks jj post-commit",
		"entire hooks jj pre-push",
		"return $_entire_jj_exit",
		"end",
	}
	for _, want := range checks {
		if !strings.Contains(output, want) {
			t.Errorf("fish wrapper missing %q", want)
		}
	}
}

func TestGenerateWrapper_Unknown(t *testing.T) {
	t.Parallel()

	unknown := GenerateWrapper("powershell")
	zsh := GenerateWrapper("zsh")

	if unknown != zsh {
		t.Error("unknown shell should default to zsh output")
	}
}

func TestSupportedShells(t *testing.T) {
	t.Parallel()

	shells := SupportedShells()
	if len(shells) != 3 {
		t.Fatalf("expected 3 supported shells, got %d", len(shells))
	}

	want := map[string]bool{"bash": true, "zsh": true, "fish": true}
	for _, s := range shells {
		if !want[s] {
			t.Errorf("unexpected shell %q", s)
		}
	}
}

func TestSupportedShells_ReturnsCopy(t *testing.T) {
	t.Parallel()

	shells := SupportedShells()
	shells[0] = "modified"

	original := SupportedShells()
	if original[0] == "modified" {
		t.Error("SupportedShells should return a copy, not the original slice")
	}
}

// TestDetectShell cannot use t.Parallel() because t.Setenv modifies
// process-global state.
func TestDetectShell(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     string
	}{
		{"zsh path", "/bin/zsh", "zsh"},
		{"bash path", "/usr/bin/bash", "bash"},
		{"fish path", "/usr/local/bin/fish", "fish"},
		{"unknown shell", "/bin/csh", "zsh"},
		{"empty SHELL", "", "zsh"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SHELL", tt.envValue)

			got := DetectShell()
			if got != tt.want {
				t.Errorf("DetectShell() with SHELL=%q = %q, want %q", tt.envValue, got, tt.want)
			}
		})
	}
}
