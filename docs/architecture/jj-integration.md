# JJ (Jujutsu) Integration

## Overview

Entire CLI supports Jujutsu (JJ) as a version control system alongside Git.
JJ integration requires **colocated mode** (`jj git init --colocate`), which
maintains both `.jj/` and `.git/` directories.

## Why Colocated Mode is Required

Entire's checkpoint system is built on Git primitives:

- Shadow branches (`entire/<hash>-<worktreeHash>`)
- Metadata branch (`entire/checkpoints/v1`)
- Session state in `.git/entire-sessions/`
- go-git v6 for all Git operations

In colocated mode, JJ maintains a full `.git/` directory that Entire's existing
infrastructure uses unchanged. JJ-only repos (no `.git/`) would require a
complete rewrite of the storage layer.

## The Hook Gap

Git hooks (`prepare-commit-msg`, `post-commit`, `pre-push`) are the primary
integration mechanism for tracking coding sessions. JJ bypasses these hooks
because it uses `gitoxide` (a Rust Git library) for all local operations
instead of invoking the `git` CLI.

### What JJ Bypasses

- `.git/hooks/prepare-commit-msg` — never triggered
- `.git/hooks/commit-msg` — never triggered
- `.git/hooks/post-commit` — never triggered
- `.git/hooks/pre-push` — may be triggered if JJ uses `git` subprocess for push

### JJ Hook System Status (as of 2026)

- JJ intentionally has NO hook system (GitHub issues #405, #3577)
- Native hooks planned for eventual daemon process (no timeline)
- Pre-push hook most likely to be added first

## Integration Approaches

### 1. Manual CLI Commands

`entire hooks jj post-commit` and `entire hooks jj pre-push [remote]`

These commands perform the equivalent of Git's prepare-commit-msg + post-commit
(combined) and pre-push hooks. Users invoke them manually after JJ operations.

### 2. Shell Wrapper

`eval "$(entire jj-wrapper)"` installs a shell function that wraps `jj` and
automatically triggers Entire commands after checkpoint-relevant operations:

- `jj commit`, `jj new`, `jj describe`, `jj amend`, `jj squash` → `entire hooks jj post-commit`
- `jj git push` → `entire hooks jj pre-push`

### 3. Filesystem Watcher

`entire watch` monitors `.jj/repo/op_heads/` for changes. When JJ performs any
operation, a new operation head file is created. The watcher detects this,
queries `jj op log` to classify the operation, and triggers the appropriate
Entire command.

This catches ALL JJ operations regardless of invocation method (CLI, GUI, IDE).

## Architecture

### No Strategy Changes

The key insight: since colocated repos have `.git/`, all existing strategy
code works unchanged. The integration only fills the hook-triggering gap.

### New Packages

- `cmd/entire/cli/vcs/` — VCS type detection
- `cmd/entire/cli/jj/` — JJ CLI execution, operation parsing, shell wrapper
- `cmd/entire/cli/watcher/` — Filesystem watcher daemon

### New Commands

- `entire hooks jj post-commit` — Manual checkpoint trigger
- `entire hooks jj pre-push [remote]` — Manual push trigger
- `entire jj-wrapper [--shell zsh|bash|fish]` — Output shell wrapper
- `entire watch` — Start watcher (foreground)
- `entire watch start` — Start watcher (daemon)
- `entire watch stop` — Stop watcher daemon
- `entire watch status` — Show watcher status

### How JJ Post-Commit Works

1. Opens Git repo via go-git (works because colocated)
2. Reads HEAD commit
3. Finds active sessions for the worktree
4. Generates a checkpoint ID
5. Adds `Entire-Checkpoint` trailer via `jj describe` (or `git commit --amend` fallback)
6. Calls `strategy.PostCommit()` which reads the trailer and condenses session data

### Filesystem Watcher Flow

1. Watches `.jj/repo/op_heads/` via fsnotify
2. On change: debounce 200ms, query `jj op log --limit 1`
3. Classify operation (checkpoint trigger, push trigger, or ignore)
4. Execute appropriate `entire hooks jj` command
5. Persist last-processed operation ID to `.entire/watcher-state.json`

## Limitations

- JJ-only repos (no `.git/`) are not supported
- Shell wrapper doesn't catch GUI/IDE JJ operations (use watcher for that)
- Watcher requires `jj` binary on PATH
- Watcher adds minor resource overhead (one fsnotify fd)

## Future Work

- Native JJ hooks (when JJ adds them): register Entire as a hook consumer
- JJ-only mode: would require a new storage backend (significant effort)
- JJ workspace support: handle multiple JJ workspaces
