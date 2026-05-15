#!/usr/bin/env bats

setup() {
  TEST_TMP="$(mktemp -d "${BATS_TMPDIR}/cma-hooks.XXXXXX")"
  export HOME="$TEST_TMP/home"
  mkdir -p "$HOME/.claude"
  TALLY_HOOK="$BATS_TEST_DIRNAME/../lib/hooks/claude-md-audit-tally.sh"
  TRIGGER_HOOK="$BATS_TEST_DIRNAME/../lib/hooks/claude-md-audit-trigger.sh"
  COMMON="$BATS_TEST_DIRNAME/../lib/hooks/_common.sh"
  AUDIT="$BATS_TEST_DIRNAME/../lib/claude-md-audit.py"
  export POWER_STEERING_DIR="$TEST_TMP/runtime/power-steering"
  mkdir -p "$POWER_STEERING_DIR"
  export POWER_STEERING_PENDING="$POWER_STEERING_DIR/pending.md"
  export BACK2BASE_HOOKS_LIB="$COMMON"
  export CLAUDE_MD_AUDIT_PY="$AUDIT"
}

teardown() { rm -rf "$TEST_TMP"; }

@test "tally hook: emits passthrough envelope" {
  payload='{"tool_name":"Edit","tool_input":{"file_path":"/repo/x.go"}}'
  run bash "$TALLY_HOOK" <<<"$payload"
  [ "$status" -eq 0 ]
  [ "$output" = "{}" ]
}

@test "tally hook: appends edit for Edit tool" {
  payload='{"tool_name":"Edit","tool_input":{"file_path":"/repo/x.go"}}'
  run bash "$TALLY_HOOK" <<<"$payload"
  [ "$status" -eq 0 ]
  [ -f "$POWER_STEERING_DIR/edits.jsonl" ]
  grep -q '/repo/x.go' "$POWER_STEERING_DIR/edits.jsonl"
}

@test "tally hook: ignores Read tool" {
  payload='{"tool_name":"Read","tool_input":{"file_path":"/repo/x.go"}}'
  run bash "$TALLY_HOOK" <<<"$payload"
  [ "$status" -eq 0 ]
  [ ! -f "$POWER_STEERING_DIR/edits.jsonl" ]
}

@test "tally hook: kill switch silences" {
  payload='{"tool_name":"Edit","tool_input":{"file_path":"/repo/x.go"}}'
  run env BACK2BASE_HOOK_CLAUDE_MD_AUDIT=off bash "$TALLY_HOOK" <<<"$payload"
  [ "$status" -eq 0 ]
  [ ! -f "$POWER_STEERING_DIR/edits.jsonl" ]
}

@test "trigger hook: emits passthrough when no signal" {
  run bash "$TRIGGER_HOOK" <<<'{}'
  [ "$status" -eq 0 ]
  [ "$output" = "{}" ]
  [ ! -s "$POWER_STEERING_PENDING" ]
}

@test "trigger hook: appends suggestion to pending.md when threshold met" {
  for i in $(seq 1 20); do
    payload="{\"tool_name\":\"Edit\",\"tool_input\":{\"file_path\":\"/repo/f$i.txt\"}}"
    bash "$TALLY_HOOK" <<<"$payload" >/dev/null
  done
  run bash "$TRIGGER_HOOK" <<<'{}'
  [ "$status" -eq 0 ]
  [ "$output" = "{}" ]
  [ -s "$POWER_STEERING_PENDING" ]
  grep -q "CLAUDE.md staleness audit" "$POWER_STEERING_PENDING"
}

@test "trigger hook: kill switch silences" {
  for i in $(seq 1 20); do
    payload="{\"tool_name\":\"Edit\",\"tool_input\":{\"file_path\":\"/repo/f$i.txt\"}}"
    bash "$TALLY_HOOK" <<<"$payload" >/dev/null
  done
  run env BACK2BASE_HOOK_CLAUDE_MD_AUDIT=off bash "$TRIGGER_HOOK" <<<'{}'
  [ "$status" -eq 0 ]
  [ ! -s "$POWER_STEERING_PENDING" ]
}

@test "manifest: claude-md-audit-tally registered as PostToolUse" {
  MANIFEST="$BATS_TEST_DIRNAME/../defaults/hooks.json"
  run jq -r '.hooks[] | select(.name=="claude-md-audit-tally") | .event' "$MANIFEST"
  [ "$status" -eq 0 ]
  [ "$output" = "PostToolUse" ]
}

@test "manifest: claude-md-audit-trigger registered as UserPromptSubmit" {
  MANIFEST="$BATS_TEST_DIRNAME/../defaults/hooks.json"
  run jq -r '.hooks[] | select(.name=="claude-md-audit-trigger") | .event' "$MANIFEST"
  [ "$status" -eq 0 ]
  [ "$output" = "UserPromptSubmit" ]
}

@test "manifest: claude-md-audit-trigger ordered BEFORE power-steering-drain" {
  MANIFEST="$BATS_TEST_DIRNAME/../defaults/hooks.json"
  trigger_idx=$(jq '[.hooks[] | .name] | index("claude-md-audit-trigger")' "$MANIFEST")
  drain_idx=$(jq '[.hooks[] | .name] | index("power-steering-drain")' "$MANIFEST")
  [ "$trigger_idx" != "null" ]
  [ "$drain_idx" != "null" ]
  [ "$trigger_idx" -lt "$drain_idx" ]
}
