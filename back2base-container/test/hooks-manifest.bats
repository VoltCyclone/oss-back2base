#!/usr/bin/env bats

# Tests for defaults/hooks.json + lib/render-hooks.py.
# The renderer is a stdlib Python script; we exercise it as a subprocess
# against a temporary settings.json under TEST_TMP.

setup() {
  TEST_TMP="$(mktemp -d "${BATS_TMPDIR}/back2base-hooks-manifest.XXXXXX")"
  export HOME="$TEST_TMP/home"
  mkdir -p "$HOME/.claude"
  MANIFEST="$BATS_TEST_DIRNAME/../defaults/hooks.json"
  RENDERER="$BATS_TEST_DIRNAME/../lib/render-hooks.py"
  SETTINGS="$HOME/.claude/settings.json"
}

teardown() {
  rm -rf "$TEST_TMP"
}

# Seed a minimal settings.json without a hooks block.
seed_minimal() {
  cat > "$SETTINGS" <<'EOF'
{
  "_back2base_schema": 4,
  "statusLine": {"type": "command", "command": "/opt/back2base/statusline.sh", "padding": 0}
}
EOF
}

# ── Manifest schema ──────────────────────────────────────────────────

@test "manifest: parses as JSON" {
  run python3 -c "import json; json.load(open('$MANIFEST'))"
  [ "$status" -eq 0 ]
}

@test "manifest: every entry has required keys" {
  run python3 -c "
import json
required = {'name', 'event', 'command', 'timeout', 'kill_switch', 'handler'}
m = json.load(open('$MANIFEST'))
for entry in m['hooks']:
    missing = required - set(entry.keys())
    assert not missing, f'{entry.get(\"name\")} missing {missing}'
"
  [ "$status" -eq 0 ]
}

@test "manifest: no duplicate hook names" {
  run python3 -c "
import json
m = json.load(open('$MANIFEST'))
names = [e['name'] for e in m['hooks']]
assert len(names) == len(set(names)), f'duplicates: {names}'
"
  [ "$status" -eq 0 ]
}

@test "manifest: every kill_switch matches BACK2BASE_HOOK_<UPPER>" {
  run python3 -c "
import json, re
m = json.load(open('$MANIFEST'))
for e in m['hooks']:
    assert re.match(r'^BACK2BASE_HOOK_[A-Z_]+$', e['kill_switch']), e
"
  [ "$status" -eq 0 ]
}

@test "manifest: version is 1" {
  run python3 -c "
import json
m = json.load(open('$MANIFEST'))
assert m.get('version') == 1, m.get('version')
"
  [ "$status" -eq 0 ]
}

# ── Renderer behavior ────────────────────────────────────────────────

@test "renderer: writes hooks block keyed by event" {
  seed_minimal
  run python3 "$RENDERER" "$SETTINGS" --manifest "$MANIFEST"
  [ "$status" -eq 0 ]
  run python3 -c "
import json
s = json.load(open('$SETTINGS'))
assert 'hooks' in s
events = sorted(s['hooks'].keys())
print(' '.join(events))
"
  [ "$status" -eq 0 ]
  [[ "$output" == *"PostToolUse"* ]]
  [[ "$output" == *"PreCompact"* ]]
  [[ "$output" == *"Stop"* ]]
  [[ "$output" == *"UserPromptSubmit"* ]]
}

@test "renderer: hooks block uses matcher+hooks envelope shape" {
  seed_minimal
  python3 "$RENDERER" "$SETTINGS" --manifest "$MANIFEST"
  run python3 -c "
import json
s = json.load(open('$SETTINGS'))
ups = s['hooks']['UserPromptSubmit']
assert isinstance(ups, list)
assert all('matcher' in g and 'hooks' in g for g in ups), ups
inner = ups[0]['hooks'][0]
print(inner['type'], inner['command'])
"
  [ "$status" -eq 0 ]
  [[ "$output" == "command "* ]]
}

@test "renderer: idempotent — second run matches first byte-for-byte" {
  seed_minimal
  python3 "$RENDERER" "$SETTINGS" --manifest "$MANIFEST"
  cp "$SETTINGS" "$TEST_TMP/run1.json"
  python3 "$RENDERER" "$SETTINGS" --manifest "$MANIFEST"
  diff -q "$SETTINGS" "$TEST_TMP/run1.json"
}

