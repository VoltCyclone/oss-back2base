#!/bin/bash
# Firewall initialization for Claude Code devcontainer.
# Whitelists only the domains Claude Code and the bundled MCP servers need.
# Everything else is default-deny outbound.
#
# Usage:
#   init-firewall.sh                  Run once at container start: resolve
#                                     domains, populate the allowed_ips
#                                     ipset, and install iptables rules.
#   source init-firewall.sh --dry-run-refresh
#                                     Define ALLOWED_DOMAINS and the
#                                     refresh_allowed_ips function in the
#                                     calling shell WITHOUT executing
#                                     iptables/ipset commands. Used by
#                                     bats tests and lib/firewall-refresh.sh
#                                     (the periodic ipset refresh daemon).

# When sourced with --dry-run-refresh we must not `set -e`: callers (bats,
# the refresh daemon's --dry-run path) need to keep their shell alive even
# if a probe command fails. The full-init path below still gets strict mode.
if [ "${1:-}" != "--dry-run-refresh" ]; then
  set -euo pipefail
fi

# -- AWS IP ranges helpers --------------------------------------------------
# AWS publishes its authoritative public IP allocations at
# https://ip-ranges.amazonaws.com/ip-ranges.json. We use this list to scope
# the deny-by-default firewall to the actual AWS prefixes our MCP servers
# need, rather than blanket-allowing eight /8 blocks (most of which are NOT
# AWS-owned). See https://docs.aws.amazon.com/vpc/latest/userguide/aws-ip-ranges.html
#
# The data file refreshes weekly, so we cache it for 24h between fetches.

AWS_RANGES_URL="${AWS_RANGES_URL:-https://ip-ranges.amazonaws.com/ip-ranges.json}"
AWS_RANGES_CACHE="${AWS_RANGES_CACHE:-/tmp/aws-ip-ranges.json}"
AWS_RANGES_CACHE_MAX_AGE="${AWS_RANGES_CACHE_MAX_AGE:-86400}"  # 24h in seconds

# Filter a downloaded ip-ranges.json to the prefixes we care about.
# Args: $1 = path to ip-ranges.json on disk.
# Stdout: one CIDR per line, sorted+deduped. Empty if jq isn't installed.
_filter_aws_ranges() {
  local file="$1"
  if ! command -v jq >/dev/null 2>&1; then
    return 1
  fi
  jq -r '
    .prefixes[]
    | select(.service == "AMAZON" or .service == "S3" or .service == "EC2" or .service == "API_GATEWAY")
    | .ip_prefix
  ' "$file" 2>/dev/null | sort -u
}

# Hardcoded /8 fallback. Used when fetch+cache both fail at boot — the
# container needs SOME path to AWS so MCP servers and CLI tools work,
# even if it's a coarser allow than the published ranges.
# Stdout: one CIDR per line.
_fallback_aws_ranges() {
  cat <<'EOF'
3.0.0.0/8
13.0.0.0/8
15.0.0.0/8
18.0.0.0/8
35.0.0.0/8
44.0.0.0/8
52.0.0.0/8
54.0.0.0/8
EOF
}

