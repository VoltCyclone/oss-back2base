#!/usr/bin/env bats

setup() {
  TEST_TMP="$(mktemp -d "${BATS_TMPDIR}/cma.XXXXXX")"
  export HOME="$TEST_TMP/home"
  mkdir -p "$HOME/.claude"
  AUDIT="$BATS_TEST_DIRNAME/../lib/claude-md-audit.py"
  export POWER_STEERING_DIR="$TEST_TMP/runtime/power-steering"
  mkdir -p "$POWER_STEERING_DIR"
}

teardown() { rm -rf "$TEST_TMP"; }

@test "cli: usage on no subcommand" {
  run bash -c 'python3 "$0" 2>&1' "$AUDIT"
  [ "$status" -ne 0 ]
  [[ "$output" == *"tally"* ]]
  [[ "$output" == *"decide"* ]]
}

@test "cli: tally subcommand exists" {
  run python3 "$AUDIT" tally <<<'{}'
  [ "$status" -eq 0 ]
}

@test "cli: decide subcommand exists" {
  run python3 "$AUDIT" decide
  [ "$status" -eq 0 ]
}

@test "tally: appends jsonl entry for Edit" {
  payload='{"tool_name":"Edit","tool_input":{"file_path":"/repo/src/foo.go"}}'
  run python3 "$AUDIT" tally <<<"$payload"
  [ "$status" -eq 0 ]
  [ -f "$POWER_STEERING_DIR/edits.jsonl" ]
  line="$(cat "$POWER_STEERING_DIR/edits.jsonl")"
  [[ "$line" == *'"tool":"Edit"'* ]]
  [[ "$line" == *'/repo/src/foo.go'* ]]
}

@test "tally: appends for Write" {
  payload='{"tool_name":"Write","tool_input":{"file_path":"/repo/x.txt"}}'
  run python3 "$AUDIT" tally <<<"$payload"
  [ "$status" -eq 0 ]
  grep -q '"tool":"Write"' "$POWER_STEERING_DIR/edits.jsonl"
}

@test "tally: appends for MultiEdit" {
  payload='{"tool_name":"MultiEdit","tool_input":{"file_path":"/repo/y.go"}}'
  run python3 "$AUDIT" tally <<<"$payload"
  [ "$status" -eq 0 ]
  grep -q '"tool":"MultiEdit"' "$POWER_STEERING_DIR/edits.jsonl"
}

@test "tally: ignores Read" {
  payload='{"tool_name":"Read","tool_input":{"file_path":"/repo/x.go"}}'
  run python3 "$AUDIT" tally <<<"$payload"
  [ "$status" -eq 0 ]
  [ ! -f "$POWER_STEERING_DIR/edits.jsonl" ]
}

@test "tally: ignores Bash" {
  payload='{"tool_name":"Bash","tool_input":{"command":"ls"}}'
  run python3 "$AUDIT" tally <<<"$payload"
  [ "$status" -eq 0 ]
  [ ! -f "$POWER_STEERING_DIR/edits.jsonl" ]
}

@test "tally: tolerates malformed payload" {
  run python3 "$AUDIT" tally <<<"not json"
  [ "$status" -eq 0 ]
  [ ! -f "$POWER_STEERING_DIR/edits.jsonl" ]
}

@test "tally: creates state dir if missing" {
  rm -rf "$POWER_STEERING_DIR"
  payload='{"tool_name":"Edit","tool_input":{"file_path":"/repo/a.go"}}'
  run python3 "$AUDIT" tally <<<"$payload"
  [ "$status" -eq 0 ]
  [ -f "$POWER_STEERING_DIR/edits.jsonl" ]
}

@test "decide: silent on missing edits.jsonl" {
  run python3 "$AUDIT" decide
  [ "$status" -eq 0 ]
  [ -z "$output" ]
}

@test "decide: silent below thresholds" {
  for i in 1 2 3; do
    payload="{\"tool_name\":\"Edit\",\"tool_input\":{\"file_path\":\"/repo/f$i.txt\"}}"
    python3 "$AUDIT" tally <<<"$payload"
  done
  run python3 "$AUDIT" decide
  [ "$status" -eq 0 ]
  [ -z "$output" ]
}

