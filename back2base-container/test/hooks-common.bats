#!/usr/bin/env bats

# Tests for lib/hooks/_common.sh — the shared primitives every back2base
# hook script sources. Tests run the library in a subshell and assert on
# stdout, exit code, and side-effects (log file contents).

setup() {
  TEST_TMP="$(mktemp -d "${BATS_TMPDIR}/back2base-hooks-common.XXXXXX")"
  export HOME="$TEST_TMP/home"
  mkdir -p "$HOME/.claude"
  COMMON="$BATS_TEST_DIRNAME/../lib/hooks/_common.sh"
}

teardown() {
  rm -rf "$TEST_TMP"
}

# Run a tiny bash snippet that sources _common.sh and then runs the args.
run_with_common() {
  bash -c ". '$COMMON'; $*"
}

@test "_common: b2b_hook_init creates log dir and sets HOOK_NAME" {
  run run_with_common 'b2b_hook_init "demo"; echo "name=$HOOK_NAME"; echo "log=$HOOK_LOG"'
  [ "$status" -eq 0 ]
  [[ "$output" == *"name=demo"* ]]
  [[ "$output" == *"log=$HOME/.claude/.back2base-hooks/demo.log"* ]]
  [ -d "$HOME/.claude/.back2base-hooks" ]
}

@test "_common: b2b_hook_init drains stdin so writers do not block" {
  # Pipe a large blob; b2b_hook_init must consume it.
  run bash -c "yes | head -c 65536 | (. '$COMMON'; b2b_hook_init demo; cat | wc -c)"
  [ "$status" -eq 0 ]
  # Whatever remains on stdin AFTER b2b_hook_init runs is what `cat` prints.
  # Init drained it, so the count is 0.
  [[ "$output" == *"0"* ]]
}

@test "_common: b2b_hook_disabled returns 0 for off|0|false|no" {
  for v in off 0 false no; do
    run bash -c "FOO=$v; . '$COMMON'; b2b_hook_disabled FOO"
    [ "$status" -eq 0 ]
  done
}

@test "_common: b2b_hook_disabled returns nonzero for unset or other values" {
  run bash -c ". '$COMMON'; b2b_hook_disabled FOO"
  [ "$status" -ne 0 ]
  run bash -c "FOO=on; . '$COMMON'; b2b_hook_disabled FOO"
  [ "$status" -ne 0 ]
  run bash -c "FOO=1; . '$COMMON'; b2b_hook_disabled FOO"
  [ "$status" -ne 0 ]
}

@test "_common: b2b_hook_log appends ISO timestamp + name + message" {
  run_with_common 'b2b_hook_init demo; b2b_hook_log "hello world"'
  log="$HOME/.claude/.back2base-hooks/demo.log"
  [ -s "$log" ]
  # Format: <ISO timestamp> demo <message>
  grep -qE '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z demo hello world$' "$log"
}

@test "_common: b2b_hook_passthrough emits {} and exits 0" {
  run run_with_common 'b2b_hook_init demo; b2b_hook_passthrough'
  [ "$status" -eq 0 ]
  [ "$output" = "{}" ]
}

@test "_common: b2b_hook_emit_context emits well-formed JSON envelope" {
  run run_with_common 'b2b_hook_init demo; b2b_hook_emit_context "UserPromptSubmit" "hello"'
  [ "$status" -eq 0 ]
  evt=$(echo "$output" | jq -r '.hookSpecificOutput.hookEventName')
  ctx=$(echo "$output" | jq -r '.hookSpecificOutput.additionalContext')
  [ "$evt" = "UserPromptSubmit" ]
  [ "$ctx" = "hello" ]
}

@test "_common: b2b_hook_emit_context preserves multi-line context" {
  run run_with_common $'b2b_hook_init demo; b2b_hook_emit_context "UserPromptSubmit" $\'line1\\nline2\''
  [ "$status" -eq 0 ]
  ctx=$(echo "$output" | jq -r '.hookSpecificOutput.additionalContext')
  [ "$ctx" = $'line1\nline2' ]
}

@test "_common: b2b_hook_emit_raw passes through arbitrary JSON" {
  run run_with_common 'b2b_hook_init demo; b2b_hook_emit_raw "{\"foo\":42}"'
  [ "$status" -eq 0 ]
  [ "$(echo "$output" | jq -r '.foo')" = "42" ]
}

@test "_common: b2b_hook_emit_context falls through to passthrough when jq is missing" {
  # Create a restricted PATH that includes the essentials but excludes jq.
  shim_dir="$TEST_TMP/shim"
  mkdir -p "$shim_dir"
  # Copy necessary tools to shim_dir (or symlink if available).
  for tool in bash mkdir cat dirname date printf; do
    target=$(command -v "$tool" 2>/dev/null)
    [ -n "$target" ] && ln -sf "$target" "$shim_dir/$tool"
  done
  # Do NOT symlink jq, so command -v jq will fail.
  run env PATH="$shim_dir" bash -c ". '$COMMON'; b2b_hook_init demo; b2b_hook_emit_context 'UserPromptSubmit' 'hello'"
  [ "$status" -eq 0 ]
  [ "$output" = "{}" ]
  log="$HOME/.claude/.back2base-hooks/demo.log"
  grep -q "jq missing" "$log"
}