# Fetch (or read from cache), filter, and emit the AWS prefix list. Falls
# back to _fallback_aws_ranges on any error so callers always get a
# non-empty list. Stdout: one CIDR per line.
_fetch_aws_ranges() {
  # If the cache is fresh, use it directly. mtime check keeps the function
  # cheap — a typical container start re-runs this without re-hitting AWS.
  if [ -f "$AWS_RANGES_CACHE" ]; then
    local now mtime age
    now=$(date +%s 2>/dev/null || echo 0)
    # GNU stat first, BSD stat fallback (so the dry-run runs on macOS test hosts).
    mtime=$(stat -c %Y "$AWS_RANGES_CACHE" 2>/dev/null || stat -f %m "$AWS_RANGES_CACHE" 2>/dev/null || echo 0)
    age=$((now - mtime))
    if [ "$age" -lt "$AWS_RANGES_CACHE_MAX_AGE" ]; then
      if _filter_aws_ranges "$AWS_RANGES_CACHE"; then
        return 0
      fi
    fi
  fi

  # Cache stale or missing. Try to fetch.
  local tmpfile
  tmpfile=$(mktemp)
  if curl -fsSL --max-time 15 --retry 2 "$AWS_RANGES_URL" -o "$tmpfile" 2>/dev/null; then
    # Validate the downloaded JSON before adopting it as the cache.
    if jq -e '.prefixes' "$tmpfile" >/dev/null 2>&1; then
      mv -f "$tmpfile" "$AWS_RANGES_CACHE"
      _filter_aws_ranges "$AWS_RANGES_CACHE"
      return 0
    fi
  fi
  rm -f "$tmpfile"

  # Fetch failed. Fall back to a stale cache if we have one (better than /8s).
  if [ -f "$AWS_RANGES_CACHE" ] && _filter_aws_ranges "$AWS_RANGES_CACHE"; then
    return 0
  fi

  # No cache either. Emit the /8 blanket so the container can still reach AWS.
  echo "WARN: failed to fetch $AWS_RANGES_URL — using /8 fallback" >&2
  _fallback_aws_ranges
}

# -- Dry-run modes (used by bats tests; never run in the container) ---------
case "${1:-}" in
  --dry-run-aws-ranges)
    # Print what _filter_aws_ranges would produce for a given fixture.
    _filter_aws_ranges "$2"
    exit 0
    ;;
  --dry-run-fallback)
    _fallback_aws_ranges
    exit 0
    ;;
  --dry-run-fetch)
    _fetch_aws_ranges
    exit 0
    ;;
esac

# -- Resolve domains to IPs ------------------------------------------------
ALLOWED_DOMAINS=(
  # Claude Code core
  "api.anthropic.com"
  "claude.ai"
  "platform.claude.com"
  "statsig.anthropic.com"
  "sentry.io"

  # npm / node
  "registry.npmjs.org"

  # GitHub (for gh CLI, npx fetches, git operations)
  "github.com"
  "api.github.com"
  "raw.githubusercontent.com"
  "objects.githubusercontent.com"
  "pkg-cache.githubusercontent.com"
  "codeload.github.com"

  # Terraform Registry (terraform-mcp-server)
  "registry.terraform.io"
  "releases.hashicorp.com"
  "app.terraform.io"
  "checkpoint-api.hashicorp.com"

  # AWS (aws CLI, STS, service endpoints)
  "sts.amazonaws.com"
  "*.amazonaws.com"
  "knowledge-mcp.global.api.aws"

  # AWS IP-ranges feed itself — needed before the firewall installs the
  # allowed_nets ipset, so the curl fetch in _fetch_aws_ranges can reach it.
  "ip-ranges.amazonaws.com"

  # Cloudflare AI Gateway
  "gateway.ai.cloudflare.com"

  # Context7 MCP (remote HTTP transport)
  "mcp.context7.com"

  # Datadog MCP (LLM Observability + local CLI OAuth)
  "mcp.datadoghq.com"
  "coterm.datadoghq.com"
  "app.datadoghq.com"

  # Brave Search API (brave-search MCP server)
  "api.search.brave.com"

  # PyPI (for uvx/pip installs inside container)
  "pypi.org"
  "files.pythonhosted.org"

  # Buildkite MCP
  "api.buildkite.com"

  # Kubernetes
  "dl.k8s.io"
  "storage.googleapis.com"

  # Go modules
  "proxy.golang.org"
  "sum.golang.org"
  "storage.googleapis.com"
)

# Dynamically add memory MCP Worker domain from env (keeps URLs out of this file)
if [ -n "${MEMORY_MCP_URL:-}" ]; then
  # Extract hostname from URL (strip protocol and path)
  _memory_host=$(echo "$MEMORY_MCP_URL" | sed -E 's|^https?://||; s|/.*||')
  if [ -n "$_memory_host" ]; then
    ALLOWED_DOMAINS+=("$_memory_host")
  fi
fi

