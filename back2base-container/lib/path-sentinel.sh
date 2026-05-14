#!/usr/bin/env bash
# back2base path sentinel
#
# entrypoint.sh aligns Claude Code's auto-derived memory directory by
# symlinking it into the namespaced location. The derivation rule it
# assumes is: `<projects>/<PWD with each '/' replaced by '-'>/memory`.
# If Anthropic ever changes that convention (URL-encoding, hashing,
# different separator), our symlink lands at the wrong path and "memory
# doesn't load" silently.
#
# This sentinel runs in the background ~30s after entrypoint hands off to
# claude. By then Claude Code has had time to write its session JSONL.
# The sentinel checks ~/.claude/projects/ for evidence:
#
#   PASS  — a directory at the expected dir name exists (real or symlink).
#   FAIL  — expected dir absent, but some other directory contains *.jsonl.
#           Drops ~/.claude/.path-sentinel-mismatch and warns to stderr.
#   NOOP  — no project dirs (or none with jsonl). Claude Code probably
#           hasn't written yet — say nothing.
#
# Tests drive the script via --dry-run plus --projects-dir/--cwd to inject
# fixtures without touching $HOME/.claude/projects.

set -u

# Defaults — can be overridden via flags or environment.
SENTINEL_DELAY_SEC="${SENTINEL_DELAY_SEC:-30}"

DRY_RUN=0
PROJECTS_DIR=""
CWD_OVERRIDE=""

while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run)        DRY_RUN=1 ;;
    --projects-dir)   PROJECTS_DIR="$2"; shift ;;
    --cwd)            CWD_OVERRIDE="$2"; shift ;;
    *) echo "path-sentinel: unknown arg: $1" >&2; exit 2 ;;
  esac
  shift
done

[ -z "$PROJECTS_DIR" ] && PROJECTS_DIR="$HOME/.claude/projects"

# Capture cwd at script-start (before any sleep) so a later `cd` from
# claude itself can't change what we verify.
if [ -n "$CWD_OVERRIDE" ]; then
  EFFECTIVE_PWD="$CWD_OVERRIDE"
else
  EFFECTIVE_PWD="$PWD"
fi

EXPECTED_DIR_NAME="${EFFECTIVE_PWD//\//-}"

_iso_now() { date -u +%Y-%m-%dT%H:%M:%SZ; }

# _evaluate — run the actual check and emit PASS / FAIL / NOOP to stdout.
# Side effects: writes ~/.claude/.path-sentinel-{ok,mismatch}.
_evaluate() {
  local marker_dir="$HOME/.claude"
  local ok_marker="$marker_dir/.path-sentinel-ok"
  local mismatch_marker="$marker_dir/.path-sentinel-mismatch"
  mkdir -p "$marker_dir" 2>/dev/null || true

  # No projects dir → Claude Code never wrote. NOOP.
  if [ ! -d "$PROJECTS_DIR" ]; then
    echo "NOOP path-sentinel: $PROJECTS_DIR does not exist"
    return 0
  fi

  # PASS: expected dir exists (real dir or symlink, even if dangling).
  local expected_path="$PROJECTS_DIR/$EXPECTED_DIR_NAME"
  if [ -d "$expected_path" ] || [ -L "$expected_path" ]; then
    {
      echo "ok=$(_iso_now)"
      echo "expected=$EXPECTED_DIR_NAME"
      echo "cwd=$EFFECTIVE_PWD"
    } > "$ok_marker" 2>/dev/null || true
    echo "PASS path-sentinel: found $expected_path"
    return 0
  fi

  # No expected dir. Look for sibling dirs with .jsonl content — that
  # signals Claude Code wrote somewhere unexpected.
  local found="" entry name
  for entry in "$PROJECTS_DIR"/*; do
    [ -d "$entry" ] || continue
    name="$(basename "$entry")"
    case "$name" in .*) continue ;; esac
    # Any *.jsonl file (top level or nested one level deep) qualifies.
    if compgen -G "$entry/*.jsonl" >/dev/null 2>&1 \
       || compgen -G "$entry/*/*.jsonl" >/dev/null 2>&1; then
      if [ -z "$found" ]; then
        found="$name"
      else
        found="$found,$name"
      fi
    fi
  done

  if [ -z "$found" ]; then
    echo "NOOP path-sentinel: no Claude Code session jsonls under $PROJECTS_DIR yet"
    return 0
  fi

  {
    echo "mismatch=$(_iso_now)"
    echo "expected=$EXPECTED_DIR_NAME"
    echo "actual=$found"
    echo "cwd=$EFFECTIVE_PWD"
    echo "projects_dir=$PROJECTS_DIR"
  } > "$mismatch_marker" 2>/dev/null || true

  echo "FAIL path-sentinel: expected '$EXPECTED_DIR_NAME' but found '$found' under $PROJECTS_DIR" >&2
  echo "FAIL path-sentinel: Claude Code may have changed its project-dir naming convention." >&2
  echo "FAIL path-sentinel: memory autoload via symlinked dir will NOT work; see $mismatch_marker" >&2
  echo "FAIL expected=$EXPECTED_DIR_NAME actual=$found"
  return 0
}

if [ "$DRY_RUN" = "1" ]; then
  _evaluate
  exit 0
fi

# Production path: wait, then evaluate, then exit. Daemon-style — the
# entrypoint launches us with `& disown` and we go away when claude exits.
sleep "$SENTINEL_DELAY_SEC"
_evaluate >/dev/null
exit 0
