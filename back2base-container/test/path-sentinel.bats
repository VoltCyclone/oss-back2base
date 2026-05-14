#!/usr/bin/env bats

# Tests for lib/path-sentinel.sh.
#
# The sentinel verifies (after a delay) that Claude Code's project-dir naming
# convention still matches our assumption (PWD with '/' replaced by '-'). The
# script supports a --dry-run mode that skips the sleep and emits PASS / FAIL
# / NOOP based on a fixture --projects-dir and --cwd.

setup() {
  TEST_TMP="$(mktemp -d "${BATS_TMPDIR}/path-sentinel.XXXXXX")"
  export HOME="$TEST_TMP/home"
  mkdir -p "$HOME/.claude"
  SCRIPT="$BATS_TEST_DIRNAME/../lib/path-sentinel.sh"
}

teardown() {
  rm -rf "$TEST_TMP"
}

@test "script is executable" {
  [ -x "$SCRIPT" ]
}

@test "script has a bash shebang" {
  run head -n1 "$SCRIPT"
  [ "$status" -eq 0 ]
  [[ "$output" == "#!"*"bash"* ]]
}

@test "dry-run reports PASS when expected dir exists" {
  projects="$TEST_TMP/projects"
  mkdir -p "$projects/-workspace/memory"

  run "$SCRIPT" --dry-run --projects-dir "$projects" --cwd /workspace
  [ "$status" -eq 0 ]
  [[ "$output" == *"PASS"* ]]
}

@test "dry-run reports FAIL when expected dir is absent but other dir has jsonl" {
  projects="$TEST_TMP/projects"
  mkdir -p "$projects/_workspace_hashed"
  : > "$projects/_workspace_hashed/abc-123.jsonl"

  run "$SCRIPT" --dry-run --projects-dir "$projects" --cwd /workspace
  [ "$status" -eq 0 ]
  [[ "$output" == *"FAIL"* ]]
  [[ "$output" == *"_workspace_hashed"* ]]
}

@test "dry-run reports NOOP when projects dir is empty" {
  projects="$TEST_TMP/projects"
  mkdir -p "$projects"

  run "$SCRIPT" --dry-run --projects-dir "$projects" --cwd /workspace
  [ "$status" -eq 0 ]
  [[ "$output" == *"NOOP"* ]]
}

@test "dry-run NOOP when projects dir does not exist" {
  projects="$TEST_TMP/nope"

  run "$SCRIPT" --dry-run --projects-dir "$projects" --cwd /workspace
  [ "$status" -eq 0 ]
  [[ "$output" == *"NOOP"* ]]
}

@test "dry-run NOOP when other dirs exist but contain no jsonl" {
  # Other dirs without jsonl files imply Claude Code hasn't written yet
  # (e.g. directories left behind by something else). Don't false-positive.
  projects="$TEST_TMP/projects"
  mkdir -p "$projects/leftover"
  : > "$projects/leftover/notes.txt"

  run "$SCRIPT" --dry-run --projects-dir "$projects" --cwd /workspace
  [ "$status" -eq 0 ]
  [[ "$output" == *"NOOP"* ]]
}

@test "dry-run PASS accepts a symlink at the expected dir name" {
  projects="$TEST_TMP/projects"
  real="$TEST_TMP/real"
  mkdir -p "$real" "$projects"
  ln -s "$real" "$projects/-workspace"

  run "$SCRIPT" --dry-run --projects-dir "$projects" --cwd /workspace
  [ "$status" -eq 0 ]
  [[ "$output" == *"PASS"* ]]
}

@test "dry-run --cwd /repos/myrepo derives -repos-myrepo" {
  projects="$TEST_TMP/projects"
  mkdir -p "$projects/-repos-myrepo/memory"

  run "$SCRIPT" --dry-run --projects-dir "$projects" --cwd /repos/myrepo
  [ "$status" -eq 0 ]
  [[ "$output" == *"PASS"* ]]
}

@test "FAIL writes mismatch sentinel file" {
  projects="$TEST_TMP/projects"
  mkdir -p "$projects/_workspace_hashed"
  : > "$projects/_workspace_hashed/abc-123.jsonl"

  run "$SCRIPT" --dry-run --projects-dir "$projects" --cwd /workspace
  [ "$status" -eq 0 ]
  [ -f "$HOME/.claude/.path-sentinel-mismatch" ]
  run cat "$HOME/.claude/.path-sentinel-mismatch"
  [[ "$output" == *"-workspace"* ]]
  [[ "$output" == *"_workspace_hashed"* ]]
}

@test "PASS writes ok marker file" {
  projects="$TEST_TMP/projects"
  mkdir -p "$projects/-workspace/memory"

  run "$SCRIPT" --dry-run --projects-dir "$projects" --cwd /workspace
  [ "$status" -eq 0 ]
  [ -f "$HOME/.claude/.path-sentinel-ok" ]
}