# refresh_allowed_ips — resolve every entry in $ALLOWED_DOMAINS and
# repopulate the `allowed_ips` ipset. Safe to call repeatedly; the
# iptables ESTABLISHED rule keeps in-flight connections alive even
# when stale IPs are removed during the flush+repopulate cycle.
refresh_allowed_ips() {
  # Create the ipset if it doesn't exist yet (no-op on subsequent calls).
  ipset create allowed_ips hash:ip -exist 2>/dev/null || true
  ipset flush allowed_ips

  # Resolve all domains in parallel, collect IPs into a temp file, then bulk-add
  local _tmpips
  _tmpips=$(mktemp)
  _resolve_domain() {
    local domain="$1"
    [[ "$domain" == *"*"* ]] && return
    dig +short +time=2 +tries=1 "$domain" 2>/dev/null | grep -E '^[0-9]+\.'
  }
  export -f _resolve_domain

  local domain
  for domain in "${ALLOWED_DOMAINS[@]}"; do
    [[ "$domain" == *"*"* ]] && continue
    _resolve_domain "$domain" >> "$_tmpips" &
  done
  wait

  sort -u "$_tmpips" | while read -r ip; do
    ipset add allowed_ips "$ip" -exist 2>/dev/null || true
  done
  rm -f "$_tmpips"
}

# Test/daemon hook: when sourced with --dry-run-refresh, expose
# ALLOWED_DOMAINS and refresh_allowed_ips to the caller and stop.
# This MUST NOT touch iptables or ipset.
if [ "${1:-}" = "--dry-run-refresh" ]; then
  return 0 2>/dev/null || exit 0
fi

# -- Initial setup ---------------------------------------------------------
# Populate the ipset and then install iptables rules. Both happen exactly
# once per container, at boot. Periodic refreshes are driven by
# lib/firewall-refresh.sh (launched as a daemon by entrypoint.sh).

refresh_allowed_ips

# -- AWS published prefixes (replaces the /8 blanket) -----------------------
# allowed_nets is a CIDR-aware ipset (hash:net) populated from AWS's
# ip-ranges.json. Falls back to the /8 blanket if the fetch fails so the
# container still boots in offline / network-restricted environments.
ipset create allowed_nets hash:net -exist 2>/dev/null || true
ipset flush allowed_nets
_fetch_aws_ranges | while read -r cidr; do
  [ -n "$cidr" ] || continue
  ipset add allowed_nets "$cidr" -exist 2>/dev/null || true
done

# -- iptables rules ---------------------------------------------------------

# Flush existing rules
iptables -F OUTPUT 2>/dev/null || true

# Allow loopback
iptables -A OUTPUT -o lo -j ACCEPT

# Allow established/related connections
iptables -A OUTPUT -m state --state ESTABLISHED,RELATED -j ACCEPT

# Allow DNS (needed for domain resolution)
iptables -A OUTPUT -p udp --dport 53 -j ACCEPT
iptables -A OUTPUT -p tcp --dport 53 -j ACCEPT

# Allow SSH (for git operations)
iptables -A OUTPUT -p tcp --dport 22 -j ACCEPT

# Allow HTTPS to resolved IPs
iptables -A OUTPUT -p tcp --dport 443 -m set --match-set allowed_ips dst -j ACCEPT

# Allow HTTP to resolved IPs (some registries redirect)
iptables -A OUTPUT -p tcp --dport 80 -m set --match-set allowed_ips dst -j ACCEPT

# AWS published prefixes (allowed_nets is populated from ip-ranges.json above).
# Replaces the previous 8 hardcoded /8 ACCEPT rules — far smaller surface area.
iptables -A OUTPUT -p tcp --dport 443 -m set --match-set allowed_nets dst -j ACCEPT

# Default deny all other outbound
iptables -A OUTPUT -j DROP

echo "Firewall initialized: $(ipset list allowed_ips | grep -c 'Members:' || echo 0) domains resolved, $(ipset list allowed_nets 2>/dev/null | grep -cE '^[0-9]+\.' || echo 0) AWS prefixes"
