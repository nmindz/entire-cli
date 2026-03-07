// Package watcher monitors JJ's operation log directory for filesystem changes
// and automatically triggers Entire checkpoint operations when JJ modifies the repo.
// JJ has no hook system, so this package provides an alternative mechanism
// by watching `.jj/repo/op_heads/` for new operation IDs.
package watcher

import "github.com/entireio/cli/cmd/entire/cli/jj"

// ActionType represents the Entire action to take in response to a JJ operation.
type ActionType int

const (
	// ActionNone means no Entire action is needed.
	ActionNone ActionType = iota
	// ActionCheckpoint means a checkpoint should be saved (post-commit equivalent).
	ActionCheckpoint
	// ActionPush means a pre-push action should be triggered.
	ActionPush
)

// String returns a human-readable name for the action type.
func (a ActionType) String() string {
	switch a {
	case ActionCheckpoint:
		return "checkpoint"
	case ActionPush:
		return "push"
	case ActionNone:
		return "none"
	default:
		return "unknown"
	}
}

// MapOperationToAction determines which Entire action should be triggered
// for a given JJ operation. It uses the operation's classified type to decide
// whether a checkpoint save, push, or no action is appropriate.
func MapOperationToAction(op *jj.Operation) ActionType {
	if op == nil {
		return ActionNone
	}

	switch {
	case op.Type.IsCheckpointTrigger():
		return ActionCheckpoint
	case op.Type.IsPushTrigger():
		return ActionPush
	default:
		return ActionNone
	}
}
