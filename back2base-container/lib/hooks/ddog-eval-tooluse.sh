#!/bin/bash
# PostToolUse hook — accumulates per-turn tool-call counters for the
# efficiency evaluator. Stays well under the 1s budget: one append.

set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -n "${BACK2BASE_HOOKS_LIB:-}" ] && [ -f "${BACK2BASE_HOOKS_LIB}" ]; then
  . "${BACK2BASE_HOOKS_LIB}"
elif [ -f "$SCRIPT_DIR/_common.sh" ]; then
  . "$SCRIPT_DIR/_common.sh"
elif [ -f "/opt/back2base/hooks/_common.sh" ]; then
  . "/opt/back2base/hooks/_common.sh"
else
  exit 0
fi

EVENT_JSON="$(cat)"
HOOK_NAME="ddog-eval-tooluse"
HOOK_LOG="${HOME}/.claude/.back2base-hooks/${HOOK_NAME}.log"
mkdir -p "$(dirname "$HOOK_LOG")"

if b2b_hook_disabled "BACK2BASE_HOOK_DDOG_LLM_EVALS"; then
  b2b_hook_passthrough
fi

QUEUE_DIR="${HOME}/.claude/.back2base-hooks/ddog-llm-evals"
mkdir -p "$QUEUE_DIR"
EVENT_FILE="$QUEUE_DIR/tool-$(date +%s%N).json"
printf '%s' "$EVENT_JSON" >"$EVENT_FILE"

PY="${B2B_PYTHON:-python3}"
MODULE_DIR="${B2B_LIB_DIR:-/opt/back2base/lib}"
if [ ! -f "$MODULE_DIR/ddog_llm_evals.py" ] && [ -f "$SCRIPT_DIR/../ddog_llm_evals.py" ]; then
  MODULE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
fi

PYTHONPATH="$MODULE_DIR:${PYTHONPATH:-}" \
  "$PY" -m ddog_llm_evals tooluse "$EVENT_FILE" \
  >>"$HOOK_LOG" 2>&1 || true
rm -f "$EVENT_FILE"

echo '{}'
exit 0
