#!/bin/bash
# claude code statusLine renderer.
#
# Reads a JSON object from stdin (claude code's session info) and prints
# a single-line footer for the in-session status bar. Cadence is owned by
# claude code (default ~5s).

set +e

input=$(cat)

# Per-container runtime dir written by power-steering. The override
# variable is for tests; production runs always resolve to /run/...
runtime_dir="${STATUSLINE_RUNTIME_DIR:-/run/back2base/power-steering}"

# Model id from JSON, or env, or "unknown".
model=$(printf '%s' "$input" | jq -r '.model.id // empty' 2>/dev/null)
model=${model:-${BACK2BASE_MODEL:-unknown}}

profile=${BACK2BASE_PROFILE:-full}
ns=${MEMORY_NAMESPACE:-?}

# Overview presence: marker the prelaunch helper wrote into CLAUDE.md.
overview="—"
if grep -q '<!-- back2base:overview-begin -->' "$HOME/.claude/CLAUDE.md" 2>/dev/null; then
  overview="✓"
fi

# Auth: presence of any supported Anthropic auth env var.
auth="✗"
if [ -n "${ANTHROPIC_API_KEY:-}" ] || [ -n "${ANTHROPIC_AUTH_TOKEN:-}" ] || [ -n "${CLAUDE_CODE_OAUTH_TOKEN:-}" ]; then
  auth="✓"
fi

# Context tokens. Prefer claude's own accounting; fall back to transcript size.
tokens=$(printf '%s' "$input" | jq -r '.cost.total_tokens // empty' 2>/dev/null)
if [ -z "$tokens" ] || [ "$tokens" = "null" ]; then
  transcript=$(printf '%s' "$input" | jq -r '.transcript_path // empty' 2>/dev/null)
  if [ -n "$transcript" ] && [ -r "$transcript" ]; then
    bytes=$(wc -c < "$transcript" 2>/dev/null | tr -d ' ')
    tokens=$((bytes / 4))
  fi
fi

# Limit depends on model variant.
case "$model" in
  *\[1m\]) limit=1048576 ;;
  *)       limit=200000 ;;
esac

ctx_str="ctx:—"
if [ -n "$tokens" ] && [ "$tokens" -gt 0 ] 2>/dev/null; then
  pct=$(( tokens * 100 / limit ))
  if [ "$pct" -lt 50 ]; then
    color=$'\e[32m'   # green
  elif [ "$pct" -lt 80 ]; then
    color=$'\e[33m'   # yellow
  else
    color=$'\e[31m'   # red
  fi
  ctx_str="ctx:${color}${pct}%"$'\e[0m'
fi

# Power-steering reviewer status. Primary signal: pending.md non-empty
# means the supervisor flagged drift and the user has NOT yet seen it
# (the UserPromptSubmit hook truncates on inject). Secondary signal: the
# tail of the log tells us idle / ok / api-error so the user can tell at
# a glance whether the daemon is actually doing anything.
pwr_label="pwr:—"
pwr_color=$'\e[2m'   # dim by default
case "${BACK2BASE_POWER_STEERING:-}" in
  off|0|false|no)
    pwr_label="pwr:off"
    ;;
  *)
    pending_file="$runtime_dir/pending.md"
    log_file="$runtime_dir/power-steering.log"
    if [ -s "$pending_file" ]; then
      pwr_label="pwr:drift!"
      pwr_color=$'\e[31m'   # red — drift queued for next turn
    elif [ -s "$log_file" ]; then
      last=$(tail -n1 "$log_file" 2>/dev/null)
      case "$last" in
        *"check drift"*) pwr_label="pwr:drift"; pwr_color=$'\e[31m' ;;
        *"check ok"*)    pwr_label="pwr:ok";    pwr_color=$'\e[32m' ;;
        *"api error"*)   pwr_label="pwr:err";   pwr_color=$'\e[33m' ;;
        *"daemon idle"*|*"; daemon idle"*) pwr_label="pwr:idle" ;;
        *"disabled via"*) pwr_label="pwr:off" ;;
        *) pwr_label="pwr:…" ;;  # daemon up, no checks yet
      esac
    fi
    ;;
esac
pwr_str="${pwr_color}${pwr_label}"$'\e[0m'

# API metrics: total tokens, cache hit ratio, total cost USD across the
# main session + power-steering's own calls + the prelaunch repo overview.
# Source: $runtime_dir/metrics.json (refreshed on each daemon
# poll, ~every 10s). Each segment renders as a dim dash when no data
# exists yet (fresh container, daemon idle, or jq missing).
metrics_file="$runtime_dir/metrics.json"
tok_str="tok:—"
cache_str="cache:—"
cost_str="cost:—"
if [ -s "$metrics_file" ] && command -v jq >/dev/null 2>&1; then
  vals=$(jq -r '[
    (.input_tokens // 0),
    (.output_tokens // 0),
    (.cache_creation_input_tokens // 0),
    (.cache_read_input_tokens // 0),
    (.request_count // 0),
    (.cost_usd // 0)
  ] | @tsv' "$metrics_file" 2>/dev/null)
  if [ -n "$vals" ]; then
    IFS=$'\t' read -r m_in m_out m_cw m_cr m_req m_cost <<< "$vals"
    if [ "${m_req:-0}" -gt 0 ] 2>/dev/null; then
      total=$(( m_in + m_out + m_cw + m_cr ))
      tok_str=$(awk -v t="$total" 'BEGIN {
        if (t >= 1000000) printf "tok:%.1fM", t/1000000
        else if (t >= 1000) printf "tok:%.1fk", t/1000
        else printf "tok:%d", t
      }')
      uncached_in=$(( m_in + m_cw + m_cr ))
      if [ "$uncached_in" -gt 0 ]; then
        cache_str=$(awk -v r="$m_cr" -v u="$uncached_in" \
          'BEGIN { printf "cache:%d%%", r/u*100 }')
      fi
      cost_str=$(awk -v c="$m_cost" 'BEGIN { printf "cost:$%.2f", c }')
    fi
  fi
fi
metrics_dim=$'\e[2m'
tok_str="${metrics_dim}${tok_str}"$'\e[0m'
cache_str="${metrics_dim}${cache_str}"$'\e[0m'
cost_str="${metrics_dim}${cost_str}"$'\e[0m'

sep=$'\e[2m·\e[0m'

printf '\e[1m%s\e[0m %s ns:%s %s %s %s overview:%s %s %s %s auth:%s %s %s %s %s %s %s %s %s\n' \
  "$model" "$sep" "$ns" "$sep" "$profile" "$sep" "$overview" "$sep" "$ctx_str" \
  "$sep" "$auth" "$sep" "$pwr_str" \
  "$sep" "$tok_str" "$sep" "$cache_str" "$sep" "$cost_str"
