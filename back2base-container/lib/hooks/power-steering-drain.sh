#!/usr/bin/env bash
# UserPromptSubmit hook — drains /run/back2base/power-steering/pending.md
# into the user's next turn as additionalContext, then truncates.

set -u
# BACK2BASE_HOOKS_LIB: test-only override for the hooks library path.
. "${BACK2BASE_HOOKS_LIB:-/opt/back2base/hooks/_common.sh}"

b2b_hook_init "power-steering-drain"
b2b_hook_disabled "BACK2BASE_HOOK_POWER_STEERING_DRAIN" && b2b_hook_passthrough

PENDING="${POWER_STEERING_PENDING:-/run/back2base/power-steering/pending.md}"
[ -s "$PENDING" ] || b2b_hook_passthrough

content=$(cat "$PENDING")
# Truncate now — losing one nudge beats re-injecting on every subsequent turn.
: > "$PENDING"

b2b_hook_log "drained ${#content} chars"
b2b_hook_emit_context "UserPromptSubmit" "$content"
