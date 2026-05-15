#!/usr/bin/env bats

# Tests for lib/render-overview.py — splices a pre-generated overview file
# into ~/.claude/CLAUDE.md between HTML-comment markers. Idempotent and
# atomic.

setup() {
  TEST_TMP="$(mktemp -d "${BATS_TMPDIR}/render-overview.XXXXXX")"
  export TEST_TMP
  REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  RENDERER="$REPO_ROOT/lib/render-overview.py"
}

teardown() {
  rm -rf "$TEST_TMP"
}

@test "appends section when no markers exist" {
  cat > "$TEST_TMP/CLAUDE.md" <<'EOF'
# Project header
Some intro text.
EOF
  echo "Body text" > "$TEST_TMP/overview.md"

  python3 "$RENDERER" "$TEST_TMP/overview.md" "$TEST_TMP/CLAUDE.md"

  grep -q '<!-- back2base:overview-begin -->' "$TEST_TMP/CLAUDE.md"
  grep -q '## Repo overview (this session)' "$TEST_TMP/CLAUDE.md"
  grep -q 'Body text' "$TEST_TMP/CLAUDE.md"
  grep -q '<!-- back2base:overview-end -->' "$TEST_TMP/CLAUDE.md"
  # Original content preserved
  grep -q '# Project header' "$TEST_TMP/CLAUDE.md"
}

@test "replaces section when markers already exist" {
  cat > "$TEST_TMP/CLAUDE.md" <<'EOF'
# Header
<!-- back2base:overview-begin -->
## Repo overview (this session)

OLD content
<!-- back2base:overview-end -->
Trailing text.
EOF
  echo "NEW content" > "$TEST_TMP/overview.md"

  python3 "$RENDERER" "$TEST_TMP/overview.md" "$TEST_TMP/CLAUDE.md"

  ! grep -q 'OLD content' "$TEST_TMP/CLAUDE.md"
  grep -q 'NEW content' "$TEST_TMP/CLAUDE.md"
  grep -q 'Trailing text.' "$TEST_TMP/CLAUDE.md"
  # Exactly one marker pair
  [ "$(grep -c '<!-- back2base:overview-begin -->' "$TEST_TMP/CLAUDE.md")" = "1" ]
  [ "$(grep -c '<!-- back2base:overview-end -->' "$TEST_TMP/CLAUDE.md")" = "1" ]
}

@test "idempotent: running twice with same input yields same file" {
  cat > "$TEST_TMP/CLAUDE.md" <<'EOF'
# Header
EOF
  echo "Same body" > "$TEST_TMP/overview.md"

  python3 "$RENDERER" "$TEST_TMP/overview.md" "$TEST_TMP/CLAUDE.md"
  cp "$TEST_TMP/CLAUDE.md" "$TEST_TMP/first.md"
  python3 "$RENDERER" "$TEST_TMP/overview.md" "$TEST_TMP/CLAUDE.md"

  diff "$TEST_TMP/first.md" "$TEST_TMP/CLAUDE.md"
}

@test "atomic write: no temp file leftovers" {
  cat > "$TEST_TMP/CLAUDE.md" <<'EOF'
# Header
EOF
  echo "Body" > "$TEST_TMP/overview.md"

  python3 "$RENDERER" "$TEST_TMP/overview.md" "$TEST_TMP/CLAUDE.md"

  # mkstemp creates files starting with `.CLAUDE.md.` — confirm none survive.
  [ -z "$(find "$TEST_TMP" -maxdepth 1 -name '.CLAUDE.md.*' -print -quit)" ]
}

@test "missing overview file errors non-zero" {
  echo "# Header" > "$TEST_TMP/CLAUDE.md"
  run python3 "$RENDERER" "$TEST_TMP/missing.md" "$TEST_TMP/CLAUDE.md"
  [ "$status" -ne 0 ]
}

@test "missing CLAUDE.md errors non-zero" {
  echo "Body" > "$TEST_TMP/overview.md"
  run python3 "$RENDERER" "$TEST_TMP/overview.md" "$TEST_TMP/missing.md"
  [ "$status" -ne 0 ]
}