@test "decide: kill switch silences" {
  for i in $(seq 1 30); do
    payload="{\"tool_name\":\"Edit\",\"tool_input\":{\"file_path\":\"/repo/f$i.txt\"}}"
    python3 "$AUDIT" tally <<<"$payload"
  done
  run env BACK2BASE_HOOK_CLAUDE_MD_AUDIT=off python3 "$AUDIT" decide
  [ "$status" -eq 0 ]
  [ -z "$output" ]
}

@test "decide: triggers suggestion at n_edits threshold (default 20)" {
  for i in $(seq 1 20); do
    payload="{\"tool_name\":\"Edit\",\"tool_input\":{\"file_path\":\"/repo/f$i.txt\"}}"
    python3 "$AUDIT" tally <<<"$payload"
  done
  run python3 "$AUDIT" decide
  [ "$status" -eq 0 ]
  [[ "$output" == *"CLAUDE.md staleness audit"* ]]
  [[ "$output" == *"/revise-claude-md"* ]]
  [ -f "$POWER_STEERING_DIR/audit-state.json" ]
  grep -q "last_suggested_ts" "$POWER_STEERING_DIR/audit-state.json"
}

@test "decide: lower threshold via env var" {
  for i in 1 2 3; do
    payload="{\"tool_name\":\"Edit\",\"tool_input\":{\"file_path\":\"/repo/f$i.txt\"}}"
    python3 "$AUDIT" tally <<<"$payload"
  done
  run env BACK2BASE_CLAUDE_MD_AUDIT_EVERY_N_EDITS=3 python3 "$AUDIT" decide
  [ "$status" -eq 0 ]
  [[ "$output" == *"CLAUDE.md staleness audit"* ]]
}

@test "decide: triggers on n_referenced_edits threshold even below n_edits" {
  PROJECT="$TEST_TMP/project"
  mkdir -p "$PROJECT/back2base-container/lib"
  cat > "$PROJECT/CLAUDE.md" <<'EOF'
# Project guide
Files live in `back2base-container/lib/` and the entry point is `main.go`.
EOF
  for i in 1 2 3 4; do
    payload="{\"tool_name\":\"Edit\",\"tool_input\":{\"file_path\":\"$PROJECT/back2base-container/lib/foo$i.go\"}}"
    python3 "$AUDIT" tally <<<"$payload"
  done
  cd "$PROJECT"
  run python3 "$AUDIT" decide
  [ "$status" -eq 0 ]
  [[ "$output" == *"CLAUDE.md staleness audit"* ]]
  [[ "$output" == *"back2base-container/lib"* ]]
}

@test "decide: silent when edits do not match referenced paths" {
  PROJECT="$TEST_TMP/project2"
  mkdir -p "$PROJECT"
  cat > "$PROJECT/CLAUDE.md" <<'EOF'
# Tiny
Edit files in `src/`.
EOF
  for i in 1 2 3 4 5; do
    payload="{\"tool_name\":\"Edit\",\"tool_input\":{\"file_path\":\"/elsewhere/x$i.txt\"}}"
    python3 "$AUDIT" tally <<<"$payload"
  done
  cd "$PROJECT"
  run env BACK2BASE_CLAUDE_MD_AUDIT_EVERY_N_EDITS=20 python3 "$AUDIT" decide
  [ "$status" -eq 0 ]
  [ -z "$output" ]
}

@test "decide: triggers on structural file edit + 5 edits" {
  for i in 1 2 3 4; do
    payload="{\"tool_name\":\"Edit\",\"tool_input\":{\"file_path\":\"/repo/file$i.txt\"}}"
    python3 "$AUDIT" tally <<<"$payload"
  done
  payload='{"tool_name":"Edit","tool_input":{"file_path":"/repo/Makefile"}}'
  python3 "$AUDIT" tally <<<"$payload"
  cd "$TEST_TMP"
  run python3 "$AUDIT" decide
  [ "$status" -eq 0 ]
  [[ "$output" == *"CLAUDE.md staleness audit"* ]]
}

@test "decide: structural alone (only 1 edit) does NOT trigger" {
  payload='{"tool_name":"Edit","tool_input":{"file_path":"/repo/Dockerfile"}}'
  python3 "$AUDIT" tally <<<"$payload"
  cd "$TEST_TMP"
  run python3 "$AUDIT" decide
  [ "$status" -eq 0 ]
  [ -z "$output" ]
}

