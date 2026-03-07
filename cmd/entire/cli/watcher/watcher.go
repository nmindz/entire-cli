package watcher

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/entireio/cli/cmd/entire/cli/jj"
	"github.com/fsnotify/fsnotify"
)

// defaultDebounce is the default delay between the last filesystem event
// and triggering the handler. This prevents rapid-fire processing when
// JJ writes multiple files during a single operation.
const defaultDebounce = 200 * time.Millisecond

// opHeadsRelPath is the path within a JJ repo to the operation heads directory.
// When JJ completes an operation, it writes a new file here.
const opHeadsRelPath = ".jj/repo/op_heads"

// Option configures a Watcher.
type Option func(*Watcher)

// WithDebounce sets the debounce delay between filesystem events and processing.
func WithDebounce(d time.Duration) Option {
	return func(w *Watcher) {
		w.debounceDelay = d
	}
}

// WithLogger sets the structured logger for the watcher.
func WithLogger(l *slog.Logger) Option {
	return func(w *Watcher) {
		w.logger = l
	}
}

// ActionHandler is a function that executes an Entire action in response to
// a JJ operation. It is called by the watcher when a relevant operation
// is detected. The handler receives the repo directory, the action type,
// and the triggering operation.
//
// This is a variable to allow replacement in tests.
type ActionHandler func(ctx context.Context, dir string, action ActionType, op *jj.Operation) error

// Watcher monitors .jj/repo/op_heads/ for filesystem changes and triggers
// Entire checkpoint operations when JJ modifies the repository.
type Watcher struct {
	dir           string        // Repository root directory
	lastOpID      string        // Last processed operation ID
	debounceDelay time.Duration // Debounce interval
	logger        *slog.Logger  // Structured logger
	mu            sync.Mutex    // Protects lastOpID

	// onAction is called when the watcher detects a relevant JJ operation.
	// Defaults to defaultActionHandler which runs `entire hooks jj` commands.
	onAction ActionHandler
}

// New creates a new Watcher for the given repository root directory.
func New(dir string, opts ...Option) *Watcher {
	w := &Watcher{
		dir:           dir,
		debounceDelay: defaultDebounce,
		logger:        slog.Default(),
		onAction:      defaultActionHandler,
	}

	for _, opt := range opts {
		opt(w)
	}

	return w
}

// SetLastOpID sets the last processed operation ID (thread-safe).
// Use this to restore state from a previous run.
func (w *Watcher) SetLastOpID(opID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lastOpID = opID
}

// LastOpID returns the last processed operation ID (thread-safe).
func (w *Watcher) LastOpID() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastOpID
}

// Run starts the watcher loop and blocks until the context is cancelled.
// It monitors .jj/repo/op_heads/ for filesystem changes and triggers
// Entire actions when relevant JJ operations are detected.
func (w *Watcher) Run(ctx context.Context) error {
	opHeadsDir := filepath.Join(w.dir, opHeadsRelPath)

	// Verify the directory exists before starting.
	if _, err := os.Stat(opHeadsDir); err != nil {
		return fmt.Errorf("watcher: op_heads directory not found at %s: %w", opHeadsDir, err)
	}

	// Get initial operation ID if not already set.
	if w.LastOpID() == "" {
		opID, err := jj.GetLatestOperationID(ctx, w.dir)
		if err != nil {
			return fmt.Errorf("watcher: get initial operation ID: %w", err)
		}
		w.SetLastOpID(opID)
		w.logger.Info("watcher started",
			slog.String("dir", w.dir),
			slog.String("initial_op_id", opID))
	} else {
		w.logger.Info("watcher started (resumed)",
			slog.String("dir", w.dir),
			slog.String("last_op_id", w.LastOpID()))
	}

	// Create filesystem watcher.
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("watcher: create fsnotify watcher: %w", err)
	}
	defer fsWatcher.Close()

	if err := fsWatcher.Add(opHeadsDir); err != nil {
		return fmt.Errorf("watcher: watch %s: %w", opHeadsDir, err)
	}

	// Debounce timer. We don't start it until a relevant event fires.
	var debounceTimer *time.Timer
	var debounceCh <-chan time.Time

	for {
		select {
		case <-ctx.Done():
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			w.logger.Info("watcher stopping", slog.String("reason", "context cancelled"))
			return fmt.Errorf("watcher: %w", ctx.Err())

		case event, ok := <-fsWatcher.Events:
			if !ok {
				return nil // channel closed
			}

			// Only react to creates/writes (new op heads).
			if !event.Has(fsnotify.Create) && !event.Has(fsnotify.Write) {
				continue
			}

			// Reset or start the debounce timer.
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.NewTimer(w.debounceDelay)
			debounceCh = debounceTimer.C

		case err, ok := <-fsWatcher.Errors:
			if !ok {
				return nil // channel closed
			}
			w.logger.Warn("watcher: fsnotify error",
				slog.String("error", err.Error()))

		case <-debounceCh:
			// Debounce period elapsed — process the change.
			debounceCh = nil
			debounceTimer = nil
			if err := w.handleChange(ctx); err != nil {
				w.logger.Warn("watcher: handleChange error",
					slog.String("error", err.Error()))
			}
		}
	}
}

// handleChange processes a filesystem change in the op_heads directory.
// It reads the latest JJ operation, checks for duplicates, classifies the
// operation, and triggers the appropriate Entire action.
func (w *Watcher) handleChange(ctx context.Context) error {
	op, err := jj.GetLatestOperation(ctx, w.dir)
	if err != nil {
		return fmt.Errorf("get latest operation: %w", err)
	}

	// Check if we've already processed this operation.
	w.mu.Lock()
	if op.ID == w.lastOpID {
		w.mu.Unlock()
		w.logger.Debug("watcher: skipping duplicate operation",
			slog.String("op_id", op.ID))
		return nil
	}
	w.lastOpID = op.ID
	w.mu.Unlock()

	action := MapOperationToAction(op)

	w.logger.Info("watcher: operation detected",
		slog.String("op_id", op.ID),
		slog.String("op_type", op.Type.String()),
		slog.String("action", action.String()))

	if action == ActionNone {
		return nil
	}

	return w.onAction(ctx, w.dir, action, op)
}

// defaultActionHandler executes Entire CLI commands in response to JJ operations.
// For checkpoint triggers, it runs `entire hooks jj post-commit`.
// For push triggers, it runs `entire hooks jj pre-push origin`.
func defaultActionHandler(ctx context.Context, dir string, action ActionType, _ *jj.Operation) error {
	var args []string

	switch action {
	case ActionCheckpoint:
		args = []string{"hooks", "jj", "post-commit"}
	case ActionPush:
		args = []string{"hooks", "jj", "pre-push", "origin"}
	case ActionNone:
		return nil
	}

	entireBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find entire binary: %w", err)
	}

	cmd := exec.CommandContext(ctx, entireBin, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run entire %v: %w", args, err)
	}

	return nil
}
