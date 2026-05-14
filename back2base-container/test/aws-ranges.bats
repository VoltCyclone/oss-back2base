#!/usr/bin/env bats

# Tests for the AWS ip-ranges integration in init-firewall.sh and
# lib/firewall-refresh.sh.
#
# init-firewall.sh and firewall-refresh.sh are designed to be source-able in
# a "dry-run" mode that defines the helper functions but skips the side
# effects (ipset/iptables calls, curl fetches). Tests exercise the helpers
# in isolation against fixture JSONs.

setup() {
  TEST_TMP="$(mktemp -d "${BATS_TMPDIR}/back2base-aws-ranges.XXXXXX")"
  export TEST_TMP

  # Mock ipset / iptables to no-ops so accidental live calls don't break.
  STUB_BIN="$TEST_TMP/stub-bin"
  mkdir -p "$STUB_BIN"
  for cmd in ipset iptables sudo dig; do
    printf '#!/bin/sh\nexit 0\n' > "$STUB_BIN/$cmd"
    chmod +x "$STUB_BIN/$cmd"
  done
  export PATH="$STUB_BIN:$PATH"

  # Build a fixture JSON with a mix of services.
  cat > "$TEST_TMP/ip-ranges.json" <<'JSON'
{
  "syncToken": "1234567890",
  "createDate": "2026-04-29-00-00-00",
  "prefixes": [
    {"ip_prefix": "3.5.140.0/22",   "service": "AMAZON",       "region": "ap-northeast-2"},
    {"ip_prefix": "52.94.0.0/22",   "service": "S3",           "region": "us-east-1"},
    {"ip_prefix": "54.231.0.0/17",  "service": "EC2",          "region": "us-east-1"},
    {"ip_prefix": "13.32.0.0/15",   "service": "API_GATEWAY",  "region": "us-east-1"},
    {"ip_prefix": "13.224.0.0/14",  "service": "CLOUDFRONT",   "region": "GLOBAL"},
    {"ip_prefix": "99.86.0.0/16",   "service": "CLOUDFRONT",   "region": "GLOBAL"},
    {"ip_prefix": "3.5.140.0/22",   "service": "AMAZON",       "region": "ap-northeast-2"}
  ],
  "ipv6_prefixes": [
    {"ipv6_prefix": "2600:1f00::/32", "service": "AMAZON", "region": "us-east-1"}
  ]
}
JSON
}

teardown() {
  rm -rf "$TEST_TMP"
}

# ── _filter_aws_ranges: keep only AMAZON/S3/EC2/API_GATEWAY ─────────────────

@test "_filter_aws_ranges keeps AMAZON, S3, EC2, API_GATEWAY services" {
  run bash "$BATS_TEST_DIRNAME/../init-firewall.sh" --dry-run-aws-ranges "$TEST_TMP/ip-ranges.json"
  [ "$status" -eq 0 ]
  echo "$output" | grep -q '^3\.5\.140\.0/22$'
  echo "$output" | grep -q '^52\.94\.0\.0/22$'
  echo "$output" | grep -q '^54\.231\.0\.0/17$'
  echo "$output" | grep -q '^13\.32\.0\.0/15$'
}

@test "_filter_aws_ranges drops CLOUDFRONT prefixes" {
  run bash "$BATS_TEST_DIRNAME/../init-firewall.sh" --dry-run-aws-ranges "$TEST_TMP/ip-ranges.json"
  [ "$status" -eq 0 ]
  ! echo "$output" | grep -q '13\.224\.0\.0/14'
  ! echo "$output" | grep -q '99\.86\.0\.0/16'
}

@test "_filter_aws_ranges deduplicates identical prefixes" {
  run bash "$BATS_TEST_DIRNAME/../init-firewall.sh" --dry-run-aws-ranges "$TEST_TMP/ip-ranges.json"
  [ "$status" -eq 0 ]
  count=$(echo "$output" | grep -c '^3\.5\.140\.0/22$')
  [ "$count" -eq 1 ]
}

@test "_filter_aws_ranges ignores ipv6_prefixes" {
  run bash "$BATS_TEST_DIRNAME/../init-firewall.sh" --dry-run-aws-ranges "$TEST_TMP/ip-ranges.json"
  [ "$status" -eq 0 ]
  ! echo "$output" | grep -q '2600:'
}

# ── Fallback path when fetch fails ─────────────────────────────────────────

@test "_fallback_aws_ranges prints the original /8 set" {
  run bash "$BATS_TEST_DIRNAME/../init-firewall.sh" --dry-run-fallback
  [ "$status" -eq 0 ]
  for octet in 3 52 54 18 13 15 35 44; do
    echo "$output" | grep -q "^${octet}\.0\.0\.0/8$"
  done
}

@test "_fetch_aws_ranges falls back to /8 set when curl fails" {
  # Mock curl to always fail. AWS_RANGES_CACHE pointed at a path that does
  # not exist, so the fetch must use curl. The function should still emit
  # the /8 fallback list and return success.
  curl_stub="$STUB_BIN/curl"
  printf '#!/bin/sh\nexit 6\n' > "$curl_stub"
  chmod +x "$curl_stub"

  export AWS_RANGES_CACHE="$TEST_TMP/cache.json"
  run bash "$BATS_TEST_DIRNAME/../init-firewall.sh" --dry-run-fetch
  [ "$status" -eq 0 ]
  echo "$output" | grep -q '^3\.0\.0\.0/8$'
  echo "$output" | grep -q '^52\.0\.0\.0/8$'
}

@test "_fetch_aws_ranges uses cache when fresh (<24h)" {
  # Drop the fixture in place of the cache; curl stub returns failure so we
  # know cache was used if the AMAZON prefix shows up.
  curl_stub="$STUB_BIN/curl"
  printf '#!/bin/sh\nexit 6\n' > "$curl_stub"
  chmod +x "$curl_stub"

  export AWS_RANGES_CACHE="$TEST_TMP/cache.json"
  cp "$TEST_TMP/ip-ranges.json" "$AWS_RANGES_CACHE"

  run bash "$BATS_TEST_DIRNAME/../init-firewall.sh" --dry-run-fetch
  [ "$status" -eq 0 ]
  echo "$output" | grep -q '^3\.5\.140\.0/22$'
}

# ── firewall-refresh integration ───────────────────────────────────────────

@test "firewall-refresh.sh defines firewall_refresh function" {
  source "$BATS_TEST_DIRNAME/../lib/firewall-refresh.sh"
  declare -F firewall_refresh >/dev/null
}

@test "firewall-refresh re-fetches AWS ranges only after 24h" {
  source "$BATS_TEST_DIRNAME/../lib/firewall-refresh.sh"
  # Fresh cache → should NOT re-fetch
  cp "$TEST_TMP/ip-ranges.json" "$TEST_TMP/cache.json"
  run aws_ranges_cache_stale "$TEST_TMP/cache.json"
  [ "$status" -ne 0 ]

  # Stale cache (touch to 25h ago) → SHOULD re-fetch
  touch -t "$(date -v-25H +%Y%m%d%H%M.%S 2>/dev/null || date -d '25 hours ago' +%Y%m%d%H%M.%S)" "$TEST_TMP/cache.json"
  run aws_ranges_cache_stale "$TEST_TMP/cache.json"
  [ "$status" -eq 0 ]

  # Missing cache → SHOULD re-fetch
  run aws_ranges_cache_stale "$TEST_TMP/missing.json"
  [ "$status" -eq 0 ]
}