@test "decide: silent during suggest cooldown" {
  for i in $(seq 1 20); do
    payload="{\"tool_name\":\"Edit\",\"tool_input\":{\"file_path\":\"/repo/f$i.txt\"}}"
    python3 "$AUDIT" tally <<<"$payload"
  done
  run python3 "$AUDIT" decide
  [ "$status" -eq 0 ]
  [ -n "$output" ]
  # Second invocation immediately after — should be silent.
  run python3 "$AUDIT" decide
  [ "$status" -eq 0 ]
  [ -z "$output" ]
}

@test "decide: cooldown can be shortened via env" {
  for i in $(seq 1 20); do
    payload="{\"tool_name\":\"Edit\",\"tool_input\":{\"file_path\":\"/repo/f$i.txt\"}}"
    python3 "$AUDIT" tally <<<"$payload"
  done
  python3 "$AUDIT" decide >/dev/null
  sleep 1
  run env BACK2BASE_CLAUDE_MD_AUDIT_SUGGEST_COOLDOWN_SEC=0 python3 "$AUDIT" decide
  [ "$status" -eq 0 ]
  [[ "$output" == *"CLAUDE.md staleness audit"* ]]
}

@test "decide: silent when drift was raised recently" {
  for i in $(seq 1 20); do
    payload="{\"tool_name\":\"Edit\",\"tool_input\":{\"file_path\":\"/repo/f$i.txt\"}}"
    python3 "$AUDIT" tally <<<"$payload"
  done
  now=$(date +%s)
  printf '{"last_drift_ts":%s}\n' "$now" > "$POWER_STEERING_DIR/drift-state.json"
  run python3 "$AUDIT" decide
  [ "$status" -eq 0 ]
  [ -z "$output" ]
}

@test "decide: drift cooldown does not block when stale" {
  for i in $(seq 1 20); do
    payload="{\"tool_name\":\"Edit\",\"tool_input\":{\"file_path\":\"/repo/f$i.txt\"}}"
    python3 "$AUDIT" tally <<<"$payload"
  done
  old=$(( $(date +%s) - 99999 ))
  printf '{"last_drift_ts":%s}\n' "$old" > "$POWER_STEERING_DIR/drift-state.json"
  run python3 "$AUDIT" decide
  [ "$status" -eq 0 ]
  [[ "$output" == *"CLAUDE.md staleness audit"* ]]
}

@test "decide: drift-state.json malformed → treated as no drift" {
  for i in $(seq 1 20); do
    payload="{\"tool_name\":\"Edit\",\"tool_input\":{\"file_path\":\"/repo/f$i.txt\"}}"
    python3 "$AUDIT" tally <<<"$payload"
  done
  echo "not json" > "$POWER_STEERING_DIR/drift-state.json"
  run python3 "$AUDIT" decide
  [ "$status" -eq 0 ]
  [[ "$output" == *"CLAUDE.md staleness audit"* ]]
}

@test "decide: CLAUDE.md mtime advance resets edit counter" {
  PROJECT="$TEST_TMP/project_reset"
  mkdir -p "$PROJECT"
  echo "# guide" > "$PROJECT/CLAUDE.md"
  cd "$PROJECT"

  # First round: hit threshold, get suggestion.
  for i in $(seq 1 20); do
    payload="{\"tool_name\":\"Edit\",\"tool_input\":{\"file_path\":\"$PROJECT/f$i.txt\"}}"
    python3 "$AUDIT" tally <<<"$payload"
  done
  python3 "$AUDIT" decide >/dev/null

  # User "audits" — touch CLAUDE.md to bump mtime.
  sleep 1
  echo "# guide v2" > "$PROJECT/CLAUDE.md"

  # Add 1 more edit. Should be silent (counter reset; far below threshold).
  payload="{\"tool_name\":\"Edit\",\"tool_input\":{\"file_path\":\"$PROJECT/extra.txt\"}}"
  python3 "$AUDIT" tally <<<"$payload"

  run env BACK2BASE_CLAUDE_MD_AUDIT_SUGGEST_COOLDOWN_SEC=0 python3 "$AUDIT" decide
  [ "$status" -eq 0 ]
  [ -z "$output" ]
}
