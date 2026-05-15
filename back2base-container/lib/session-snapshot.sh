#!/usr/bin/env bash
# back2base session-snapshot daemon
#
# Periodically copies the active Claude Code session JSONL under
# ~/.claude/projects/$MEMORY_NAMESPACE/<sessionId>/<file>.jsonl to
# .snapshots/<ISO>.jsonl, so a hard crash mid-write cannot block resume.
#
# Sourced by test/session-snapshot.bats; main_loop only runs when invoked
# as a script.
#
# Required tools (all in back2base-base + macOS dev hosts): bash 4+, jq.
# Uses portable wrappers for sha256 and ISO timestamp generation.

set -u

LOG="${HOME}/.claude/.back2base-snapshot.log"
INTERVAL="${SNAPSHOT_INTERVAL_SEC:-300}"
ACTIVE="${ACTIVE_WINDOW_SEC:-600}"
KEEP="${SNAPSHOT_KEEP:-5}"
COLD_DAYS="${SESSION_COLD_DAYS:-30}"

# Portable sha256: GNU coreutils ships sha256sum; macOS ships shasum.
if command -v sha256sum >/dev/null 2>&1; then
  _sha256() { sha256sum "$1" | awk '{print $1}'; }
else
  _sha256() { shasum -a 256 "$1" | awk '{print $1}'; }
fi

# Portable ISO-8601 UTC timestamp (e.g. 2026-04-27T19:30:00Z). GNU `date -Is`
# is not available on macOS; the explicit format string works on both.
_iso_now() { date -u +%Y-%m-%dT%H:%M:%SZ; }

log() { mkdir -p "$(dirname "$LOG")"; echo "$(_iso_now) $*" >> "$LOG"; }

# validate_jsonl <file> — return 0 iff last \n-terminated record parses as JSON.
validate_jsonl() {
  local f="$1"
  [ -s "$f" ] || return 1
  local last
  last=$(tail -c 65536 "$f" | awk 'BEGIN{RS="\n"} NF{last=$0} END{print last}')
  [ -n "$last" ] || return 1
  printf '%s' "$last" | jq -e . >/dev/null 2>&1
}

# snapshot_session <sessionDir>
snapshot_session() {
  local sd="$1"
  local live
  live=$(ls -t "$sd"/*.jsonl 2>/dev/null | head -n1)
  [ -n "$live" ] || return 0
  validate_jsonl "$live" || { log "skip corrupt: $live"; return 0; }

  local snap_dir="$sd/.snapshots"
  mkdir -p "$snap_dir" || { log "mkdir fail: $snap_dir"; return 0; }

  # Skip if the newest snapshot has identical content.
  local newest_snap cur_hash last_hash
  newest_snap=$(ls -t "$snap_dir"/*.jsonl 2>/dev/null | head -n1)
  cur_hash=$(_sha256 "$live")
  if [ -n "$newest_snap" ]; then
    last_hash=$(_sha256 "$newest_snap")
    if [ "$cur_hash" = "$last_hash" ]; then
      return 0
    fi
  fi

  local stamp tmp out
  # Use hyphens (not colons) inside the timestamp so the filename is portable
  # across filesystems that disallow colons.
  stamp=$(date -u +%Y-%m-%dT%H-%M-%SZ)
  tmp="$snap_dir/.${stamp}.jsonl.tmp"
  out="$snap_dir/${stamp}.jsonl"
  cp "$live" "$tmp" || { log "copy fail: $live"; rm -f "$tmp"; return 0; }
  mv "$tmp" "$out" || { log "rename fail: $tmp"; rm -f "$tmp"; return 0; }
  log "snapshot: $out"

  # Retain newest $KEEP only.
  local total
  total=$(ls -1 "$snap_dir"/*.jsonl 2>/dev/null | wc -l)
  if [ "$total" -gt "$KEEP" ]; then
    ls -t "$snap_dir"/*.jsonl | tail -n +"$((KEEP+1))" | while IFS= read -r f; do
      rm -f "$f"
    done
  fi
}

# gc_cold_sessions <namespaceDir>
# For each non-hidden session dir directly under ns_dir whose newest .jsonl
# is older than COLD_DAYS days, remove its .snapshots/ subdirectory.
gc_cold_sessions() {
  local ns_dir="$1"
  [ -d "$ns_dir" ] || return 0
  local sd live
  while IFS= read -r sd; do
    live=$(ls -t "$sd"/*.jsonl 2>/dev/null | head -n1)
    [ -n "$live" ] || continue
    if [ -n "$(find "$live" -mtime +"$COLD_DAYS" -print -quit 2>/dev/null)" ]; then
      if [ -d "$sd/.snapshots" ]; then
        rm -rf "$sd/.snapshots" && log "cold gc: $sd"
      fi
    fi
  done < <(find "$ns_dir" -maxdepth 1 -mindepth 1 -type d ! -name '.*' 2>/dev/null)
}

# main_loop — periodically snapshots active sessions and runs cold GC.
# When ONE_SHOT is set, executes the per-tick body once and returns
# (used by tests).
main_loop() {
  local ns_dir="$HOME/.claude/projects/${MEMORY_NAMESPACE:-}"
  [ -n "${MEMORY_NAMESPACE:-}" ] || { log "MEMORY_NAMESPACE unset; daemon idle"; return 0; }
  mkdir -p "$ns_dir"

  while :; do
    local sd live age now
    now=$(date +%s)
    while IFS= read -r sd; do
      live=$(ls -t "$sd"/*.jsonl 2>/dev/null | head -n1)
      [ -n "$live" ] || continue
      if stat -c %Y "$live" >/dev/null 2>&1; then
        age=$(( now - $(stat -c %Y "$live") ))
      else
        age=$(( now - $(stat -f %m "$live") ))
      fi
      [ "$age" -le "$ACTIVE" ] && snapshot_session "$sd"
    done < <(find "$ns_dir" -maxdepth 1 -mindepth 1 -type d ! -name '.*' 2>/dev/null)

    gc_cold_sessions "$ns_dir"

    [ -n "${ONE_SHOT:-}" ] && return 0
    sleep "$INTERVAL"
  done
}

# Run only when invoked as a script. When sourced (by tests), do nothing.
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  main_loop
fi
