#!/bin/bash
# Stop hook — fire-and-forget Datadog LLM Observability evaluator.
#
# Stays under the 1s hook budget by writing the event JSON to a queue file
# and detaching the Python worker. The worker reads the transcript, computes
# four evaluators (skill_choice, memory_hygiene, efficiency, answer_quality)
# and submits them via the ddtrace LLMObs SDK.

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

# b2b_hook_init drains stdin; we need the JSON, so read it before init.
EVENT_JSON="$(cat)"
HOOK_NAME="ddog-eval-stop"
HOOK_LOG="${HOME}/.claude/.back2base-hooks/${HOOK_NAME}.log"
mkdir -p "$(dirname "$HOOK_LOG")"

if b2b_hook_disabled "BACK2BASE_HOOK_DDOG_LLM_EVALS"; then
  b2b_hook_log "disabled via BACK2BASE_HOOK_DDOG_LLM_EVALS"
  b2b_hook_passthrough
fi

QUEUE_DIR="${HOME}/.claude/.back2base-hooks/ddog-llm-evals"
mkdir -p "$QUEUE_DIR"
EVENT_FILE="$QUEUE_DIR/event-$(date +%s%N).json"
printf '%s' "$EVENT_JSON" >"$EVENT_FILE"

PY="${B2B_PYTHON:-python3}"
MODULE_DIR="${B2B_LIB_DIR:-/opt/back2base/lib}"
if [ ! -f "$MODULE_DIR/ddog_llm_evals.py" ] && [ -f "$SCRIPT_DIR/../ddog_llm_evals.py" ]; then
  MODULE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
fi

(
  cd "$MODULE_DIR" || exit 0
  PYTHONPATH="$MODULE_DIR:${PYTHONPATH:-}" \
    "$PY" -m ddog_llm_evals process "$EVENT_FILE" \
    >>"$HOOK_LOG" 2>&1
  rm -f "$EVENT_FILE"
) </dev/null >/dev/null 2>&1 &
disown 2>/dev/null || true

b2b_hook_log "queued evaluation: $EVENT_FILE"
echo '{}'
exit 0
