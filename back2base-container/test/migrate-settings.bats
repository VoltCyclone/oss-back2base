#!/usr/bin/env bats

# Tests for lib/migrate-settings.py.
#
# The migrator stamps a top-level `_back2base_schema` field, runs every
# numbered migration whose number is greater than the current schema, and
# updates the schema to the highest applied number. Migrations registered:
#
#   1. matcher+hooks envelope: bare {type: command, ...} entries under
#      hooks.<event>[i] get wrapped into {matcher: "", hooks: [...]}.
#   2. prune defunct /opt/back2base/memory-sessionstart-hook.sh registration.
#   3. seed statusLine block.
#   4. strip hooks block (render-hooks.py owns it from schema 4 on).
#
# The script must be idempotent (re-running is a no-op) and tolerant of
# malformed input (silently exits 0).

setup() {
  TEST_TMP="$(mktemp -d "${BATS_TMPDIR}/back2base-migrate-settings.XXXXXX")"
  SETTINGS="$TEST_TMP/settings.json"
  SCRIPT="$BATS_TEST_DIRNAME/../lib/migrate-settings.py"
  CURRENT_SCHEMA=4
}

teardown() {
  rm -rf "$TEST_TMP"
}

# ── Fresh / minimal files ──────────────────────────────────────────────

@test "missing settings.json: exits 0 silently" {
  run python3 "$SCRIPT" "$TEST_TMP/does-not-exist.json"
  [ "$status" -eq 0 ]
}

@test "fresh empty-object file gets stamped to current schema" {
  echo '{}' > "$SETTINGS"
  run python3 "$SCRIPT" "$SETTINGS"
  [ "$status" -eq 0 ]
  schema=$(python3 -c "import json,sys; print(json.load(open('$SETTINGS')).get('_back2base_schema'))")
  [ "$schema" = "$CURRENT_SCHEMA" ]
}

@test "malformed JSON: exits 0 and leaves file alone" {
  echo 'not valid json {' > "$SETTINGS"
  before=$(cat "$SETTINGS")
  run python3 "$SCRIPT" "$SETTINGS"
  [ "$status" -eq 0 ]
  after=$(cat "$SETTINGS")
  [ "$before" = "$after" ]
}

# ── Schema 0 → migration 1 (envelope wrapping) ─────────────────────────

@test "schema 0 with bare command hook: gets wrapped in envelope then stripped by m4" {
  cat > "$SETTINGS" <<'EOF'
{
  "hooks": {
    "UserPromptSubmit": [
      {"type": "command", "command": "/foo/bar.sh", "timeout": 10}
    ]
  }
}
EOF
  run python3 "$SCRIPT" "$SETTINGS"
  [ "$status" -eq 0 ]
  # m1 wraps, then m4 strips the whole hooks block — renderer owns it now.
  has_hooks=$(python3 -c "import json; d=json.load(open('$SETTINGS')); print('hooks' in d)")
  [ "$has_hooks" = "False" ]
  schema=$(python3 -c "import json; print(json.load(open('$SETTINGS')).get('_back2base_schema'))")
  [ "$schema" = "$CURRENT_SCHEMA" ]
}

# ── Schema 0 → migration 2 (prune defunct hook) ────────────────────────

@test "schema 0 with defunct SessionStart hook: gets pruned then hooks stripped by m4" {
  cat > "$SETTINGS" <<'EOF'
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "",
        "hooks": [
          {"type": "command", "command": "/opt/back2base/memory-sessionstart-hook.sh"}
        ]
      }
    ]
  }
}
EOF
  run python3 "$SCRIPT" "$SETTINGS"
  [ "$status" -eq 0 ]
  # m2 prunes SessionStart, m4 strips the whole hooks block
  has_hooks=$(python3 -c "import json; d=json.load(open('$SETTINGS')); print('hooks' in d)")
  [ "$has_hooks" = "False" ]
  schema=$(python3 -c "import json; print(json.load(open('$SETTINGS')).get('_back2base_schema'))")
  [ "$schema" = "$CURRENT_SCHEMA" ]
}

@test "schema 0 with both legacy issues: all four migrations apply" {
  cat > "$SETTINGS" <<'EOF'
{
  "hooks": {
    "UserPromptSubmit": [
      {"type": "command", "command": "/foo.sh"}
    ],
    "SessionStart": [
      {
        "matcher": "",
        "hooks": [
          {"type": "command", "command": "/opt/back2base/memory-sessionstart-hook.sh"}
        ]
      }
    ]
  }
}
EOF
  run python3 "$SCRIPT" "$SETTINGS"
  [ "$status" -eq 0 ]
  # m4 strips the whole hooks block (m1 wrapped, m2 pruned SessionStart, then m4 strips)
  has_hooks=$(python3 -c "import json; d=json.load(open('$SETTINGS')); print('hooks' in d)")
  [ "$has_hooks" = "False" ]
  schema=$(python3 -c "import json; print(json.load(open('$SETTINGS')).get('_back2base_schema'))")
  [ "$schema" = "$CURRENT_SCHEMA" ]
}

