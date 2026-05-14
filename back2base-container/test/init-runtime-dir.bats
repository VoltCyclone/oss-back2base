#!/usr/bin/env bats
# Tests for lib/init-runtime-dir.sh — the wrapper invoked via sudo from
# entrypoint phase 8 to recreate /run/back2base after Docker tmpfs-mounts
# /run. We can't exercise the real path (it operates on root-owned /run),
# so the tests run the script against an overridden target prefix.

setup() {
  TEST_TMP="$(mktemp -d "${BATS_TMPDIR}/init-runtime.XXXXXX")"
  SCRIPT="$BATS_TEST_DIRNAME/../lib/init-runtime-dir.sh"
  # The shipped script hard-codes /run/back2base because the sudoers entry
  # restricts what arguments the wrapper accepts. To test the logic, we
  # copy the script into the tmp dir and string-substitute the target.
  cp "$SCRIPT" "$TEST_TMP/init-runtime-dir.sh"
  sed -i.bak "s|/run/back2base|$TEST_TMP/runtime|g; s|node:node|$(id -u):$(id -g)|g" \
    "$TEST_TMP/init-runtime-dir.sh"
  chmod +x "$TEST_TMP/init-runtime-dir.sh"
}

teardown() { rm -rf "$TEST_TMP"; }

@test "init-runtime-dir.sh: creates /run/back2base/power-steering" {
  run bash "$TEST_TMP/init-runtime-dir.sh"
  [ "$status" -eq 0 ]
  [ -d "$TEST_TMP/runtime/power-steering" ]
}

@test "init-runtime-dir.sh: idempotent — survives re-invocation" {
  mkdir -p "$TEST_TMP/runtime/power-steering"
  echo "stale" > "$TEST_TMP/runtime/old-file"

  run bash "$TEST_TMP/init-runtime-dir.sh"
  [ "$status" -eq 0 ]
  [ -d "$TEST_TMP/runtime/power-steering" ]
  # Stale contents should be wiped — the rm -rf at the top is intentional
  # so a `docker start` of an existing container doesn't inherit old state.
  [ ! -f "$TEST_TMP/runtime/old-file" ]
}

@test "init-runtime-dir.sh: shipped script targets /run/back2base only" {
  # Sudoers grants NOPASSWD for the wrapper — if the script ever accepted
  # arguments or operated on other paths, the privilege boundary widens.
  # Pin the hard-coded prefix.
  run grep -cE "^[[:space:]]*(rm -rf|mkdir -p|chown -R node:node) /run/back2base" \
    "$BATS_TEST_DIRNAME/../lib/init-runtime-dir.sh"
  [ "$status" -eq 0 ]
  [ "$output" = "3" ]
}

@test "Dockerfile: COPY + sudoers entry for init-runtime-dir.sh present" {
  DOCKERFILE="$BATS_TEST_DIRNAME/../Dockerfile"
  grep -q "COPY lib/init-runtime-dir.sh /usr/local/bin/init-runtime-dir.sh" "$DOCKERFILE"
  grep -q "node ALL=(root) NOPASSWD: /usr/local/bin/init-runtime-dir.sh" "$DOCKERFILE"
}

@test "entrypoint: no bare sudo rm/mkdir/chown on /run/back2base" {
  # Regression guard against the v0.25.x hang: bare sudo invocations on
  # /run/back2base aren't NOPASSWD-allowed and freeze gum spin.
  ENTRYPOINT="$BATS_TEST_DIRNAME/../entrypoint.sh"
  ! grep -E "^[[:space:]]*sudo (rm|mkdir|chown).*/run/back2base" "$ENTRYPOINT"
}
