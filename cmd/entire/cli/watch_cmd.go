package cli

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/entireio/cli/cmd/entire/cli/paths"
	"github.com/entireio/cli/cmd/entire/cli/vcs"
	"github.com/entireio/cli/cmd/entire/cli/watcher"

	"github.com/spf13/cobra"
)

// pidFileName is the PID file name within .entire/.
const pidFileName = "watcher.pid"

// pidFilePath returns the full path to the watcher PID file.
func pidFilePath(repoRoot string) string {
	return filepath.Join(repoRoot, paths.EntireDir, pidFileName)
}

// processGracePeriod is how long to wait for the daemon to exit after SIGTERM.
const processGracePeriod = 5 * time.Second

func newWatchCmd() *cobra.Command {
	var daemonMode bool

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch for JJ operations and trigger Entire checkpoints",
		Long: `Start a filesystem watcher that monitors JJ's operation log directory
(.jj/repo/op_heads/) and automatically triggers checkpoint operations
when JJ modifies the repository.

Requires a JJ colocated repository (both .jj/ and .git/ present).

Without subcommands, runs the watcher in the foreground (blocks until interrupted).
Use 'entire watch start' to run as a background daemon.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			if err := vcs.RequireColocated(ctx); err != nil {
				cmd.SilenceUsage = true
				fmt.Fprintln(cmd.ErrOrStderr(), "Not a JJ colocated repository. The watcher requires both .jj/ and .git/ directories.")
				return NewSilentError(err)
			}

			repoRoot, err := paths.WorktreeRoot(ctx)
			if err != nil {
				cmd.SilenceUsage = true
				fmt.Fprintln(cmd.ErrOrStderr(), "Failed to determine repository root.")
				return NewSilentError(fmt.Errorf("watch: get repo root: %w", err))
			}

			logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{
				Level: slog.LevelInfo,
			}))

			w := watcher.New(repoRoot,
				watcher.WithLogger(logger),
			)

			// Load saved state to resume from last known operation.
			state, loadErr := watcher.LoadState(repoRoot)
			if loadErr != nil {
				logger.Warn("failed to load watcher state, starting fresh",
					slog.String("error", loadErr.Error()))
			} else if state.LastOpID != "" {
				w.SetLastOpID(state.LastOpID)
			}

			// Set up signal handling for graceful shutdown.
			ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
			defer stop()

			if daemonMode {
				// Write our PID when running in daemon mode.
				if writeErr := writePIDFile(repoRoot); writeErr != nil {
					logger.Warn("failed to write PID file",
						slog.String("error", writeErr.Error()))
				}
				defer removePIDFile(repoRoot)
			}

			fmt.Fprintf(cmd.ErrOrStderr(), "Watching for JJ operations in %s (Ctrl+C to stop)\n", repoRoot)

			runErr := w.Run(ctx)

			// Save state on exit (best effort).
			saveState := &watcher.State{
				LastOpID: w.LastOpID(),
				LastRun:  time.Now(),
			}
			if saveErr := watcher.SaveState(repoRoot, saveState); saveErr != nil {
				logger.Warn("failed to save watcher state",
					slog.String("error", saveErr.Error()))
			}

			// Context cancellation is expected on shutdown — not an error.
			if runErr != nil && ctx.Err() != nil {
				return nil
			}

			return runErr //nolint:wrapcheck // error originates from watcher.Run, already wrapped there
		},
	}

	cmd.Flags().BoolVar(&daemonMode, "daemon", false, "Run as daemon (internal use)")
	if err := cmd.Flags().MarkHidden("daemon"); err != nil {
		// This should never fail — panic is appropriate for programmer error.
		panic(fmt.Sprintf("watch: mark --daemon hidden: %v", err))
	}

	cmd.AddCommand(newWatchStartCmd())
	cmd.AddCommand(newWatchStopCmd())
	cmd.AddCommand(newWatchStatusCmd())

	return cmd
}

func newWatchStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the watcher as a background daemon",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			if err := vcs.RequireColocated(ctx); err != nil {
				cmd.SilenceUsage = true
				fmt.Fprintln(cmd.ErrOrStderr(), "Not a JJ colocated repository.")
				return NewSilentError(err)
			}

			repoRoot, err := paths.WorktreeRoot(ctx)
			if err != nil {
				cmd.SilenceUsage = true
				return NewSilentError(fmt.Errorf("watch start: get repo root: %w", err))
			}

			// Check if already running.
			if pid, running := readPIDIfAlive(repoRoot); running {
				fmt.Fprintf(cmd.OutOrStdout(), "Watcher already running (PID: %d)\n", pid)
				return nil
			}

			// Spawn self as background daemon.
			entireBin, err := os.Executable()
			if err != nil {
				return fmt.Errorf("watch start: find executable: %w", err)
			}

			daemonCmd := exec.CommandContext(ctx, entireBin, "watch", "--daemon")
			daemonCmd.Dir = repoRoot
			daemonCmd.SysProcAttr = &syscall.SysProcAttr{
				Setsid: true, // Detach from terminal
			}

			if err := daemonCmd.Start(); err != nil {
				return fmt.Errorf("watch start: spawn daemon: %w", err)
			}

			pid := daemonCmd.Process.Pid

			// Write PID file (daemon will also write it, but write here
			// immediately so 'status' works right away).
			if writeErr := writePIDFileWithPID(repoRoot, pid); writeErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to write PID file: %v\n", writeErr)
			}

			// Detach — don't wait for the child process.
			if err := daemonCmd.Process.Release(); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to release daemon process: %v\n", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Watcher started (PID: %d)\n", pid)
			return nil
		},
	}
}

func newWatchStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running watcher daemon",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			repoRoot, err := paths.WorktreeRoot(ctx)
			if err != nil {
				cmd.SilenceUsage = true
				return NewSilentError(fmt.Errorf("watch stop: get repo root: %w", err))
			}

			pid, running := readPIDIfAlive(repoRoot)
			if !running {
				fmt.Fprintln(cmd.OutOrStdout(), "Watcher is not running")
				removePIDFile(repoRoot)
				return nil
			}

			// Send SIGTERM for graceful shutdown.
			proc, err := os.FindProcess(pid)
			if err != nil {
				removePIDFile(repoRoot)
				return fmt.Errorf("watch stop: find process %d: %w", pid, err)
			}

			if signalErr := proc.Signal(syscall.SIGTERM); signalErr != nil {
				removePIDFile(repoRoot)
				// Process may have already exited — not a real error.
				fmt.Fprintf(cmd.OutOrStdout(), "Watcher stopped (process %d already exited)\n", pid)
				return nil //nolint:nilerr // signal failure means process already exited — success
			}

			// Wait for process to exit.
			if waitForProcessExit(pid, processGracePeriod) {
				removePIDFile(repoRoot)
				fmt.Fprintf(cmd.OutOrStdout(), "Watcher stopped (PID: %d)\n", pid)
				return nil
			}

			// Force kill if still running — ignore error since we're cleaning up.
			_ = proc.Kill() //nolint:errcheck // best-effort kill during cleanup
			removePIDFile(repoRoot)
			fmt.Fprintf(cmd.OutOrStdout(), "Watcher killed (PID: %d)\n", pid)
			return nil
		},
	}
}

func newWatchStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show watcher daemon status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			repoRoot, err := paths.WorktreeRoot(ctx)
			if err != nil {
				cmd.SilenceUsage = true
				return NewSilentError(fmt.Errorf("watch status: get repo root: %w", err))
			}

			pidFile := pidFilePath(repoRoot)
			data, err := os.ReadFile(pidFile) //nolint:gosec // path is derived from repo root + fixed constant, not user-controlled
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Fprintln(cmd.OutOrStdout(), "Watcher: not running")
					return nil
				}
				return fmt.Errorf("watch status: read PID file: %w", err)
			}

			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if parseErr != nil {
				removePIDFile(repoRoot)
				fmt.Fprintln(cmd.OutOrStdout(), "Watcher: not running (cleaned up invalid PID file)")
				return nil //nolint:nilerr // invalid PID file is cleaned up — not a user-facing error
			}

			if isProcessAlive(pid) {
				fmt.Fprintf(cmd.OutOrStdout(), "Watcher: running (PID: %d)\n", pid)
			} else {
				removePIDFile(repoRoot)
				fmt.Fprintln(cmd.OutOrStdout(), "Watcher: not running (cleaned up stale PID file)")
			}

			return nil
		},
	}
}

// writePIDFile writes the current process's PID to .entire/watcher.pid.
func writePIDFile(repoRoot string) error {
	return writePIDFileWithPID(repoRoot, os.Getpid())
}

// writePIDFileWithPID writes the given PID to .entire/watcher.pid.
func writePIDFileWithPID(repoRoot string, pid int) error {
	entireDir := filepath.Join(repoRoot, paths.EntireDir)
	if err := os.MkdirAll(entireDir, 0o750); err != nil {
		return fmt.Errorf("create .entire dir: %w", err)
	}

	pidStr := strconv.Itoa(pid)
	if err := os.WriteFile(pidFilePath(repoRoot), []byte(pidStr), 0o600); err != nil {
		return fmt.Errorf("write PID file: %w", err)
	}

	return nil
}

// removePIDFile removes the PID file (best effort).
func removePIDFile(repoRoot string) {
	_ = os.Remove(pidFilePath(repoRoot))
}

// readPIDIfAlive reads the PID file and checks if the process is alive.
// Returns the PID and true if the process is alive, or 0 and false otherwise.
func readPIDIfAlive(repoRoot string) (int, bool) {
	data, err := os.ReadFile(pidFilePath(repoRoot))
	if err != nil {
		return 0, false
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false
	}

	return pid, isProcessAlive(pid)
}

// isProcessAlive checks if a process with the given PID exists and is alive.
// Uses signal 0 which doesn't actually send a signal but checks for the process.
func isProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// Signal 0 checks if the process exists without sending a real signal.
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

// waitForProcessExit polls until the process exits or the timeout is reached.
// Returns true if the process exited, false if timeout was reached.
func waitForProcessExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !isProcessAlive(pid) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}
