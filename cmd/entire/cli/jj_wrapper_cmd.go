package cli

import (
	"fmt"

	"github.com/entireio/cli/cmd/entire/cli/jj"
	"github.com/spf13/cobra"
)

func newJJWrapperCmd() *cobra.Command {
	var shell string

	cmd := &cobra.Command{
		Use:   "jj-wrapper",
		Short: "Output JJ shell wrapper function",
		Long: `Output a shell function that wraps 'jj' to automatically trigger
Entire checkpoint operations after relevant JJ commands.

Usage:
  # Detect shell automatically
  eval "$(entire jj-wrapper)"

  # Specify shell explicitly
  eval "$(entire jj-wrapper --shell zsh)"

  # Add to shell profile for persistence
  echo 'eval "$(entire jj-wrapper)"' >> ~/.zshrc`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if shell == "" {
				shell = jj.DetectShell()
			}
			fmt.Fprint(cmd.OutOrStdout(), jj.GenerateWrapper(shell))
			return nil
		},
	}

	cmd.Flags().StringVar(&shell, "shell", "", "Shell type (bash, zsh, fish). Auto-detected if not specified.")

	return cmd
}