@test "renderer: malformed manifest leaves settings.json untouched + exit 0" {
  seed_minimal
  cp "$SETTINGS" "$TEST_TMP/before.json"
  bad="$TEST_TMP/bad.json"
  echo "not valid json {{" > "$bad"
  run python3 "$RENDERER" "$SETTINGS" --manifest "$bad"
  [ "$status" -eq 0 ]
  diff -q "$SETTINGS" "$TEST_TMP/before.json"
  # render.log captured the failure
  [ -s "$HOME/.claude/.back2base-hooks/render.log" ]
}

@test "renderer: missing manifest leaves settings.json untouched + exit 0" {
  seed_minimal
  cp "$SETTINGS" "$TEST_TMP/before.json"
  run python3 "$RENDERER" "$SETTINGS" --manifest "/nonexistent/hooks.json"
  [ "$status" -eq 0 ]
  diff -q "$SETTINGS" "$TEST_TMP/before.json"
}

@test "renderer: schema-violation entry rejects the whole manifest" {
  seed_minimal
  cp "$SETTINGS" "$TEST_TMP/before.json"
  bad="$TEST_TMP/bad.json"
  cat > "$bad" <<'EOF'
{"version": 1, "hooks": [{"name": "x"}]}
EOF
  run python3 "$RENDERER" "$SETTINGS" --manifest "$bad"
  [ "$status" -eq 0 ]
  diff -q "$SETTINGS" "$TEST_TMP/before.json"
}

@test "renderer: replaces only the hooks key, leaves other keys alone" {
  cat > "$SETTINGS" <<'EOF'
{
  "_back2base_schema": 4,
  "statusLine": {"type": "command", "command": "/opt/back2base/statusline.sh", "padding": 0},
  "userCustomKey": "should-survive",
  "hooks": {"OldEvent": []}
}
EOF
  python3 "$RENDERER" "$SETTINGS" --manifest "$MANIFEST"
  run python3 -c "
import json
s = json.load(open('$SETTINGS'))
print(s.get('userCustomKey'))
print('OldEvent' in s.get('hooks', {}))
"
  [ "$status" -eq 0 ]
  [[ "$output" == *"should-survive"* ]]
  [[ "$output" == *"False"* ]]
}

@test "renderer: rejects manifest with non-string command" {
  seed_minimal
  cp "$SETTINGS" "$TEST_TMP/before.json"
  bad="$TEST_TMP/bad.json"
  cat > "$bad" <<'EOF'
{"version": 1, "hooks": [{"name": "x", "event": "Stop", "command": 42, "timeout": 1, "kill_switch": "BACK2BASE_HOOK_X", "handler": "command"}]}
EOF
  run python3 "$RENDERER" "$SETTINGS" --manifest "$bad"
  [ "$status" -eq 0 ]
  diff -q "$SETTINGS" "$TEST_TMP/before.json"
}

@test "renderer: rejects manifest with non-string matcher" {
  seed_minimal
  cp "$SETTINGS" "$TEST_TMP/before.json"
  bad="$TEST_TMP/bad.json"
  cat > "$bad" <<'EOF'
{"version": 1, "hooks": [{"name": "x", "event": "PostToolUse", "command": "/x", "timeout": 1, "kill_switch": "BACK2BASE_HOOK_X", "handler": "command", "matcher": 42}]}
EOF
  run python3 "$RENDERER" "$SETTINGS" --manifest "$bad"
  [ "$status" -eq 0 ]
  diff -q "$SETTINGS" "$TEST_TMP/before.json"
}

@test "renderer: rejects manifest with empty-string command" {
  seed_minimal
  cp "$SETTINGS" "$TEST_TMP/before.json"
  bad="$TEST_TMP/bad.json"
  cat > "$bad" <<'EOF'
{"version": 1, "hooks": [{"name": "x", "event": "Stop", "command": "", "timeout": 1, "kill_switch": "BACK2BASE_HOOK_X", "handler": "command"}]}
EOF
  run python3 "$RENDERER" "$SETTINGS" --manifest "$bad"
  [ "$status" -eq 0 ]
  diff -q "$SETTINGS" "$TEST_TMP/before.json"
}
