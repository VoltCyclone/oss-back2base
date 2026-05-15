#!/usr/bin/env bash
# back2base hook common library.
#
# Sourced by every script under lib/hooks/. Provides:
#   b2b_hook_init <name>                — required first call. Sets
#                                          HOOK_NAME, HOOK_LOG; drains stdin.
#   b2b_hook_disabled <env_var_name>    — returns 0 if var is off|0|false|no.
#   b2b_hook_log <message…>             — append to ~/.claude/.back2base-hooks/<name>.log
#   b2b_hook_passthrough                — `echo '{}'; exit 0` (canonical no-op).
#   b2b_hook_emit_context <event> <ctx> — emit hookSpecificOutput envelope.
#   b2b_hook_emit_raw <json>            — emit caller-shaped JSON.
#
# All hooks are budgeted < 1s. Heavy work belongs in the daemons.

b2b_hook_init() {
  HOOK_NAME="$1"
  HOOK_LOG="${HOME}/.claude/.back2base-hooks/${HOOK_NAME}.log"
  mkdir -p "$(dirname "$HOOK_LOG")"
  # Drain stdin: Claude Code may pipe a JSON payload we don't consume. If we
  # leave it in the buffer, the writer side may block on a full pipe.
  cat >/dev/null 2>&1 || true
}

b2b_hook_disabled() {
  case "${!1:-}" in
    off|0|false|no) return 0 ;;
    *) return 1 ;;
  esac
}

b2b_hook_log() {
  local ts
  ts=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  printf '%s %s %s\n' "$ts" "$HOOK_NAME" "$*" >> "$HOOK_LOG"
}

b2b_hook_passthrough() {
  echo '{}'
  exit 0
}

b2b_hook_emit_context() {
  local event="$1" ctx="$2"
  if ! command -v jq >/dev/null 2>&1; then
    b2b_hook_log "jq missing; passthrough"
    b2b_hook_passthrough
  fi
  jq -nc \
    --arg evt "$event" \
    --arg ctx "$ctx" \
    '{hookSpecificOutput: {hookEventName: $evt, additionalContext: $ctx}}'
}

b2b_hook_emit_raw() {
  printf '%s\n' "$1"
}
