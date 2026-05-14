#!/usr/bin/env bats

# Tests for lib/prelaunch-overview.sh — orchestrates `claude -p` + splice
# into CLAUDE.md, with timeout, heartbeat, and graceful-skip on failure.

setup() {
  TEST_TMP="$(mktemp -d "${BATS_TMPDIR}/prelaunch-overview.XXXXXX")"
  export TEST_TMP

  REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  HELPER="$REPO_ROOT/lib/prelaunch-overview.sh"
  RENDERER="$REPO_ROOT/lib/render-overview.py"
  PROMPT_FILE="$REPO_ROOT/lib/prelaunch-prompt.txt"

  # Stub `claude` and any other binaries we need on PATH.
  STUB_BIN="$TEST_TMP/stub-bin"
  mkdir -p "$STUB_BIN"
  export PATH="$STUB_BIN:$PATH"

  # Faux HOME with a starter CLAUDE.md.
  export HOME="$TEST_TMP/home"
  mkdir -p "$HOME/.claude"
  echo "# Stub CLAUDE.md" > "$HOME/.claude/CLAUDE.md"

  # Faux mounts the helper expects.
  export OVERVIEW_RENDERER_PATH="$RENDERER"
  export OVERVIEW_PROMPT_PATH="$PROMPT_FILE"

  # Tighten timing for tests so the suite stays fast.
  export OVERVIEW_TIMEOUT_SECS=3
  export OVERVIEW_HEARTBEAT_SECS=1
}

teardown() {
  rm -rf "$TEST_TMP"
}

stub_claude() {
  cat > "$STUB_BIN/claude" <<EOF
#!/bin/sh
$1
EOF
  chmod +x "$STUB_BIN/claude"
}

@test "skip when BACK2BASE_OVERVIEW=0" {
  export BACK2BASE_OVERVIEW=0
  stub_claude 'echo "should not run"; exit 0'

  # shellcheck source=/dev/null
  . "$HELPER"
  run run_prelaunch_overview
  [ "$status" -eq 0 ]
  ! grep -q 'should not run' "$HOME/.claude/CLAUDE.md"
  ! grep -q 'overview-begin' "$HOME/.claude/CLAUDE.md"
}

@test "skip when BACK2BASE_OVERVIEW unset" {
  unset BACK2BASE_OVERVIEW
  stub_claude 'echo "should not run"; exit 0'

  . "$HELPER"
  run run_prelaunch_overview
  [ "$status" -eq 0 ]
  ! grep -q 'overview-begin' "$HOME/.claude/CLAUDE.md"
}

@test "splice on success" {
  export BACK2BASE_OVERVIEW=1
  stub_claude 'cat <<MARK
## What this is
Hello overview body.
MARK
exit 0'

  . "$HELPER"
  run run_prelaunch_overview
  [ "$status" -eq 0 ]
  grep -q 'overview-begin' "$HOME/.claude/CLAUDE.md"
  grep -q 'Hello overview body.' "$HOME/.claude/CLAUDE.md"
}

@test "skip when claude exits non-zero" {
  export BACK2BASE_OVERVIEW=1
  stub_claude 'echo partial; exit 7'

  . "$HELPER"
  run run_prelaunch_overview
  [ "$status" -eq 0 ]
  echo "$output" | grep -q 'overview skipped: claude exited 7'
  ! grep -q 'overview-begin' "$HOME/.claude/CLAUDE.md"
}

@test "skip on empty output" {
  export BACK2BASE_OVERVIEW=1
  stub_claude 'exit 0'  # nothing to stdout

  . "$HELPER"
  run run_prelaunch_overview
  [ "$status" -eq 0 ]
  echo "$output" | grep -q 'overview skipped: empty output'
  ! grep -q 'overview-begin' "$HOME/.claude/CLAUDE.md"
}

@test "timeout kills slow claude" {
  export BACK2BASE_OVERVIEW=1
  stub_claude 'sleep 10; echo too-late'

  . "$HELPER"
  run run_prelaunch_overview
  [ "$status" -eq 0 ]
  echo "$output" | grep -qE 'overview skipped: timeout after [0-9]+s'
  ! grep -q 'too-late' "$HOME/.claude/CLAUDE.md"
}

@test "heartbeat lines appear during slow run" {
  export BACK2BASE_OVERVIEW=1
  stub_claude 'sleep 2; echo done'

  . "$HELPER"
  run run_prelaunch_overview
  [ "$status" -eq 0 ]
  # OVERVIEW_HEARTBEAT_SECS=1 → at least one "still running" line in 2s
  echo "$output" | grep -q 'still running'
}

@test "missing claude binary skips gracefully" {
  export BACK2BASE_OVERVIEW=1
  rm -f "$STUB_BIN/claude"
  # Restrict PATH so no real `claude` binary leaks in from the host environment.
  export PATH="$STUB_BIN:/usr/local/bin:/usr/bin:/bin"

  . "$HELPER"
  run run_prelaunch_overview
  [ "$status" -eq 0 ]
  echo "$output" | grep -q 'overview skipped: claude CLI not found'
}
