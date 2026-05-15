#!/bin/bash
# run_prelaunch_overview — opt-in, blocking, pre-launch repo overview.
# Sourced by entrypoint.sh between generate_claude_md and the daemon launches.
#
# Reads:
#   BACK2BASE_OVERVIEW    1 = run, anything else = skip
#   PWD                   directory to scan (already cd'd to repo by entrypoint)
#   HOME                  ~/.claude/CLAUDE.md is the splice target
#   OVERVIEW_TIMEOUT_SECS    (test-only override; default 240)
#   OVERVIEW_HEARTBEAT_SECS  (test-only override; default 10)
#   OVERVIEW_PROMPT_PATH     (test-only override; default /opt/back2base/prelaunch-prompt.txt)
#   OVERVIEW_RENDERER_PATH   (test-only override; default /opt/back2base/render-overview.py)
#
# Never propagates a failure: any error becomes a stderr warning and a 0
# return so the entrypoint's `exec "$@"` always runs.

run_prelaunch_overview() {
  if [ "${BACK2BASE_OVERVIEW:-0}" != "1" ]; then
    return 0
  fi

  local timeout_secs="${OVERVIEW_TIMEOUT_SECS:-240}"
  local heartbeat_secs="${OVERVIEW_HEARTBEAT_SECS:-10}"
  local prompt_path="${OVERVIEW_PROMPT_PATH:-/opt/back2base/prelaunch-prompt.txt}"
  local renderer="${OVERVIEW_RENDERER_PATH:-/opt/back2base/render-overview.py}"
  local claude_md="$HOME/.claude/CLAUDE.md"

  if ! command -v claude >/dev/null 2>&1; then
    echo ":: ⚠ overview skipped: claude CLI not found" >&2
    return 0
  fi
  if [ ! -r "$prompt_path" ]; then
    echo ":: ⚠ overview skipped: prompt file missing ($prompt_path)" >&2
    return 0
  fi
  if [ ! -r "$claude_md" ]; then
    echo ":: ⚠ overview skipped: CLAUDE.md missing ($claude_md)" >&2
    return 0
  fi

  local prompt
  prompt="$(cat "$prompt_path")"

  local tmp_out tmp_err
  tmp_out="$(mktemp "${TMPDIR:-/tmp}/repo-overview.XXXXXX.md")"
  tmp_err="$(mktemp "${TMPDIR:-/tmp}/repo-overview-err.XXXXXX")"

  echo ":: overview: starting (model=claude-haiku-4-5, scan=$PWD, timeout=${timeout_secs}s)" >&2

  # Run claude -p in the background so we can heartbeat alongside it.
  # --allowed-tools is comma-separated. Read-only set; the prompt also asks
  # the model not to modify files but the tool list is the contract.
  # stderr is captured to tmp_err so a nonzero exit can include the reason.
  #
  # Output format depends on whether jq is present: with jq we use json
  # mode and capture a usage event for the metrics tally; without jq we
  # fall back to text mode so the overview still renders. Decision is
  # made once, at invocation time.
  local fmt="text"
  if command -v jq >/dev/null 2>&1; then
    fmt="json"
  fi

  claude -p "$prompt" \
    --output-format "$fmt" \
    --model claude-haiku-4-5 \
    --allowed-tools "Read,Glob,Grep,Bash" \
    > "$tmp_out" 2> "$tmp_err" &
  local claude_pid=$!

  local elapsed=0
  local exit_code=0
  while :; do
    # Bash's `wait -t` isn't portable; poll instead.
    if ! kill -0 "$claude_pid" 2>/dev/null; then
      # `|| exit_code=$?` keeps `set -e` from killing us when claude errored.
      wait "$claude_pid" || exit_code=$?
      break
    fi
    if [ "$elapsed" -ge "$timeout_secs" ]; then
      kill -TERM "$claude_pid" 2>/dev/null || true
      sleep 2
      kill -KILL "$claude_pid" 2>/dev/null || true
      wait "$claude_pid" 2>/dev/null || true
      echo ":: ⚠ overview skipped: timeout after ${timeout_secs}s" >&2
      rm -f "$tmp_out" "$tmp_err"
      return 0
    fi
    sleep "$heartbeat_secs"
    elapsed=$((elapsed + heartbeat_secs))
    # Only print the heartbeat if claude is still running after the sleep.
    if kill -0 "$claude_pid" 2>/dev/null; then
      echo ":: overview: still running (${elapsed}s elapsed)" >&2
    fi
  done

  if [ "$exit_code" -ne 0 ]; then
    echo ":: ⚠ overview skipped: claude exited ${exit_code}" >&2
    # Surface the first few lines of stderr AND stdout so the user can see
    # why. claude -p emits auth/usage failures on stdout in some versions,
    # so checking only stderr would miss them.
    for src in "$tmp_err" "$tmp_out"; do
      if [ -s "$src" ]; then
        head -3 "$src" | sed 's/^/::   /' >&2
      fi
    done
    rm -f "$tmp_out" "$tmp_err"
    return 0
  fi

  # If we ran in json mode, split out the response text and emit a usage
  # event for the metrics tally. Replace tmp_out with the text so the
  # downstream renderer sees the same shape it always has.
  if [ "$fmt" = "json" ]; then
    local text_path="$tmp_out.text"
    if jq -er '.result // empty' "$tmp_out" > "$text_path" 2>/dev/null \
        && [ -s "$text_path" ]; then
      local events_file="${BACK2BASE_RUNTIME_DIR:-/run/back2base/power-steering}/api-events.jsonl"
      mkdir -p "$(dirname "$events_file")"
      jq -c --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
            --arg src "overview" \
            --arg model "claude-haiku-4-5" '{
        ts: $ts,
        source: $src,
        model: $model,
        input_tokens: (.usage.input_tokens // 0),
        output_tokens: (.usage.output_tokens // 0),
        cache_creation_input_tokens: (.usage.cache_creation_input_tokens // 0),
        cache_read_input_tokens: (.usage.cache_read_input_tokens // 0)
      }' "$tmp_out" >> "$events_file" 2>/dev/null || true
      mv "$text_path" "$tmp_out"
    else
      # Couldn't parse; leave tmp_out as-is and let the empty-check below
      # bail. This handles the case where claude -p emitted a non-result
      # JSON shape (e.g. error envelope).
      rm -f "$text_path"
    fi
  fi

  # Reject empty / whitespace-only output.
  if ! grep -q '[^[:space:]]' "$tmp_out" 2>/dev/null; then
    echo ":: ⚠ overview skipped: empty output" >&2
    rm -f "$tmp_out" "$tmp_err"
    return 0
  fi

  if ! python3 "$renderer" "$tmp_out" "$claude_md" 2>/dev/null; then
    echo ":: ⚠ overview render failed; CLAUDE.md unchanged" >&2
    rm -f "$tmp_out" "$tmp_err"
    return 0
  fi

  local lines
  lines=$(wc -l < "$tmp_out" | tr -d ' ')
  echo ":: overview: ${lines} lines, ${elapsed}s" >&2
  rm -f "$tmp_out" "$tmp_err"
  return 0
}
