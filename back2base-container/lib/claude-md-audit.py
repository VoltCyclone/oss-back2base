#!/usr/bin/env python3
"""back2base CLAUDE.md staleness audit helper.

Two subcommands:
  tally   — called by PostToolUse hook with the tool payload on stdin.
            Appends Edit/Write/MultiEdit events to edits.jsonl. No-op
            for irrelevant tools.
  decide  — called by UserPromptSubmit hook. Reads edits.jsonl +
            audit-state.json + drift-state.json; if thresholds suggest
            CLAUDE.md may be stale and no cooldown blocks, prints a
            system-reminder block to stdout and updates audit-state.json.

State directory: $POWER_STEERING_DIR (default /run/back2base/power-steering).
"""
import json
import os
import re
import sys
import time
from pathlib import Path

RELEVANT_TOOLS = {"Edit", "Write", "MultiEdit"}

STRUCTURAL_BASENAMES = {
    "Makefile",
    "Dockerfile",
    "package.json",
    "go.mod",
    "go.sum",
    "pyproject.toml",
    "Cargo.toml",
}


def state_dir() -> Path:
    return Path(os.environ.get("POWER_STEERING_DIR", "/run/back2base/power-steering"))


def _is_disabled() -> bool:
    val = os.environ.get("BACK2BASE_HOOK_CLAUDE_MD_AUDIT", "").lower()
    return val in {"off", "0", "false", "no"}


def _read_edits(sd: Path) -> list[dict]:
    path = sd / "edits.jsonl"
    if not path.exists():
        return []
    out: list[dict] = []
    for line in path.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            obj = json.loads(line)
        except json.JSONDecodeError:
            continue
        if isinstance(obj, dict):
            out.append(obj)
    return out


def _config() -> dict:
    def _int(name: str, default: int) -> int:
        try:
            return int(os.environ.get(name, default))
        except (TypeError, ValueError):
            return default
    return {
        "every_n_edits": _int("BACK2BASE_CLAUDE_MD_AUDIT_EVERY_N_EDITS", 20),
        "referenced_threshold": _int("BACK2BASE_CLAUDE_MD_AUDIT_REFERENCED_THRESHOLD", 3),
        "structural_min_edits": _int("BACK2BASE_CLAUDE_MD_AUDIT_STRUCTURAL_MIN_EDITS", 5),
        "suggest_cooldown_sec": _int("BACK2BASE_CLAUDE_MD_AUDIT_SUGGEST_COOLDOWN_SEC", 600),
        "defer_after_drift_sec": _int("BACK2BASE_CLAUDE_MD_AUDIT_DEFER_AFTER_DRIFT_SEC", 600),
    }


def _load_state(sd: Path) -> dict:
    path = sd / "audit-state.json"
    if not path.exists():
        return {}
    try:
        obj = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError:
        return {}
    return obj if isinstance(obj, dict) else {}


def _save_state(sd: Path, state: dict) -> None:
    sd.mkdir(parents=True, exist_ok=True)
    tmp = sd / "audit-state.json.tmp"
    tmp.write_text(json.dumps(state, separators=(",", ":")), encoding="utf-8")
    tmp.replace(sd / "audit-state.json")


def _format_suggestion(n_edits: int, n_referenced: int, sample_paths: list[str]) -> str:
    sample = ", ".join(sample_paths[:3]) if sample_paths else ""
    sample_clause = f", {n_referenced} touching files referenced in CLAUDE.md ({sample})" if sample else ""
    return (
        "## CLAUDE.md staleness audit suggested\n\n"
        f"{n_edits} edits this session{sample_clause}. "
        "Consider running `/revise-claude-md` from the claude-md-management "
        "plugin to audit CLAUDE.md against the current project state.\n\n"
        "Defer if mid-task. This suggestion will not repeat for ~10 minutes.\n"
    )


_BACKTICK_RE = re.compile(r"`([^`]+)`")
_CLAUDE_MD_GLOB_DEPTH = 6


def _find_claude_md_files(root: Path) -> list[Path]:
    out: list[Path] = []
    for depth in range(_CLAUDE_MD_GLOB_DEPTH + 1):
        pattern = "/".join(["*"] * depth + ["CLAUDE.md"]) if depth else "CLAUDE.md"
        out.extend(p for p in root.glob(pattern) if p.is_file())
    return out


def _extract_referenced_paths(claude_md_files: list[Path]) -> list[str]:
    refs: set[str] = set()
    for f in claude_md_files:
        try:
            text = f.read_text(encoding="utf-8", errors="replace")
        except OSError:
            continue
        for m in _BACKTICK_RE.finditer(text):
            token = m.group(1).strip()
            if not token or " " in token or len(token) > 200:
                continue
            refs.add(token)
    return sorted(refs)


