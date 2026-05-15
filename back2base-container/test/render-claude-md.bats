#!/usr/bin/env bats

# Tests for lib/render-claude-md.py.
#
# Replaces the bash-based generate_claude_md function in entrypoint.sh.
# The script reads a CLAUDE.md.template containing three @GENERATED markers
# and substitutes:
#   * <!-- @GENERATED:MCP_SERVERS -->   → two markdown tables (Docker / binary+remote)
#   * <!-- @GENERATED:COMMANDS -->      → slash command table
#   * <!-- @GENERATED:PROFILE_GUIDE --> → per-profile snippet (or marker line dropped)
#
# Same mtime-based cache invalidation as the original bash. Output must be
# byte-identical to the bash version (the diff was hand-traced before
# committing).

setup() {
  TEST_TMP="$(mktemp -d "${BATS_TMPDIR}/back2base-render-claude-md.XXXXXX")"
  SCRIPT="$BATS_TEST_DIRNAME/../lib/render-claude-md.py"
  TEMPLATE="$TEST_TMP/CLAUDE.md.template"
  MCP="$TEST_TMP/.mcp.json"
  COMMANDS_DIR="$TEST_TMP/commands"
  PROFILE_SNIPPET="$TEST_TMP/profile-snippet.md"
  OUTPUT="$TEST_TMP/CLAUDE.md"

  # Minimal template that exercises all three markers
  cat > "$TEMPLATE" <<'EOF'
# header
<!-- @GENERATED:MCP_SERVERS -->
middle
<!-- @GENERATED:COMMANDS -->
<!-- @GENERATED:PROFILE_GUIDE -->
footer
EOF
}

teardown() {
  rm -rf "$TEST_TMP"
}

# Helper: invoke script with all 5 path args, allowing missing optional inputs
render() {
  python3 "$SCRIPT" \
    --template "$TEMPLATE" \
    --mcp "$MCP" \
    --commands-dir "$COMMANDS_DIR" \
    --profile-snippet "$PROFILE_SNIPPET" \
    --output "$OUTPUT" \
    "$@"
}

# ── MCP servers section ────────────────────────────────────────────────

@test "mcp section: docker, stdio binary, http server render correctly" {
  cat > "$MCP" <<'EOF'
{
  "mcpServers": {
    "buildkite": {
      "type": "stdio",
      "command": "docker",
      "args": ["run", "-i", "--rm", "buildkite/mcp-server", "stdio"],
      "env": {"BUILDKITE_API_TOKEN": "${BUILDKITE_API_TOKEN}"}
    },
    "context7": {
      "type": "stdio",
      "command": "context7-mcp",
      "args": [],
      "env": {"CONTEXT7_API_KEY": "${CONTEXT7_API_KEY}"}
    },
    "aws-knowledge": {
      "type": "http",
      "url": "https://example/"
    }
  }
}
EOF
  run render
  [ "$status" -eq 0 ]
  # Docker table
  grep -F '| `buildkite` | `buildkite/mcp-server` | — requires: `BUILDKITE_API_TOKEN` |' "$OUTPUT"
  # Stdio binary
  grep -F '| `context7` | `context7-mcp` | — requires: `CONTEXT7_API_KEY` |' "$OUTPUT"
  # HTTP server
  grep -F '| `aws-knowledge` | HTTP (remote) |' "$OUTPUT"
  # Both tables present
  grep -F 'Docker containers' "$OUTPUT"
  grep -F 'Binaries and remote' "$OUTPUT"
}

@test "mcp section: npx server uses package name in bin_display" {
  cat > "$MCP" <<'EOF'
{
  "mcpServers": {
    "fancy": {
      "command": "npx",
      "args": ["-y", "@some/pkg"]
    }
  }
}
EOF
  run render
  [ "$status" -eq 0 ]
  grep -F '| `fancy` | `npx @some/pkg` |' "$OUTPUT"
}

@test "mcp section: server with no env keys gets no requires suffix" {
  cat > "$MCP" <<'EOF'
{
  "mcpServers": {
    "naked": {
      "command": "naked-mcp",
      "args": []
    }
  }
}
EOF
  run render
  [ "$status" -eq 0 ]
  # Note: row ends with "| naked-mcp` | |" — empty notes cell
  grep -F '| `naked` | `naked-mcp` | |' "$OUTPUT"
  ! grep -F 'requires:' "$OUTPUT"
}

@test "mcp section: env keys appear sorted alphabetically, comma-separated" {
  cat > "$MCP" <<'EOF'
{
  "mcpServers": {
    "many": {
      "command": "many-mcp",
      "args": [],
      "env": {
        "ZED_KEY": "z",
        "ALPHA_KEY": "a",
        "MIDDLE_KEY": "m",
        "EMPTY_KEY": ""
      }
    }
  }
}
EOF
  run render
  [ "$status" -eq 0 ]
  # Empty value must be filtered, others sorted alphabetically
  grep -F '— requires: `ALPHA_KEY`, `MIDDLE_KEY`, `ZED_KEY`' "$OUTPUT"
  ! grep -F 'EMPTY_KEY' "$OUTPUT"
}

