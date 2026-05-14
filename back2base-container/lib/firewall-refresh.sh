#!/bin/bash
# back2base firewall refresh daemon
#
# Long sessions break silently when GitHub/Cloudflare/S3/etc. rotate IPs
# and the boot-time `allowed_ips` ipset is left holding stale entries.
# This daemon re-resolves every domain in $ALLOWED_DOMAINS at a fixed
# interval and repopulates the ipset. The iptables ESTABLISHED rule
# keeps in-flight connections alive across the flush.
#
# Additionally, when the AWS ip-ranges cache (populated by
# init-firewall.sh from https://ip-ranges.amazonaws.com/ip-ranges.json)
# goes stale (>24h by default), the daemon re-fetches and refreshes the
# allowed_nets ipset. AWS publishes the feed weekly, so a 24h check is
# generous and avoids hammering the endpoint on hosts running multiple
# containers.
#
# Launched in the background by entrypoint.sh AFTER the initial
# init-firewall.sh succeeds, and ONLY when DISABLE_FIREWALL is unset.
# Like init-firewall.sh, this needs root (ipset is privileged) — the
# entrypoint invokes it via `sudo`.
#
# Usage:
#   firewall-refresh.sh             Loop forever, refreshing the ipset.
#   firewall-refresh.sh --dry-run   Print the domains that would be
#                                   resolved, then exit 0. Used by tests.
#   firewall-refresh.sh --help      Show this message.
#
# Unknown args are rejected with exit 2.

set -u

INTERVAL="${FIREWALL_REFRESH_INTERVAL_SEC:-1800}"

# Mirror the cache path used by init-firewall.sh so a single cache is
# shared between boot-time fetch and the periodic refresh below.
: "${AWS_RANGES_CACHE:=/tmp/aws-ip-ranges.json}"
: "${AWS_RANGES_CACHE_MAX_AGE:=86400}"  # 24h

# Locate init-firewall.sh — installed at /usr/local/bin in the container,
# but tests source it from the repo. Prefer the installed copy.
_find_init_firewall() {
  if [ -r /usr/local/bin/init-firewall.sh ]; then
    echo /usr/local/bin/init-firewall.sh
    return 0
  fi
  local here
  here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  if [ -r "$here/../init-firewall.sh" ]; then
    echo "$here/../init-firewall.sh"
    return 0
  fi
  return 1
}

# Returns 0 (true) if the AWS cache file is missing or older than
# AWS_RANGES_CACHE_MAX_AGE seconds; 1 otherwise. Bash 3.2-friendly mtime
# check (BSD `stat -f %m` fallback for the macOS test host).
aws_ranges_cache_stale() {
  local cache="${1:-$AWS_RANGES_CACHE}"
  if [ ! -f "$cache" ]; then
    return 0
  fi
  local now mtime age
  now=$(date +%s 2>/dev/null || echo 0)
  mtime=$(stat -c %Y "$cache" 2>/dev/null || stat -f %m "$cache" 2>/dev/null || echo 0)
  age=$((now - mtime))
  if [ "$age" -ge "$AWS_RANGES_CACHE_MAX_AGE" ]; then
    return 0
  fi
  return 1
}

# Re-fetch ip-ranges.json (via init-firewall.sh --dry-run-fetch) and
# repopulate the allowed_nets ipset. Best-effort: any failure is logged
# but never aborts the daemon loop.
refresh_aws_ranges() {
  local script
  script="$(_find_init_firewall)" || return 0
  ipset create allowed_nets hash:net -exist 2>/dev/null || true
  ipset flush allowed_nets 2>/dev/null || true
  bash "$script" --dry-run-fetch 2>/dev/null | while read -r cidr; do
    [ -n "$cidr" ] || continue
    ipset add allowed_nets "$cidr" -exist 2>/dev/null || true
  done
  echo ":: firewall-refresh: AWS prefixes refreshed"
}

# One-shot refresh: re-resolve domains AND, when the AWS cache is stale,
# re-fetch the AWS ranges. Used by the daemon's loop body and exposed for
# callers (and tests) that want to drive the cadence externally without
# running the long-lived daemon.
firewall_refresh() {
  if declare -F refresh_allowed_ips >/dev/null 2>&1; then
    refresh_allowed_ips || true
  fi
  if aws_ranges_cache_stale; then
    refresh_aws_ranges || true
  fi
}

usage() {
  cat <<'EOF'
firewall-refresh.sh — periodic ipset refresh daemon for back2base.

Usage:
  firewall-refresh.sh             Loop forever, refreshing the ipset
                                  every $FIREWALL_REFRESH_INTERVAL_SEC
                                  seconds (default 1800). When the AWS
                                  ip-ranges cache is older than 24h,
                                  also re-fetches and refreshes
                                  allowed_nets.
  firewall-refresh.sh --dry-run   Print the domains that would be
                                  resolved, then exit 0.
  firewall-refresh.sh --help      Show this message.
EOF
}

main() {
  case "${1:-}" in
    -h|--help)
      usage
      return 0
      ;;
    --dry-run)
      local init_fw
      init_fw="$(_find_init_firewall)" || {
        echo "firewall-refresh: cannot locate init-firewall.sh" >&2
        return 1
      }
      # shellcheck source=/dev/null
      . "$init_fw" --dry-run-refresh
      local d
      for d in "${ALLOWED_DOMAINS[@]}"; do
        printf '%s\n' "$d"
      done
      return 0
      ;;
    "")
      ;;
    *)
      echo "firewall-refresh: unknown argument: $1" >&2
      usage >&2
      return 2
      ;;
  esac

  local init_fw
  init_fw="$(_find_init_firewall)" || {
    echo "firewall-refresh: cannot locate init-firewall.sh" >&2
    return 1
  }
  # shellcheck source=/dev/null
  . "$init_fw" --dry-run-refresh

  echo ":: firewall-refresh: starting (interval=${INTERVAL}s, ${#ALLOWED_DOMAINS[@]} domains)"
  while true; do
    firewall_refresh
    sleep "$INTERVAL"
  done
}

# Run only when invoked as a script. When sourced (by tests), do nothing —
# the helpers above are exposed but the daemon loop is gated.
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  main "$@"
fi