def _touched_structural(edits: list[dict]) -> bool:
    for e in edits:
        fp = e.get("file") or ""
        if Path(fp).name in STRUCTURAL_BASENAMES:
            return True
    return False


def _path_matches_ref(file_path: str, ref: str) -> bool:
    if ref in file_path:
        return True
    if ref.startswith("*."):
        return file_path.endswith(ref[1:])
    return False


def _max_claude_md_mtime(root: Path) -> tuple[float, list[str]]:
    files = _find_claude_md_files(root)
    if not files:
        return 0.0, []
    mtimes = [(f.stat().st_mtime, str(f)) for f in files]
    return max(m[0] for m in mtimes), [m[1] for m in mtimes]


def _last_drift_ts(sd: Path) -> int:
    path = sd / "drift-state.json"
    if not path.exists():
        return 0
    try:
        obj = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError:
        return 0
    if not isinstance(obj, dict):
        return 0
    val = obj.get("last_drift_ts", 0)
    try:
        return int(val)
    except (TypeError, ValueError):
        return 0


def cmd_tally(_argv: list[str]) -> int:
    raw = sys.stdin.read()
    try:
        payload = json.loads(raw)
    except json.JSONDecodeError:
        return 0
    if not isinstance(payload, dict):
        return 0
    tool = payload.get("tool_name")
    if tool not in RELEVANT_TOOLS:
        return 0
    tool_input = payload.get("tool_input") or {}
    file_path = tool_input.get("file_path") if isinstance(tool_input, dict) else None
    if not file_path:
        return 0
    sd = state_dir()
    sd.mkdir(parents=True, exist_ok=True)
    entry = {"ts": int(time.time()), "tool": tool, "file": file_path}
    with (sd / "edits.jsonl").open("a", encoding="utf-8") as f:
        f.write(json.dumps(entry, separators=(",", ":")) + "\n")
    return 0


def cmd_decide(_argv: list[str]) -> int:
    if _is_disabled():
        return 0
    sd = state_dir()
    now = int(time.time())
    cfg = _config()
    last_drift = _last_drift_ts(sd)
    if last_drift and now - last_drift < cfg["defer_after_drift_sec"]:
        return 0
    edits = _read_edits(sd)
    if not edits:
        return 0
    state = _load_state(sd)

    cwd = Path.cwd()
    current_mtime, _ = _max_claude_md_mtime(cwd)
    snapshot_mtime = float(state.get("claude_md_mtime", 0) or 0)
    if current_mtime and snapshot_mtime and current_mtime > snapshot_mtime:
        # Audit appears to have happened: rebase the counter and refresh refs.
        # Reuse the `edits` snapshot from above so we don't open the same
        # append-only file twice (closes a narrow TOCTOU with the tally hook).
        state["edits_at_last_reset"] = len(edits)
        state["claude_md_mtime"] = current_mtime
        state["referenced_paths"] = _extract_referenced_paths(_find_claude_md_files(cwd))
        state["last_suggested_ts"] = 0
        _save_state(sd, state)
        return 0

    last_suggested = int(state.get("last_suggested_ts", 0) or 0)
    if last_suggested and now - last_suggested < cfg["suggest_cooldown_sec"]:
        return 0
    edits_at_reset = int(state.get("edits_at_last_reset", 0) or 0)
    relevant = edits[edits_at_reset:]
    n_edits = len(relevant)

    refs = state.get("referenced_paths")
    if not isinstance(refs, list):
        claude_md_files = _find_claude_md_files(Path.cwd())
        refs = _extract_referenced_paths(claude_md_files)
        state["referenced_paths"] = refs

    matched: list[str] = []
    for e in relevant:
        fp = e.get("file") or ""
        for ref in refs:
            if _path_matches_ref(fp, ref):
                matched.append(fp)
                break
    n_referenced = len(matched)

    structural = _touched_structural(relevant)
    trigger = (
        n_edits >= cfg["every_n_edits"]
        or n_referenced >= cfg["referenced_threshold"]
        or (structural and n_edits >= cfg["structural_min_edits"])
    )
    if not trigger:
        _save_state(sd, state)
        return 0

    suggestion = _format_suggestion(n_edits, n_referenced, matched)
    sys.stdout.write(suggestion)
    if not state.get("claude_md_mtime"):
        state["claude_md_mtime"] = current_mtime
    state["last_suggested_ts"] = now
    _save_state(sd, state)
    return 0


def main(argv: list[str]) -> int:
    if len(argv) < 2:
        sys.stderr.write("usage: claude-md-audit.py {tally,decide} [args...]\n")
        return 2
    sub = argv[1]
    if sub == "tally":
        return cmd_tally(argv[2:])
    if sub == "decide":
        return cmd_decide(argv[2:])
    sys.stderr.write(f"unknown subcommand: {sub}\n")
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))
