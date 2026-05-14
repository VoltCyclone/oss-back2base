#!/usr/bin/env bats

# Tests for lib/session-snapshot.sh.
#
# The daemon is sourced (not exec'd) so its functions can be called directly
# with controlled inputs. main_loop is gated by a BASH_SOURCE check and does
# not run when sourced.
#
# Required tools on host: bash 4+, jq, GNU coreutils-ish date/stat OR macOS
# BSD equivalents — the daemon detects which is available.

setup() {
  TEST_TMP="$(mktemp -d "${BATS_TMPDIR}/session-snapshot.XXXXXX")"
  export HOME="$TEST_TMP/home"
  mkdir -p "$HOME/.claude"
  export MEMORY_NAMESPACE="ws"
  export SNAPSHOT_INTERVAL_SEC=1
  export ACTIVE_WINDOW_SEC=600
  export SNAPSHOT_KEEP=5
  export SESSION_COLD_DAYS=30
  source "$BATS_TEST_DIRNAME/../lib/session-snapshot.sh"
}

teardown() {
  rm -rf "$TEST_TMP"
}

# Helper: create a session dir with a valid live JSONL.
mksession() {
  local id="$1" content="$2"
  local sd="$HOME/.claude/projects/$MEMORY_NAMESPACE/$id"
  mkdir -p "$sd"
  printf '%s\n' "$content" > "$sd/$id.jsonl"
  echo "$sd"
}

@test "snapshot_session creates a snapshot for a valid live JSONL" {
  sd=$(mksession "s1" '{"k":"v"}')
  snapshot_session "$sd"
  run bash -c "ls $sd/.snapshots/*.jsonl 2>/dev/null | wc -l"
  [ "$status" -eq 0 ]
  [ "$output" -eq 1 ]
}

@test "snapshot_session skips a corrupt live JSONL" {
  sd=$(mksession "s2" '{"good":1}')
  printf '%s\n' '{garbage}' > "$sd/s2.jsonl"
  snapshot_session "$sd"
  run bash -c "ls $sd/.snapshots/*.jsonl 2>/dev/null | wc -l"
  [ "$output" -eq 0 ]
}

@test "snapshot_session dedups when content unchanged" {
  sd=$(mksession "s3" '{"k":"v"}')
  snapshot_session "$sd"
  sleep 1   # ensure timestamp would differ
  snapshot_session "$sd"
  run bash -c "ls $sd/.snapshots/*.jsonl 2>/dev/null | wc -l"
  [ "$output" -eq 1 ]
}

@test "snapshot_session creates a new snapshot when content changes" {
  sd=$(mksession "s4" '{"k":"v"}')
  snapshot_session "$sd"
  sleep 1
  printf '%s\n' '{"k":"v2"}' > "$sd/s4.jsonl"
  snapshot_session "$sd"
  run bash -c "ls $sd/.snapshots/*.jsonl 2>/dev/null | wc -l"
  [ "$output" -eq 2 ]
}

@test "snapshot_session retains only last K snapshots" {
  export SNAPSHOT_KEEP=3
  # Re-source so KEEP picks up the new value (the daemon reads env at source time).
  source "$BATS_TEST_DIRNAME/../lib/session-snapshot.sh"
  sd=$(mksession "s5" '{"k":"v0"}')
  for i in 1 2 3 4 5; do
    printf '%s\n' "{\"k\":\"v$i\"}" > "$sd/s5.jsonl"
    snapshot_session "$sd"
    sleep 1
  done
  run bash -c "ls $sd/.snapshots/*.jsonl 2>/dev/null | wc -l"
  [ "$output" -eq 3 ]
}

@test "gc_cold_sessions removes .snapshots for sessions older than COLD_DAYS" {
  export SESSION_COLD_DAYS=30
  source "$BATS_TEST_DIRNAME/../lib/session-snapshot.sh"
  sd=$(mksession "cold" '{"k":"v"}')
  mkdir -p "$sd/.snapshots"
  touch "$sd/.snapshots/old.jsonl"
  # Backdate live JSONL well past COLD_DAYS — Jan 1, 2000.
  touch -t 200001011200 "$sd/cold.jsonl"
  ns_dir="$HOME/.claude/projects/$MEMORY_NAMESPACE"
  gc_cold_sessions "$ns_dir"
  [ ! -d "$sd/.snapshots" ]
}

@test "gc_cold_sessions leaves recent sessions alone" {
  sd=$(mksession "warm" '{"k":"v"}')
  mkdir -p "$sd/.snapshots"
  touch "$sd/.snapshots/new.jsonl"
  ns_dir="$HOME/.claude/projects/$MEMORY_NAMESPACE"
  gc_cold_sessions "$ns_dir"
  [ -d "$sd/.snapshots" ]
  [ -f "$sd/.snapshots/new.jsonl" ]
}

@test "main_loop snapshots only sessions whose JSONL is within ACTIVE_WINDOW_SEC" {
  export ACTIVE_WINDOW_SEC=60
  source "$BATS_TEST_DIRNAME/../lib/session-snapshot.sh"
  hot=$(mksession "hot" '{"k":"hot"}')
  cold=$(mksession "cold" '{"k":"cold"}')
  # Backdate cold's live JSONL ~10 minutes via touch -t (portable).
  # touch -t interprets timestamps in local time, so use local date (no -u).
  past=$(date -v-10M +%Y%m%d%H%M 2>/dev/null || date --date='10 minutes ago' +%Y%m%d%H%M)
  touch -t "$past" "$cold/cold.jsonl"

  # One-shot mode: run the per-tick body once, then exit.
  ONE_SHOT=1 main_loop

  hot_count=$(ls "$hot/.snapshots"/*.jsonl 2>/dev/null | wc -l | tr -d ' ')
  cold_count=$(ls "$cold/.snapshots"/*.jsonl 2>/dev/null | wc -l | tr -d ' ')
  [ "$hot_count" -eq 1 ]
  [ "$cold_count" -eq 0 ]
}