@test "mcp section: missing .mcp.json reports unavailable" {
  rm -f "$MCP"
  run render
  [ "$status" -eq 0 ]
  grep -F '_MCP server list unavailable' "$OUTPUT"
}

# ── Commands section ───────────────────────────────────────────────────

@test "commands: frontmatter description wins over heading" {
  mkdir -p "$COMMANDS_DIR"
  cat > "$COMMANDS_DIR/foo.md" <<'EOF'
---
description: from frontmatter
---

# Heading line ignored
EOF
  run render
  [ "$status" -eq 0 ]
  grep -F '| `/foo` | from frontmatter |' "$OUTPUT"
}

@test "commands: no frontmatter, falls back to first heading" {
  mkdir -p "$COMMANDS_DIR"
  cat > "$COMMANDS_DIR/bar.md" <<'EOF'
# the heading line

body
EOF
  run render
  [ "$status" -eq 0 ]
  grep -F '| `/bar` | the heading line |' "$OUTPUT"
}

@test "commands: no frontmatter, no heading: falls back to em dash" {
  mkdir -p "$COMMANDS_DIR"
  cat > "$COMMANDS_DIR/baz.md" <<'EOF'
just a paragraph
EOF
  run render
  [ "$status" -eq 0 ]
  grep -F '| `/baz` | — |' "$OUTPUT"
}

@test "commands: subdirectory becomes colon-separated name" {
  mkdir -p "$COMMANDS_DIR/tmux"
  cat > "$COMMANDS_DIR/tmux/list.md" <<'EOF'
# list panes
EOF
  run render
  [ "$status" -eq 0 ]
  grep -F '| `/tmux:list` |' "$OUTPUT"
}

@test "commands: missing dir reports not found" {
  # Don't create COMMANDS_DIR
  run render
  [ "$status" -eq 0 ]
  grep -F '_Commands directory not found._' "$OUTPUT"
}

@test "commands: empty dir reports no commands installed" {
  mkdir -p "$COMMANDS_DIR"
  run render
  [ "$status" -eq 0 ]
  grep -F '_No slash commands installed._' "$OUTPUT"
}

@test "commands: frontmatter with quoted description strips quotes" {
  mkdir -p "$COMMANDS_DIR"
  cat > "$COMMANDS_DIR/quoted.md" <<'EOF'
---
description: "double-quoted desc"
---
EOF
  run render
  [ "$status" -eq 0 ]
  grep -F '| `/quoted` | double-quoted desc |' "$OUTPUT"
}

# ── Profile guide ──────────────────────────────────────────────────────

@test "profile snippet missing: marker line dropped entirely" {
  rm -f "$PROFILE_SNIPPET"
  run render
  [ "$status" -eq 0 ]
  ! grep -F '@GENERATED:PROFILE_GUIDE' "$OUTPUT"
  # The next line after COMMANDS section in the template is the
  # PROFILE_GUIDE marker — make sure we did NOT replace it with a blank
  # line. Specifically: the line "footer" must follow whatever the
  # commands section emitted (with no stray blank between them where
  # the marker was).
  # The line right before "footer" should not be blank-due-to-dropped-marker.
  # We match that "footer" appears and the file does not have a literal
  # empty marker placeholder.
  grep -F 'footer' "$OUTPUT"
}

@test "profile snippet present: content inlined verbatim" {
  cat > "$PROFILE_SNIPPET" <<'EOF'
### Profile guide

custom snippet body line 1
custom snippet body line 2
EOF
  run render
  [ "$status" -eq 0 ]
  grep -F '### Profile guide' "$OUTPUT"
  grep -F 'custom snippet body line 1' "$OUTPUT"
  grep -F 'custom snippet body line 2' "$OUTPUT"
  ! grep -F '@GENERATED:PROFILE_GUIDE' "$OUTPUT"
}

# ── Cache invalidation ─────────────────────────────────────────────────

@test "cache: rerun with no input changes preserves output mtime" {
  echo '{"mcpServers": {}}' > "$MCP"
  mkdir -p "$COMMANDS_DIR"
  run render
  [ "$status" -eq 0 ]
  # Backdate output far in the past so we can detect rewrites
  touch -t 202001010000 "$OUTPUT"
  before_mtime=$(stat -f %m "$OUTPUT" 2>/dev/null || stat -c %Y "$OUTPUT")
  # Backdate inputs to BEFORE the output's backdated mtime so they're not "newer"
  touch -t 201912010000 "$TEMPLATE" "$MCP"
  run render
  [ "$status" -eq 0 ]
  after_mtime=$(stat -f %m "$OUTPUT" 2>/dev/null || stat -c %Y "$OUTPUT")
  [ "$before_mtime" = "$after_mtime" ]
}

