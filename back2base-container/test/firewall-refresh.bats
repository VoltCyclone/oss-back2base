#!/usr/bin/env bats

# Tests for the periodic firewall ipset refresh daemon and the supporting
# refactor of init-firewall.sh. These tests cannot exercise real iptables/
# ipset (privileged, kernel-dependent) — they verify the script structure
# and the sourceable refresh function instead.

setup() {
  REPO_DIR="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
}

@test "firewall-refresh.sh exists and is executable" {
  [ -x "$REPO_DIR/lib/firewall-refresh.sh" ]
}

@test "firewall-refresh.sh has a bash shebang" {
  head -n 1 "$REPO_DIR/lib/firewall-refresh.sh" | grep -qE '^#!.*bash'
}

@test "firewall-refresh.sh --help exits 0 and prints usage" {
  run "$REPO_DIR/lib/firewall-refresh.sh" --help
  [ "$status" -eq 0 ]
  echo "$output" | grep -qi 'firewall'
}

@test "firewall-refresh.sh --dry-run prints the domains it would resolve" {
  run "$REPO_DIR/lib/firewall-refresh.sh" --dry-run
  [ "$status" -eq 0 ]
  echo "$output" | grep -q 'api.anthropic.com'
  echo "$output" | grep -q 'github.com'
}

@test "firewall-refresh.sh --dry-run respects MEMORY_MCP_URL injection" {
  MEMORY_MCP_URL=https://memory.example.test/mcp run "$REPO_DIR/lib/firewall-refresh.sh" --dry-run
  [ "$status" -eq 0 ]
  echo "$output" | grep -q 'memory.example.test'
}

@test "firewall-refresh.sh rejects unknown args with exit 2" {
  run "$REPO_DIR/lib/firewall-refresh.sh" --bogus-flag
  [ "$status" -eq 2 ]
}

@test "init-firewall.sh exposes refresh_allowed_ips when sourced via --dry-run-refresh" {
  # Source the script in dry-run mode. This must not invoke iptables/ipset
  # but must define the refresh_allowed_ips function.
  source "$REPO_DIR/init-firewall.sh" --dry-run-refresh
  declare -f refresh_allowed_ips >/dev/null
}

@test "init-firewall.sh --dry-run-refresh defines ALLOWED_DOMAINS as non-empty array" {
  source "$REPO_DIR/init-firewall.sh" --dry-run-refresh
  [ "${#ALLOWED_DOMAINS[@]}" -gt 0 ]
}

@test "init-firewall.sh --dry-run-refresh picks up MEMORY_MCP_URL host" {
  MEMORY_MCP_URL=https://memory.example.test/mcp source "$REPO_DIR/init-firewall.sh" --dry-run-refresh
  printf '%s\n' "${ALLOWED_DOMAINS[@]}" | grep -q '^memory\.example\.test$'
}

@test "init-firewall.sh allow-lists the back2base OTel collector" {
  # Daemon ships traces here via mTLS; firewall must let outbound
  # connections through.
  source "$REPO_DIR/init-firewall.sh" --dry-run-refresh
  printf '%s\n' "${ALLOWED_DOMAINS[@]}" | grep -q '^otel\.back2base\.net$'
}
