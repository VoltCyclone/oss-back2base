#!/bin/bash
# Cold-start phase indicator helpers. Sourced by entrypoint.sh.
#
# _status_run "<title>" <cmd...>      Wrap cmd in `gum spin`, return cmd's exit code.
# _status_warn "<message>"            Between-phase advisory; uses `gum log` when available.
# _should_fall_back                   Internal: returns 0 (fall back to chatty) when
#                                     BACK2BASE_VERBOSE=1, stderr is not a TTY, or gum is missing.
#                                     FORCE_TTY=1 overrides the TTY check (test hook).

_should_fall_back() {
  [ "${BACK2BASE_VERBOSE:-0}" = "1" ] && return 0
  if [ "${FORCE_TTY:-}" != "1" ] && [ ! -t 2 ]; then
    return 0
  fi
  command -v gum >/dev/null 2>&1 || return 0
  return 1
}

_status_run() {
  local title="$1"; shift
  if _should_fall_back; then
    echo "→ $title" >&2
    "$@"
    return $?
  fi
  local start=$SECONDS
  local rc=0

  # gum spin runs the wrapped command via execve, which only sees exported
  # variables and binaries on PATH — it cannot resolve shell functions
  # defined in this process. If the caller passed a function name, export
  # every currently-defined function (the wrapper plus any helpers it
  # calls) and dispatch through `bash -c` so they all resolve in the
  # child shell. Otherwise run the command line as-is.
  if [ $# -gt 0 ] && declare -F "$1" >/dev/null 2>&1; then
    local _fn
    while IFS= read -r _fn; do
      # shellcheck disable=SC2163
      export -f "$_fn" 2>/dev/null || true
    done < <(declare -F | awk '{print $3}')
    gum spin --spinner dot --show-error --title "$title" -- bash -c '"$@"' _ "$@"
    rc=$?
  else
    gum spin --spinner dot --show-error --title "$title" -- "$@"
    rc=$?
  fi

  if [ "$rc" -eq 0 ]; then
    printf '\e[32m✓\e[0m %s \e[2m(%ds)\e[0m\n' "$title" "$((SECONDS - start))" >&2
    return 0
  else
    printf '\e[31m✗\e[0m %s \e[2m(failed in %ds)\e[0m\n' "$title" "$((SECONDS - start))" >&2
    return $rc
  fi
}

_status_warn() {
  if ! _should_fall_back; then
    gum log --level warn "$1" >&2
  else
    echo "⚠ $1" >&2
  fi
}