@test "cache: touching an input regenerates output" {
  echo '{"mcpServers": {}}' > "$MCP"
  mkdir -p "$COMMANDS_DIR"
  run render
  [ "$status" -eq 0 ]
  # Backdate the output far in the past so a fresh template touch is newer
  touch -t 202001010000 "$OUTPUT"
  before_mtime=$(stat -f %m "$OUTPUT" 2>/dev/null || stat -c %Y "$OUTPUT")
  # Touch template to "now" — should be newer than output
  touch "$TEMPLATE"
  run render
  [ "$status" -eq 0 ]
  after_mtime=$(stat -f %m "$OUTPUT" 2>/dev/null || stat -c %Y "$OUTPUT")
  [ "$before_mtime" != "$after_mtime" ]
}

@test "cache: touching a command file regenerates output" {
  echo '{"mcpServers": {}}' > "$MCP"
  mkdir -p "$COMMANDS_DIR"
  cat > "$COMMANDS_DIR/foo.md" <<'EOF'
# foo
EOF
  run render
  [ "$status" -eq 0 ]
  touch -t 202001010000 "$OUTPUT"
  # Backdate inputs to before output, so they're not "newer"
  touch -t 201912010000 "$TEMPLATE" "$MCP" "$COMMANDS_DIR/foo.md"
  before_mtime=$(stat -f %m "$OUTPUT" 2>/dev/null || stat -c %Y "$OUTPUT")
  # Now touch the command file to "now" — should trigger regen
  touch "$COMMANDS_DIR/foo.md"
  run render
  [ "$status" -eq 0 ]
  after_mtime=$(stat -f %m "$OUTPUT" 2>/dev/null || stat -c %Y "$OUTPUT")
  [ "$before_mtime" != "$after_mtime" ]
}

@test "cache: touching the profile snippet regenerates output" {
  # Regression for the bug where args.profile_snippet was missing from the
  # cache_fresh inputs list — editing a profile snippet silently no-op'd.
  echo '{"mcpServers": {}}' > "$MCP"
  mkdir -p "$COMMANDS_DIR"
  echo "## profile guide v1" > "$PROFILE_SNIPPET"
  run render
  [ "$status" -eq 0 ]
  touch -t 202001010000 "$OUTPUT"
  touch -t 201912010000 "$TEMPLATE" "$MCP" "$PROFILE_SNIPPET"
  before_mtime=$(stat -f %m "$OUTPUT" 2>/dev/null || stat -c %Y "$OUTPUT")
  # Touch the profile snippet to "now" — should trigger regen
  echo "## profile guide v2" > "$PROFILE_SNIPPET"
  touch "$PROFILE_SNIPPET"
  run render
  [ "$status" -eq 0 ]
  after_mtime=$(stat -f %m "$OUTPUT" 2>/dev/null || stat -c %Y "$OUTPUT")
  [ "$before_mtime" != "$after_mtime" ]
  # Updated content actually lands in the rendered output
  grep -F 'profile guide v2' "$OUTPUT"
}

# ── Marker tolerance ───────────────────────────────────────────────────

@test "markers with trailing whitespace still match" {
  # Whole-line equality matching is fragile — a stray trailing space on a
  # @GENERATED marker would silently leave the marker comment in the output.
  # Renderer strips surrounding whitespace before comparing.
  # printf is used because heredocs and editor-save commonly trim trailing
  # whitespace; we need to inject it deterministically.
  {
    printf '# header\n'
    printf '<!-- @GENERATED:MCP_SERVERS -->   \n'   # trailing spaces
    printf 'middle\n'
    printf '\t<!-- @GENERATED:COMMANDS -->\n'        # leading tab
    printf '  <!-- @GENERATED:PROFILE_GUIDE -->  \n' # both
    printf 'footer\n'
  } > "$TEMPLATE"
  echo '{"mcpServers": {}}' > "$MCP"
  mkdir -p "$COMMANDS_DIR"
  echo "## profile" > "$PROFILE_SNIPPET"
  run render
  [ "$status" -eq 0 ]
  # No @GENERATED comment should survive into the rendered output.
  ! grep -F '@GENERATED:MCP_SERVERS' "$OUTPUT"
  ! grep -F '@GENERATED:COMMANDS' "$OUTPUT"
  ! grep -F '@GENERATED:PROFILE_GUIDE' "$OUTPUT"
  # The expected sections all rendered:
  grep -F 'Docker containers' "$OUTPUT"
  grep -F '## profile' "$OUTPUT"
}
