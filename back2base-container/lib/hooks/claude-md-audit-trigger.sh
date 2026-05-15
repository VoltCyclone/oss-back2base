#!/usr/bin/env bash
# UserPromptSubmit hook — runs the audit decision; if a suggestion is
# emitted, appends it to /run/back2base/power-steering/pending.md. The
# existing power-steering-drain hook (registered AFTER this one) reads
# pending.md and injects it as additionalContext on this same turn.

set -u
. "${BACK2BASE_HOOKS_LIB:-/opt/back2base/hooks/_common.sh}"

b2b_hook_init "claude-md-audit-trigger"
b2b_hook_disabled "BACK2BASE_HOOK_CLAUDE_MD_AUDIT" && b2b_hook_passthrough

AUDIT="${CLAUDE_MD_AUDIT_PY:-/opt/back2base/claude-md-audit.py}"
PENDING="${POWER_STEERING_PENDING:-/run/back2base/power-steering/pending.md}"

mkdir -p "$(dirname "$PENDING")"
SUGGESTION="$(python3 "$AUDIT" decide 2>/dev/null || true)"

if [ -n "$SUGGESTION" ]; then
  if [ -s "$PENDING" ]; then
    printf '\n\n%s' "$SUGGESTION" >> "$PENDING"
  else
    printf '%s' "$SUGGESTION" >> "$PENDING"
  fi
  b2b_hook_log "appended ${#SUGGESTION} chars"
fi

b2b_hook_passthrough
