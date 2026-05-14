#!/usr/bin/env bats

# Tests for lib/render-skill-preplan.py — fingerprints a workspace against
# each skill's declared `paths:` glob and splices a preplanned-skills block
# into a target CLAUDE.md.

setup() {
  TEST_TMP="$(mktemp -d "${BATS_TMPDIR}/render-skill-preplan.XXXXXX")"
  export TEST_TMP
  REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  RENDERER="$REPO_ROOT/lib/render-skill-preplan.py"

  WORKSPACE="$TEST_TMP/workspace"
  SKILLS="$TEST_TMP/skills"
  CLAUDE_MD="$TEST_TMP/CLAUDE.md"
  mkdir -p "$WORKSPACE" "$SKILLS"

  cat > "$CLAUDE_MD" <<'EOF'
# Project header
Some intro text.
EOF

  # Minimal skill fixtures with realistic frontmatter. Bodies kept short
  # so we can grep the splice output by recognizable strings.
  cat > "$SKILLS/docker-ops.md" <<'EOF'
---
name: docker-ops
description: "Docker containerization patterns. Use for: Dockerfile, docker-compose, container."
paths: "Dockerfile*,docker-compose*.yml,docker-compose*.yaml,.dockerignore"
---

# Docker Operations

Body text describing docker patterns.
EOF

  cat > "$SKILLS/modern-python.md" <<'EOF'
---
name: modern-python
description: "Modern Python with uv, ruff, pytest. Use for: pyproject.toml, .py, virtualenv."
paths: "**/*.py,pyproject.toml,setup.cfg,setup.py"
---

# Modern Python

Body text describing python tooling.
EOF

  cat > "$SKILLS/ci-cd-ops.md" <<'EOF'
---
name: ci-cd-ops
description: "CI/CD pipeline patterns. Use for: GitHub Actions, GitLab CI."
paths: ".github/workflows/*.yml,.github/workflows/*.yaml,.gitlab-ci.yml"
---

# CI/CD Operations

Body text describing CI patterns.
EOF

  # Skill with no `paths:` field — should be ignored entirely.
  cat > "$SKILLS/no-paths.md" <<'EOF'
---
name: no-paths
description: "A skill that doesn't declare paths."
---

# No Paths

This shouldn't appear in any output.
EOF
}

teardown() {
  rm -rf "$TEST_TMP"
}

# ─── Empty / no-match cases ──────────────────────────────────────────────

@test "empty workspace: no markers, CLAUDE.md unchanged" {
  cp "$CLAUDE_MD" "$TEST_TMP/before.md"

  python3 "$RENDERER" \
    --workspace "$WORKSPACE" \
    --skills-dir "$SKILLS" \
    --claude-md "$CLAUDE_MD"

  ! grep -q 'back2base:skill-preplan-begin' "$CLAUDE_MD"
  diff "$TEST_TMP/before.md" "$CLAUDE_MD"
}

@test "missing skills dir: exit 0, CLAUDE.md unchanged" {
  cp "$CLAUDE_MD" "$TEST_TMP/before.md"

  python3 "$RENDERER" \
    --workspace "$WORKSPACE" \
    --skills-dir "$TEST_TMP/nonexistent_skills" \
    --claude-md "$CLAUDE_MD"

  diff "$TEST_TMP/before.md" "$CLAUDE_MD"
}

@test "missing CLAUDE.md: exit 0 with warning" {
  run python3 "$RENDERER" \
    --workspace "$WORKSPACE" \
    --skills-dir "$SKILLS" \
    --claude-md "$TEST_TMP/nonexistent.md"

  [ "$status" = "0" ]
  echo "$output" | grep -q 'CLAUDE.md missing'
}

@test "missing workspace: exit 0 with warning" {
  run python3 "$RENDERER" \
    --workspace "$TEST_TMP/nonexistent_workspace" \
    --skills-dir "$SKILLS" \
    --claude-md "$CLAUDE_MD"

  [ "$status" = "0" ]
  echo "$output" | grep -q 'workspace missing'
}

