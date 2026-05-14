#!/usr/bin/env bash
# PostToolUse hook — for Edit/Write/MultiEdit tools, append the file path
# to /run/back2base/power-steering/edits.jsonl. The decision logic lives
# in claude-md-audit.py; this hook is a thin wrapper that captures the
# tool payload (b2b_hook_init drains stdin) and pipes it through.

set -u
. "${BACK2BASE_HOOKS_LIB:-/opt/back2base/hooks/_common.sh}"

# Capture stdin BEFORE b2b_hook_init drains it.
PAYLOAD=$(cat || true)

b2b_hook_init "claude-md-audit-tally"
b2b_hook_disabled "BACK2BASE_HOOK_CLAUDE_MD_AUDIT" && b2b_hook_passthrough

AUDIT="${CLAUDE_MD_AUDIT_PY:-/opt/back2base/claude-md-audit.py}"
printf '%s' "$PAYLOAD" | python3 "$AUDIT" tally >/dev/null 2>&1 || true

b2b_hook_passthrough
