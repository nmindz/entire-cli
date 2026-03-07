# JJ (Jujutsu) Integration

Entire CLI supports [Jujutsu](https://martinvonz.github.io/jj/) in **colocated mode** — repos with both `.jj/` and `.git/` directories. Entire's checkpoint system uses Git primitives (shadow branches, metadata branch, go-git), so a `.git/` directory must be present.

## Prerequisites

- A JJ colocated repository:
  ```bash
  jj git init --colocate    # new repo
  # or
  jj init --colocate        # existing directory
  ```
- Entire CLI installed and enabled:
  ```bash
  entire enable
  ```
  The enable flow auto-detects JJ colocated mode and suggests using `entire jj-wrapper`.

## Why This Exists

JJ bypasses Git hooks entirely — it uses gitoxide (a Rust Git library) for local operations instead of invoking the `git` CLI. This means `.git/hooks/post-commit`, `prepare-commit-msg`, etc. are never triggered. Entire provides three alternative ways to trigger checkpoint operations.

## Quick Start

The fastest path: add one line to your shell profile.

```bash
# Add to ~/.zshrc or ~/.bashrc
eval "$(entire jj-wrapper)"
```

Restart your shell. Now `jj commit`, `jj new`, and other commands automatically trigger Entire checkpoints.

## Three Ways to Use Entire with JJ

### 1. Manual Hook Commands (Simplest)

Run Entire's hook commands manually after JJ operations.

**After committing:**

```bash
jj commit -m "Add feature"
entire hooks jj post-commit
```

**Before/after pushing:**

```bash
entire hooks jj pre-push          # pushes entire/checkpoints/v1 to origin
jj git push
```

The `post-commit` command:

1. Reads the HEAD commit from `.git/`
2. Checks for active Entire sessions
3. Generates a checkpoint ID and adds an `Entire-Checkpoint` trailer via `jj describe`
4. Triggers session condensation (saves session data to `entire/checkpoints/v1`)

The `pre-push` command pushes the `entire/checkpoints/v1` branch alongside your code. It accepts an optional remote argument (defaults to `origin`).

**When to use:** Quick testing, one-off operations, or scripting.

### 2. Shell Wrapper (Recommended)

The shell wrapper installs a function that intercepts `jj` commands and automatically triggers the appropriate Entire hook.

**Setup:**

```bash
# Auto-detect shell
eval "$(entire jj-wrapper)"

# Or specify explicitly
eval "$(entire jj-wrapper --shell zsh)"
eval "$(entire jj-wrapper --shell bash)"
entire jj-wrapper --shell fish | source
```

**Make it permanent:**

```bash
# Zsh
echo 'eval "$(entire jj-wrapper)"' >> ~/.zshrc

# Bash
echo 'eval "$(entire jj-wrapper)"' >> ~/.bashrc

# Fish
echo 'entire jj-wrapper --shell fish | source' >> ~/.config/fish/config.fish
```

**Intercepted commands:**

| JJ Command    | Entire Hook Triggered         |
| ------------- | ----------------------------- |
| `jj commit`   | `entire hooks jj post-commit` |
| `jj new`      | `entire hooks jj post-commit` |
| `jj describe` | `entire hooks jj post-commit` |
| `jj amend`    | `entire hooks jj post-commit` |
| `jj squash`   | `entire hooks jj post-commit` |
| `jj git push` | `entire hooks jj pre-push`    |

Hooks run in the background (`&`) for commit-like operations and won't block your terminal. Hooks are only triggered on successful JJ commands (exit code 0).

**When to use:** Daily development. Covers all CLI-based JJ workflows with zero friction.

### 3. Filesystem Watcher (Most Comprehensive)

The watcher monitors `.jj/repo/op_heads/` for changes and triggers Entire checkpoints for any JJ operation — including those from GUIs, IDEs, or other tools.

**Start as daemon:**

```bash
entire watch start     # background daemon
entire watch status    # check if running
entire watch stop      # stop daemon
```

**Run in foreground (debugging):**

```bash
entire watch           # blocks until Ctrl+C
```

**How it works:**

1. Watches `.jj/repo/op_heads/` via fsnotify
2. When JJ creates a new operation head, the watcher detects it
3. Queries `jj op log --limit 1` to classify the operation
4. Triggers the appropriate `entire hooks jj` command
5. Persists the last-processed operation ID to `.entire/watcher-state.json`

The daemon writes its PID to `.entire/watcher.pid` and handles graceful shutdown via SIGTERM.

**When to use:** When you use JJ through GUIs, IDEs, or want to catch every operation regardless of how JJ is invoked.

## Health Checks

`entire doctor` includes JJ-specific checks when a `.jj/` directory is present:

```bash
entire doctor
```

**Checks performed:**

- **JJ without colocated mode:** Warns and suggests `jj git init --colocate`
- **Missing `jj` binary:** Warns if a colocated repo is detected but `jj` isn't on PATH
- **Integration suggestion:** Recommends `entire jj-wrapper` for automatic tracking

## Limitations

- **Colocated mode only.** JJ-only repos (no `.git/`) are not supported. Entire's storage layer depends on Git primitives.
- **JJ has no native hooks.** All integration approaches are workarounds. When JJ adds native hooks (tracked in jj issues #405, #3577), Entire will adopt them.
- **Shell wrapper is CLI-only.** It won't catch JJ operations from GUIs or IDEs — use the filesystem watcher for that.
- **Watcher requires `jj` on PATH.** It needs to run `jj op log` to classify operations.
- **Pre-push behavior.** JJ may or may not trigger Git's pre-push hook depending on its push implementation. Use `entire hooks jj pre-push` explicitly or rely on the shell wrapper.

## Troubleshooting

**"Not a JJ colocated repository"**
You need both `.jj/` and `.git/` directories. Convert with:

```bash
jj git init --colocate
```

**Checkpoint trailers not appearing on commits**
Make sure you're running `entire hooks jj post-commit` after JJ commit operations (or using the shell wrapper). The trailer is added via `jj describe`, which amends the commit message.

**Watcher not detecting operations**

1. Check status: `entire watch status`
2. Ensure `.jj/repo/op_heads/` exists
3. Try running in foreground for debugging: `entire watch`

**Shell wrapper not activating**

1. Verify the wrapper is loaded: `type jj` should show a function, not the binary path
2. Make sure `eval "$(entire jj-wrapper)"` comes after any other JJ-related shell config
3. Restart your shell after adding to profile
