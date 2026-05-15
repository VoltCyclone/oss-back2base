#!/usr/bin/env bats

# Tests for lib/status.sh — _status_run wraps a command in gum spin with
# graceful fallback to plain chatty output. Stub `gum` on PATH with a
# fixture script that records args, so we don't need real gum installed.

setup() {
  TEST_TMP="$(mktemp -d "${BATS_TMPDIR}/status.XXXXXX")"
  export TEST_TMP

  REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  HELPER="$REPO_ROOT/lib/status.sh"

  STUB_BIN="$TEST_TMP/stub-bin"
  mkdir -p "$STUB_BIN"
  export PATH="$STUB_BIN:/usr/bin:/bin"   # restrict so we don't pick up host gum

  # Default gum stub: records its args, runs the wrapped command (after `--`),
  # exits with that command's status.
  cat > "$STUB_BIN/gum" <<'EOF'
#!/bin/sh
echo "GUM_ARGS: $*" >> "$TEST_TMP/gum-calls.log"
# Find the `--` separator and run everything after it
shift_to_dashes() {
  while [ "$#" -gt 0 ]; do
    if [ "$1" = "--" ]; then shift; break; fi
    shift
  done
  if [ "$#" -gt 0 ]; then "$@"; fi
}
shift_to_dashes "$@"
EOF
  chmod +x "$STUB_BIN/gum"

  # Force TTY so the helper enters the fancy path even though bats redirects.
  export FORCE_TTY=1
}

teardown() {
  rm -rf "$TEST_TMP"
}

gum_call_count() {
  if [ -f "$TEST_TMP/gum-calls.log" ]; then
    wc -l < "$TEST_TMP/gum-calls.log" | tr -d ' '
  else
    echo "0"
  fi
}

@test "fancy mode runs gum spin with title" {
  # shellcheck source=/dev/null
  . "$HELPER"
  run _status_run "Test phase" /bin/sh -c 'echo hi'
  [ "$status" -eq 0 ]
  [ -f "$TEST_TMP/gum-calls.log" ]
  grep -q -- '--title Test phase' "$TEST_TMP/gum-calls.log"
  echo "$output" | grep -q '✓'
  echo "$output" | grep -q 'Test phase'
}

@test "chatty mode bypasses gum on BACK2BASE_VERBOSE=1" {
  export BACK2BASE_VERBOSE=1
  . "$HELPER"
  run _status_run "Verbose phase" /bin/sh -c 'echo verbose-out'
  [ "$status" -eq 0 ]
  [ "$(gum_call_count)" = "0" ]
  echo "$output" | grep -q '→ Verbose phase'
  echo "$output" | grep -q 'verbose-out'
}

@test "chatty mode when gum is missing" {
  rm -f "$STUB_BIN/gum"
  . "$HELPER"
  run _status_run "No-gum phase" /bin/sh -c 'true'
  [ "$status" -eq 0 ]
  echo "$output" | grep -q '→ No-gum phase'
}

@test "chatty mode when not on a TTY" {
  unset FORCE_TTY
  # Bats already redirects stderr; without FORCE_TTY, [ -t 2 ] is false.
  . "$HELPER"
  run _status_run "Non-tty phase" /bin/sh -c 'true'
  [ "$status" -eq 0 ]
  [ "$(gum_call_count)" = "0" ]
  echo "$output" | grep -q '→ Non-tty phase'
}

@test "nonzero exit is propagated and ✗ sigil is printed" {
  . "$HELPER"
  run _status_run "Failing phase" /bin/sh -c 'exit 7'
  [ "$status" -eq 7 ]
  echo "$output" | grep -q '✗'
  echo "$output" | grep -q 'Failing phase'
}

@test "_status_warn falls back to plain ⚠ on missing gum" {
  rm -f "$STUB_BIN/gum"
  . "$HELPER"
  run _status_warn "Heads up message"
  [ "$status" -eq 0 ]
  echo "$output" | grep -q '⚠ Heads up message'
}

