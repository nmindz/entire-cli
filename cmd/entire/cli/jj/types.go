package jj

import "time"

// OperationType classifies a JJ operation by its semantic meaning.
// Values correspond to the operation descriptions from `jj op log`.
type OperationType string

const (
	// OpSnapshot represents a "snapshot working copy" operation.
	OpSnapshot OperationType = "snapshot working copy"
	// OpNew represents a "new empty commit" operation.
	OpNew OperationType = "new empty commit"
	// OpDescribe represents a "describe commit" operation.
	OpDescribe OperationType = "describe commit"
	// OpCommit represents a "commit working copy" operation (jj commit = jj new + describe).
	OpCommit OperationType = "commit working copy"
	// OpAmend represents an "amend/squash changes" operation.
	OpAmend OperationType = "amend/squash changes"
	// OpSquash represents a "squash commits" operation.
	OpSquash OperationType = "squash commits"
	// OpRebase represents a "rebase commits" operation.
	OpRebase OperationType = "rebase commits"
	// OpAbandon represents an "abandon commit" operation.
	OpAbandon OperationType = "abandon commit"
	// OpGitPush represents a "push to git remote" / "push bookmarks" operation.
	OpGitPush OperationType = "push to git remote"
	// OpGitFetch represents a "fetch from git remote" operation.
	OpGitFetch OperationType = "fetch from git remote"
	// OpEdit represents an "edit commit" operation.
	OpEdit OperationType = "edit commit"
	// OpOther represents any unclassified operation.
	OpOther OperationType = "other"
)

// Operation represents a single JJ operation log entry.
type Operation struct {
	ID          string        // Short operation ID
	Description string        // Raw description from jj op log
	Type        OperationType // Classified type
	Timestamp   time.Time     // When the operation occurred
	User        string        // Username that performed the operation
	Tags        string        // Operation tags (if any)
}

// checkpointTriggers are operations that "finalize" work and should trigger
// a checkpoint save.
var checkpointTriggers = map[OperationType]bool{
	OpNew:      true,
	OpDescribe: true,
	OpCommit:   true,
	OpAmend:    true,
	OpSquash:   true,
}

// pushTriggers are operations that push to a remote.
var pushTriggers = map[OperationType]bool{
	OpGitPush: true,
}

// IsCheckpointTrigger reports whether this operation type should trigger
// a checkpoint save. Returns true for: OpNew, OpDescribe, OpCommit, OpAmend, OpSquash.
func (op OperationType) IsCheckpointTrigger() bool {
	return checkpointTriggers[op]
}

// IsPushTrigger reports whether this operation type represents a push to a remote.
// Returns true for: OpGitPush.
func (op OperationType) IsPushTrigger() bool {
	return pushTriggers[op]
}

// String returns a human-readable name for the operation type.
func (op OperationType) String() string {
	return string(op)
}
