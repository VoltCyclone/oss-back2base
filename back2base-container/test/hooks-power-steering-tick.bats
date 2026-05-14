#!/usr/bin/env bats

setup() {
  TEST_TMP="$(mktemp -d "${BATS_TMPDIR}/back2base-tick.XXXXXX")"
  export HOME="$TEST_TMP/home"
  mkdir -p "$HOME/.claude"
  HOOK="$BATS_TEST_DIRNAME/../lib/hooks/power-steering-tick.sh"
  COMMON="$BATS_TEST_DIRNAME/../lib/hooks/_common.sh"
  # Redirect the tick into the test tmp dir so tests don't need
  # /run write access.
  export POWER_STEERING_TICK="$TEST_TMP/runtime/.tick"
}

teardown() { rm -rf "$TEST_TMP"; }

@test "tick: touches \$POWER_STEERING_TICK" {
  run env BACK2BASE_HOOKS_LIB="$COMMON" POWER_STEERING_TICK="$POWER_STEERING_TICK" bash "$HOOK" </dev/null
  [ "$status" -eq 0 ]
  [ -f "$POWER_STEERING_TICK" ]
}

@test "tick: emits {} (passthrough envelope)" {
  run env BACK2BASE_HOOKS_LIB="$COMMON" POWER_STEERING_TICK="$POWER_STEERING_TICK" bash "$HOOK" </dev/null
  [ "$status" -eq 0 ]
  [ "$output" = "{}" ]
}

@test "tick: BACK2BASE_HOOK_POWER_STEERING_TICK=off does not write the tick file" {
  run env BACK2BASE_HOOKS_LIB="$COMMON" POWER_STEERING_TICK="$POWER_STEERING_TICK" BACK2BASE_HOOK_POWER_STEERING_TICK=off bash "$HOOK" </dev/null
  [ "$status" -eq 0 ]
  [ "$output" = "{}" ]
  [ ! -f "$POWER_STEERING_TICK" ]
}

@test "tick: completes in under 500ms (cheap signaler invariant)" {
  if [ -n "${EPOCHREALTIME:-}" ]; then
    start_ms=$(awk -v t="$EPOCHREALTIME" 'BEGIN{print int(t*1000)}')
  else
    start_ms=$(($(date +%s) * 1000))
  fi
  env BACK2BASE_HOOKS_LIB="$COMMON" POWER_STEERING_TICK="$POWER_STEERING_TICK" bash "$HOOK" </dev/null >/dev/null
  if [ -n "${EPOCHREALTIME:-}" ]; then
    end_ms=$(awk -v t="$EPOCHREALTIME" 'BEGIN{print int(t*1000)}')
  else
    end_ms=$(($(date +%s) * 1000))
  fi
  elapsed=$((end_ms - start_ms))
  [ "$elapsed" -lt 500 ]
}

@test "tick: subsequent calls advance mtime" {
  env BACK2BASE_HOOKS_LIB="$COMMON" POWER_STEERING_TICK="$POWER_STEERING_TICK" bash "$HOOK" </dev/null >/dev/null
  m1=$(stat -f '%m' "$POWER_STEERING_TICK" 2>/dev/null || stat -c '%Y' "$POWER_STEERING_TICK")
  sleep 1
  env BACK2BASE_HOOKS_LIB="$COMMON" POWER_STEERING_TICK="$POWER_STEERING_TICK" bash "$HOOK" </dev/null >/dev/null
  m2=$(stat -f '%m' "$POWER_STEERING_TICK" 2>/dev/null || stat -c '%Y' "$POWER_STEERING_TICK")
  [ "$m2" -gt "$m1" ]
}

@test "tick: default path (no override) is /run/back2base/power-steering/.tick" {
  # Verify default by reading the literal from the script source —
  # don't actually try to write to /run/ in the test.
  grep -F '/run/back2base/power-steering/.tick' "$HOOK"
}
