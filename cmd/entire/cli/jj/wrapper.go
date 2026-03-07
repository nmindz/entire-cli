package jj

import (
	"os"
	"path/filepath"
	"slices"
)

// shellZsh is the default shell constant to avoid magic string repetition.
const shellZsh = "zsh"

const bashZshWrapper = `# Entire CLI: JJ wrapper for automatic session tracking
# Source this in your ~/.zshrc or ~/.bashrc:
#   eval "$(entire jj-wrapper --shell zsh)"
#
# This wraps the 'jj' command to automatically trigger Entire checkpoint
# operations after relevant JJ commands (describe, new, commit, git push).
jj() {
  command jj "$@"
  local _entire_jj_exit=$?
  if [ $_entire_jj_exit -eq 0 ]; then
    case "$1" in
      commit|new|describe|amend|squash)
        entire hooks jj post-commit 2>/dev/null &
        ;;
      git)
        if [ "$2" = "push" ]; then
          entire hooks jj pre-push "${3:-origin}" 2>/dev/null
        fi
        ;;
    esac
  fi
  return $_entire_jj_exit
}
`

const fishWrapper = `# Entire CLI: JJ wrapper for automatic session tracking
# Source this in your ~/.config/fish/config.fish:
#   entire jj-wrapper --shell fish | source
#
# This wraps the 'jj' command to automatically trigger Entire checkpoint
# operations after relevant JJ commands (describe, new, commit, git push).
function jj --wraps jj
  command jj $argv
  set _entire_jj_exit $status
  if test $_entire_jj_exit -eq 0
    switch $argv[1]
      case commit new describe amend squash
        entire hooks jj post-commit 2>/dev/null &
      case git
        if test (count $argv) -ge 2; and test $argv[2] = push
          set -l _remote origin
          if test (count $argv) -ge 3
            set _remote $argv[3]
          end
          entire hooks jj pre-push $_remote 2>/dev/null
        end
    end
  end
  return $_entire_jj_exit
end
`

// supportedShells is the list of shell types that GenerateWrapper can produce output for.
var supportedShells = []string{"bash", "zsh", "fish"}

// GenerateWrapper returns a shell wrapper function for the given shell type.
// The wrapper intercepts 'jj' commands and triggers Entire checkpoint operations
// after relevant JJ commands (describe, new, commit, git push).
// Supported shells: "bash", "zsh", "fish". Unknown shells default to zsh output.
func GenerateWrapper(shell string) string {
	switch shell {
	case "fish":
		return fishWrapper
	case "bash", shellZsh:
		return bashZshWrapper
	default:
		return bashZshWrapper
	}
}

// SupportedShells returns the list of shell types that GenerateWrapper can produce output for.
func SupportedShells() []string {
	return slices.Clone(supportedShells)
}

// DetectShell detects the current shell from the SHELL environment variable.
// It extracts the basename (e.g., "/bin/zsh" → "zsh") and returns it if recognized.
// Defaults to "zsh" (macOS default) if the shell is unrecognized or SHELL is unset.
func DetectShell() string {
	shellPath := os.Getenv("SHELL")
	if shellPath == "" {
		return shellZsh
	}

	shell := filepath.Base(shellPath)
	if slices.Contains(supportedShells, shell) {
		return shell
	}

	return shellZsh
}