# ── Schema 1 → only migration 2 runs ───────────────────────────────────

@test "schema 1 with bare hook: m1 NOT re-run, m2+m3+m4 apply" {
  cat > "$SETTINGS" <<'EOF'
{
  "_back2base_schema": 1,
  "hooks": {
    "UserPromptSubmit": [
      {"type": "command", "command": "/foo.sh"}
    ],
    "SessionStart": [
      {
        "matcher": "",
        "hooks": [
          {"type": "command", "command": "/opt/back2base/memory-sessionstart-hook.sh"}
        ]
      }
    ]
  }
}
EOF
  run python3 "$SCRIPT" "$SETTINGS"
  [ "$status" -eq 0 ]
  # m4 strips the whole hooks block (m2 pruned SessionStart, then m4 strips everything)
  has_hooks=$(python3 -c "import json; d=json.load(open('$SETTINGS')); print('hooks' in d)")
  [ "$has_hooks" = "False" ]
  # m3 added statusLine
  has_statusline=$(python3 -c "import json; d=json.load(open('$SETTINGS')); print('statusLine' in d)")
  [ "$has_statusline" = "True" ]
  schema=$(python3 -c "import json; print(json.load(open('$SETTINGS')).get('_back2base_schema'))")
  [ "$schema" = "$CURRENT_SCHEMA" ]
}

# ── Idempotency ────────────────────────────────────────────────────────

@test "idempotent: running twice produces the same content" {
  cat > "$SETTINGS" <<'EOF'
{
  "hooks": {
    "UserPromptSubmit": [
      {"type": "command", "command": "/foo.sh"}
    ]
  }
}
EOF
  python3 "$SCRIPT" "$SETTINGS"
  first=$(cat "$SETTINGS")
  python3 "$SCRIPT" "$SETTINGS"
  second=$(cat "$SETTINGS")
  [ "$first" = "$second" ]
}

@test "already-stamped at current schema: file untouched (mtime preserved)" {
  cat > "$SETTINGS" <<EOF
{
  "_back2base_schema": $CURRENT_SCHEMA,
  "hooks": {
    "UserPromptSubmit": [
      {"matcher": "", "hooks": [{"type": "command", "command": "/foo.sh"}]}
    ]
  }
}
EOF
  # Backdate the file so we can detect an mtime change
  touch -t 202001010000 "$SETTINGS"
  before_mtime=$(stat -f %m "$SETTINGS" 2>/dev/null || stat -c %Y "$SETTINGS")
  run python3 "$SCRIPT" "$SETTINGS"
  [ "$status" -eq 0 ]
  after_mtime=$(stat -f %m "$SETTINGS" 2>/dev/null || stat -c %Y "$SETTINGS")
  [ "$before_mtime" = "$after_mtime" ]
}

@test "schema higher than current: not downgraded, file untouched" {
  cat > "$SETTINGS" <<'EOF'
{
  "_back2base_schema": 99,
  "hooks": {}
}
EOF
  touch -t 202001010000 "$SETTINGS"
  before_mtime=$(stat -f %m "$SETTINGS" 2>/dev/null || stat -c %Y "$SETTINGS")
  run python3 "$SCRIPT" "$SETTINGS"
  [ "$status" -eq 0 ]
  after_mtime=$(stat -f %m "$SETTINGS" 2>/dev/null || stat -c %Y "$SETTINGS")
  [ "$before_mtime" = "$after_mtime" ]
  schema=$(python3 -c "import json; print(json.load(open('$SETTINGS')).get('_back2base_schema'))")
  [ "$schema" = "99" ]
}

# ── Migration 3: add statusLine block ──────────────────────────────────

@test "migration 3 adds statusLine when missing" {
  cat > "$SETTINGS" <<'JSON'
{"_back2base_schema": 2, "permissions": {"defaultMode": "default"}}
JSON
  run python3 "$SCRIPT" "$SETTINGS"
  [ "$status" -eq 0 ]
  python3 -c "import json,sys; d=json.load(open('$SETTINGS')); assert d['statusLine']['type']=='command', d; assert d['statusLine']['command']=='/opt/back2base/statusline.sh', d; assert d['_back2base_schema']>=3, d"
}

@test "migration 3 preserves existing statusLine" {
  cat > "$SETTINGS" <<'JSON'
{"_back2base_schema": 2, "statusLine": {"type": "command", "command": "/usr/local/bin/my-custom-line"}}
JSON
  run python3 "$SCRIPT" "$SETTINGS"
  [ "$status" -eq 0 ]
  python3 -c "import json; d=json.load(open('$SETTINGS')); assert d['statusLine']['command']=='/usr/local/bin/my-custom-line', d; assert d['_back2base_schema']>=3, d"
}
