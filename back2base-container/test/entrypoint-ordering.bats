#!/usr/bin/env bats

# Tests for the cold-start phase ordering in entrypoint.sh.
#
# These tests are structural — they grep for the _phase invocations and
# verify their order matches the spec. They don't exec entrypoint.sh
# end-to-end (it shells out to sudo, docker, python3, etc.).

setup() {
  REPO_DIR="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  ENTRYPOINT="$REPO_DIR/entrypoint.sh"
}

phase_line() {
  # Print the line number of the first _phase call whose title contains
  # the given fragment, or empty if not found.
  grep -nE "_phase[[:space:]]+\"[^\"]*$1[^\"]*\"" "$ENTRYPOINT" | head -1 | cut -d: -f1
}

@test "entrypoint sources lib/status.sh" {
  grep -qE 'lib/status\.sh|/opt/back2base/status\.sh' "$ENTRYPOINT"
}

@test "entrypoint defines _phase wrapper" {
  grep -qE '^_phase\(\)' "$ENTRYPOINT"
}

@test "entrypoint defines _FANCY_FAILED state" {
  grep -qE '^_FANCY_FAILED=' "$ENTRYPOINT"
}

@test "phase 1 is Initializing" {
  [ -n "$(phase_line 'Initializing')" ]
}

@test "phase 2 is Setting up workspace" {
  [ -n "$(phase_line 'Setting up workspace')" ]
}

@test "phase 2.5 is Seeding settings" {
  [ -n "$(phase_line 'Seeding settings')" ]
}

@test "phase 3 is Filtering MCP profile" {
  [ -n "$(phase_line 'Filtering MCP profile')" ]
}

@test "phase 4 is Pulling MCP server images" {
  [ -n "$(phase_line 'Pulling MCP server images')" ]
}

@test "phase 5 is Seeding image defaults" {
  [ -n "$(phase_line 'Seeding image defaults')" ]
}

@test "phase 6 is Generating CLAUDE.md" {
  [ -n "$(phase_line 'Generating CLAUDE.md')" ]
}

@test "phase 7 is Generating repo overview (when enabled)" {
  [ -n "$(phase_line 'Generating repo overview')" ]
}

@test "phase 8 is Starting daemons" {
  [ -n "$(phase_line 'Starting daemons')" ]
}

@test "phases appear in spec order" {
  p1=$(phase_line 'Initializing')
  p2=$(phase_line 'Setting up workspace')
  p2_5=$(phase_line 'Seeding settings')
  p3=$(phase_line 'Filtering MCP profile')
  p4=$(phase_line 'Pulling MCP server images')
  p5=$(phase_line 'Seeding image defaults')
  p6=$(phase_line 'Generating CLAUDE.md')
  p7=$(phase_line 'Generating repo overview')
  p8=$(phase_line 'Starting daemons')
  [ "$p1" -lt "$p2" ]
  [ "$p2" -lt "$p2_5" ]
  [ "$p2_5" -lt "$p3" ]
  [ "$p3" -lt "$p4" ]
  [ "$p4" -lt "$p5" ]
  [ "$p5" -lt "$p6" ]
  [ "$p6" -lt "$p7" ]
  [ "$p7" -lt "$p8" ]
}

@test "phase 7 is gated on BACK2BASE_OVERVIEW=1" {
  # The _phase call for "Generating repo overview" must be inside an `if`
  # branch on BACK2BASE_OVERVIEW.
  awk '/BACK2BASE_OVERVIEW/,/fi/' "$ENTRYPOINT" | grep -q 'Generating repo overview'
}

@test "phase 8 immediately precedes exec" {
  # Find the _phase 'Starting daemons' line and the `exec "$@"` line; the
  # exec must come after, and there should be no other _phase calls between.
  daemons_line=$(phase_line 'Starting daemons')
  exec_line=$(grep -nE '^exec[[:space:]]+"\$@"' "$ENTRYPOINT" | head -1 | cut -d: -f1)
  [ -n "$daemons_line" ]
  [ -n "$exec_line" ]
  [ "$daemons_line" -lt "$exec_line" ]
  # No other _phase calls between daemons and exec
  between=$(awk -v start="$daemons_line" -v end="$exec_line" 'NR>start && NR<end' "$ENTRYPOINT" | grep -cE '_phase[[:space:]]+"' || true)
  [ "$between" = "0" ]
}