# ─── High-tier (root-level match) cases ──────────────────────────────────

@test "root-level Dockerfile: docker-ops at high tier with full body" {
  touch "$WORKSPACE/Dockerfile"

  python3 "$RENDERER" \
    --workspace "$WORKSPACE" \
    --skills-dir "$SKILLS" \
    --claude-md "$CLAUDE_MD"

  grep -q 'back2base:skill-preplan-begin' "$CLAUDE_MD"
  grep -q 'back2base:skill-preplan-end' "$CLAUDE_MD"
  grep -q 'Pre-loaded — apply without invoking the Skill tool' "$CLAUDE_MD"
  grep -q '\*\*Skill: docker-ops\*\*' "$CLAUDE_MD"
  grep -q 'Body text describing docker patterns' "$CLAUDE_MD"
  # Other skills with no matches should not appear.
  ! grep -q '\*\*Skill: modern-python\*\*' "$CLAUDE_MD"
  ! grep -q '\*\*Skill: ci-cd-ops\*\*' "$CLAUDE_MD"
  # Original content preserved.
  grep -q '# Project header' "$CLAUDE_MD"
}

@test "pyproject.toml at root: modern-python at high tier" {
  touch "$WORKSPACE/pyproject.toml"

  python3 "$RENDERER" \
    --workspace "$WORKSPACE" \
    --skills-dir "$SKILLS" \
    --claude-md "$CLAUDE_MD"

  grep -q '\*\*Skill: modern-python\*\*' "$CLAUDE_MD"
  grep -q 'Body text describing python tooling' "$CLAUDE_MD"
}

@test "github workflows present: ci-cd-ops detected" {
  mkdir -p "$WORKSPACE/.github/workflows"
  touch "$WORKSPACE/.github/workflows/test.yml"

  python3 "$RENDERER" \
    --workspace "$WORKSPACE" \
    --skills-dir "$SKILLS" \
    --claude-md "$CLAUDE_MD"

  grep -q 'ci-cd-ops' "$CLAUDE_MD"
}

# ─── Tier capping ────────────────────────────────────────────────────────

@test "three+ root-match skills: only top 2 at high tier, third demoted to medium" {
  touch "$WORKSPACE/Dockerfile"
  touch "$WORKSPACE/pyproject.toml"
  mkdir -p "$WORKSPACE/.github/workflows"
  touch "$WORKSPACE/.github/workflows/ci.yml"

  python3 "$RENDERER" \
    --workspace "$WORKSPACE" \
    --skills-dir "$SKILLS" \
    --claude-md "$CLAUDE_MD"

  # Two skills got full bodies inlined. (Sorted by name within equal score:
  # ci-cd-ops, docker-ops come before modern-python alphabetically — but
  # selection order is by match count desc, then root-match, then name.
  # All three have count=1 + root_match=true, so name asc breaks ties.)
  local body_count
  body_count=$(grep -c '^\*\*Skill: ' "$CLAUDE_MD")
  [ "$body_count" -eq 2 ]

  # Third skill appears as pointer.
  grep -q 'Pointers — invoke via Skill tool when needed' "$CLAUDE_MD"
}

@test "skill without paths frontmatter is silently skipped" {
  touch "$WORKSPACE/Dockerfile"

  python3 "$RENDERER" \
    --workspace "$WORKSPACE" \
    --skills-dir "$SKILLS" \
    --claude-md "$CLAUDE_MD"

  ! grep -q 'no-paths' "$CLAUDE_MD"
}

# ─── Idempotency / atomicity ─────────────────────────────────────────────

@test "idempotent: running twice with same workspace yields same file" {
  touch "$WORKSPACE/Dockerfile"

  python3 "$RENDERER" \
    --workspace "$WORKSPACE" \
    --skills-dir "$SKILLS" \
    --claude-md "$CLAUDE_MD"
  cp "$CLAUDE_MD" "$TEST_TMP/first.md"

  python3 "$RENDERER" \
    --workspace "$WORKSPACE" \
    --skills-dir "$SKILLS" \
    --claude-md "$CLAUDE_MD"

  diff "$TEST_TMP/first.md" "$CLAUDE_MD"
}

