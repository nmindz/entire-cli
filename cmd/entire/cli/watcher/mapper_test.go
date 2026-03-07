package watcher

import (
	"testing"

	"github.com/entireio/cli/cmd/entire/cli/jj"
	"github.com/stretchr/testify/assert"
)

func TestMapOperationToAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		op   *jj.Operation
		want ActionType
	}{
		{
			name: "nil operation returns none",
			op:   nil,
			want: ActionNone,
		},
		{
			name: "new commit triggers checkpoint",
			op:   &jj.Operation{Type: jj.OpNew},
			want: ActionCheckpoint,
		},
		{
			name: "describe triggers checkpoint",
			op:   &jj.Operation{Type: jj.OpDescribe},
			want: ActionCheckpoint,
		},
		{
			name: "commit triggers checkpoint",
			op:   &jj.Operation{Type: jj.OpCommit},
			want: ActionCheckpoint,
		},
		{
			name: "amend triggers checkpoint",
			op:   &jj.Operation{Type: jj.OpAmend},
			want: ActionCheckpoint,
		},
		{
			name: "squash triggers checkpoint",
			op:   &jj.Operation{Type: jj.OpSquash},
			want: ActionCheckpoint,
		},
		{
			name: "git push triggers push",
			op:   &jj.Operation{Type: jj.OpGitPush},
			want: ActionPush,
		},
		{
			name: "snapshot returns none",
			op:   &jj.Operation{Type: jj.OpSnapshot},
			want: ActionNone,
		},
		{
			name: "git fetch returns none",
			op:   &jj.Operation{Type: jj.OpGitFetch},
			want: ActionNone,
		},
		{
			name: "rebase returns none",
			op:   &jj.Operation{Type: jj.OpRebase},
			want: ActionNone,
		},
		{
			name: "abandon returns none",
			op:   &jj.Operation{Type: jj.OpAbandon},
			want: ActionNone,
		},
		{
			name: "edit returns none",
			op:   &jj.Operation{Type: jj.OpEdit},
			want: ActionNone,
		},
		{
			name: "other returns none",
			op:   &jj.Operation{Type: jj.OpOther},
			want: ActionNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := MapOperationToAction(tt.op)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestActionType_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a    ActionType
		want string
	}{
		{name: "none", a: ActionNone, want: "none"},
		{name: "checkpoint", a: ActionCheckpoint, want: "checkpoint"},
		{name: "push", a: ActionPush, want: "push"},
		{name: "unknown value", a: ActionType(99), want: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.a.String())
		})
	}
}