@test "fancy mode dispatches shell functions via bash -c (regression)" {
  # Without the function-detection branch, gum spin's execve fails to find
  # the shell function and the phase silently aborts. This test catches
  # the bug that broke phase 2-8 in production.

  # Replace the gum stub with one that records args AND actually exec's the
  # `bash -c` wrapper so the function can run.
  cat > "$STUB_BIN/gum" <<'GSTUB'
#!/bin/sh
echo "GUM_ARGS: $*" >> "$TEST_TMP/gum-calls.log"
shift_to_dashes() {
  while [ "$#" -gt 0 ]; do
    if [ "$1" = "--" ]; then shift; break; fi
    shift
  done
  if [ "$#" -gt 0 ]; then "$@"; fi
}
shift_to_dashes "$@"
GSTUB
  chmod +x "$STUB_BIN/gum"

  . "$HELPER"
  _my_phase_helper() {
    echo "ran in helper" > "$TEST_TMP/helper-marker"
  }
  run _status_run "Function phase" _my_phase_helper
  [ "$status" -eq 0 ]
  [ -f "$TEST_TMP/helper-marker" ]
  grep "ran in helper" "$TEST_TMP/helper-marker"
  # Confirm the dispatch route went through `bash -c`
  grep -q -- '-- bash -c' "$TEST_TMP/gum-calls.log"
}

@test "fancy mode does not hang on backgrounded daemons that detach stdio (regression)" {
  # Phase 8 launches daemons under gum spin. If a daemon inherits the
  # wrapped command's stdout/stderr (a pipe gum holds open), the spinner
  # blocks forever waiting for EOF even though the daemon was disowned.
  # Daemons must redirect stdio to /dev/null at launch — this test
  # asserts the spinner returns promptly when a daemon does so.

  cat > "$STUB_BIN/gum" <<'GSTUB'
#!/bin/sh
echo "GUM_ARGS: $*" >> "$TEST_TMP/gum-calls.log"
shift_to_dashes() {
  while [ "$#" -gt 0 ]; do
    if [ "$1" = "--" ]; then shift; break; fi
    shift
  done
  if [ "$#" -gt 0 ]; then "$@"; fi
}
shift_to_dashes "$@"
GSTUB
  chmod +x "$STUB_BIN/gum"

  . "$HELPER"
  _launch_long_running_daemon() {
    # Simulate a real daemon: long-lived sleeper that writes periodically.
    # WITH stdio detached, the spinner must return immediately.
    sleep 30 </dev/null >/dev/null 2>&1 &
    disown
  }

  # If the wrapped command leaks fds into the spinner pipe, _status_run
  # would block forever waiting on the child bash. Measure elapsed time
  # to assert the spinner returned promptly.
  SECONDS=0
  run _status_run "Daemon phase" _launch_long_running_daemon
  [ "$status" -eq 0 ]
  [ "$SECONDS" -lt 5 ]

  # Reap the daemon we spawned so it doesn't outlive the test.
  pkill -P $$ -f "sleep 30" 2>/dev/null || true
}

@test "fancy mode exports nested helper functions to child shell (regression)" {
  # Phase wrappers like _phase5_seed_image_defaults call other shell
  # functions (seed_skills_if_missing, etc.). If _status_run only exports
  # the top-level wrapper, the child bash -c shell sees:
  #   environment: line N: seed_skills_if_missing: command not found
  # Regression guard: ensure all currently-defined functions get exported.

  cat > "$STUB_BIN/gum" <<'GSTUB'
#!/bin/sh
echo "GUM_ARGS: $*" >> "$TEST_TMP/gum-calls.log"
shift_to_dashes() {
  while [ "$#" -gt 0 ]; do
    if [ "$1" = "--" ]; then shift; break; fi
    shift
  done
  if [ "$#" -gt 0 ]; then "$@"; fi
}
shift_to_dashes "$@"
GSTUB
  chmod +x "$STUB_BIN/gum"

  . "$HELPER"
  _nested_helper_a() { echo "a-ran" >> "$TEST_TMP/nested-marker"; }
  _nested_helper_b() { echo "b-ran" >> "$TEST_TMP/nested-marker"; }
  _phase_wrapper() {
    _nested_helper_a
    _nested_helper_b
  }
  run _status_run "Nested phase" _phase_wrapper
  [ "$status" -eq 0 ]
  [ -f "$TEST_TMP/nested-marker" ]
  grep -q "a-ran" "$TEST_TMP/nested-marker"
  grep -q "b-ran" "$TEST_TMP/nested-marker"
  # No "command not found" leaked into stderr (captured in $output by run)
  ! echo "$output" | grep -q "command not found"
}
