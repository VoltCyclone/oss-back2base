#!/usr/bin/env bash
# PostToolUse hook — touches /run/back2base/power-steering/.tick. The
# power-steering daemon watches the file's mtime instead of polling
# the JSONL on a fixed timer. Idle sessions cost zero CPU.
#
# This hook does NOT count tools or read the JSONL — that logic lives
# inside the daemon, single source of truth.

set -u
# BACK2BASE_HOOKS_LIB: test-only override for the hooks library path.
. "${BACK2BASE_HOOKS_LIB:-/opt/back2base/hooks/_common.sh}"

b2b_hook_init "power-steering-tick"
b2b_hook_disabled "BACK2BASE_HOOK_POWER_STEERING_TICK" && b2b_hook_passthrough

TICK="${POWER_STEERING_TICK:-/run/back2base/power-steering/.tick}"
mkdir -p "$(dirname "$TICK")"
: > "$TICK"

b2b_hook_passthrough