@test "re-run with different workspace replaces block, no duplication" {
  # First run with Dockerfile.
  touch "$WORKSPACE/Dockerfile"
  python3 "$RENDERER" \
    --workspace "$WORKSPACE" \
    --skills-dir "$SKILLS" \
    --claude-md "$CLAUDE_MD"
  grep -q 'docker-ops' "$CLAUDE_MD"

  # Second run with workspace switched to pyproject.toml only.
  rm "$WORKSPACE/Dockerfile"
  touch "$WORKSPACE/pyproject.toml"
  python3 "$RENDERER" \
    --workspace "$WORKSPACE" \
    --skills-dir "$SKILLS" \
    --claude-md "$CLAUDE_MD"

  # Old skill body gone, new one in.
  ! grep -q 'docker-ops' "$CLAUDE_MD"
  grep -q 'modern-python' "$CLAUDE_MD"
  # Exactly one marker pair.
  [ "$(grep -c 'back2base:skill-preplan-begin' "$CLAUDE_MD")" = "1" ]
  [ "$(grep -c 'back2base:skill-preplan-end' "$CLAUDE_MD")" = "1" ]
}

@test "workspace match disappears: existing block is removed" {
  touch "$WORKSPACE/Dockerfile"
  python3 "$RENDERER" \
    --workspace "$WORKSPACE" \
    --skills-dir "$SKILLS" \
    --claude-md "$CLAUDE_MD"
  grep -q 'back2base:skill-preplan-begin' "$CLAUDE_MD"

  # Remove the trigger; re-run.
  rm "$WORKSPACE/Dockerfile"
  python3 "$RENDERER" \
    --workspace "$WORKSPACE" \
    --skills-dir "$SKILLS" \
    --claude-md "$CLAUDE_MD"

  ! grep -q 'back2base:skill-preplan-begin' "$CLAUDE_MD"
  ! grep -q 'back2base:skill-preplan-end' "$CLAUDE_MD"
  # Header preserved.
  grep -q '# Project header' "$CLAUDE_MD"
}

@test "atomic write: no .CLAUDE.md.* tempfile leftovers" {
  touch "$WORKSPACE/Dockerfile"

  python3 "$RENDERER" \
    --workspace "$WORKSPACE" \
    --skills-dir "$SKILLS" \
    --claude-md "$CLAUDE_MD"

  # mkstemp creates files starting with `.CLAUDE.md.` — none should survive.
  [ -z "$(find "$TEST_TMP" -maxdepth 1 -name '.CLAUDE.md.*' -print -quit)" ]
}

# ─── Pruning ─────────────────────────────────────────────────────────────

@test "matches inside node_modules are pruned" {
  mkdir -p "$WORKSPACE/node_modules/some-pkg"
  touch "$WORKSPACE/node_modules/some-pkg/setup.py"

  python3 "$RENDERER" \
    --workspace "$WORKSPACE" \
    --skills-dir "$SKILLS" \
    --claude-md "$CLAUDE_MD"

  # Only node_modules content present — modern-python should NOT trigger.
  ! grep -q 'modern-python' "$CLAUDE_MD"
}

# ─── Non-root single match → medium tier ────────────────────────────────

@test "single non-root .py match: modern-python at medium tier (pointer only)" {
  mkdir -p "$WORKSPACE/src"
  touch "$WORKSPACE/src/lonely.py"

  python3 "$RENDERER" \
    --workspace "$WORKSPACE" \
    --skills-dir "$SKILLS" \
    --claude-md "$CLAUDE_MD"

  # Pointer section present, no inlined body.
  grep -q 'Pointers — invoke via Skill tool' "$CLAUDE_MD"
  grep -q '`modern-python`' "$CLAUDE_MD"
  ! grep -q '\*\*Skill: modern-python\*\*' "$CLAUDE_MD"
}
