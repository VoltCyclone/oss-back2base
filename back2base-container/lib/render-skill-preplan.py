#!/usr/bin/env python3
"""Skill preplanning — fingerprint the workspace, splice a skill block into
~/.claude/CLAUDE.md.

For each skill markdown file in --skills-dir we parse its YAML frontmatter
to find the `paths:` glob list and `description:`. Skills whose paths match
files in --workspace are ranked: top 2 with strong evidence go in as full
inlined bodies (high tier); next few with at least one match go in as
one-line pointers (medium tier). The block is spliced into --claude-md
between HTML-comment markers, atomically.

Designed to be tolerant of edge cases — every error path returns 0 with
a stderr warning, so a fingerprint failure never blocks container startup.

Usage:
  render-skill-preplan.py --workspace /workspace \\
                          --skills-dir ~/.claude/skills \\
                          --claude-md ~/.claude/CLAUDE.md
"""

import argparse
import os
import pathlib
import re
import signal
import sys
import tempfile

BEGIN = "<!-- back2base:skill-preplan-begin -->"
END = "<!-- back2base:skill-preplan-end -->"
HEADER = "## Preplanned skills (workspace fingerprint)"

# Tunables. Conservative defaults — tighten via env vars if needed.
MAX_HIGH_TIER = 2  # full bodies inlined
MAX_MEDIUM_TIER = 4  # one-line pointers
MATCH_CAP_PER_SKILL = 5  # stop globbing once a skill has this many matches
HIGH_TIER_THRESHOLD = 2  # ≥ this many matches OR a root-level match → high
TIMEOUT_SECS = 5  # signal.alarm bound on the whole detection pass

# Pruned glob roots — patterns starting with these prefixes are skipped.
# Empty workspace dirs typically don't contain these, but real repos do.
GLOB_PRUNE = ("node_modules/", ".git/", "vendor/", "dist/", "build/", "target/", ".venv/", "venv/", "__pycache__/")


# ── Frontmatter ──────────────────────────────────────────────────────────


def parse_frontmatter(text: str) -> dict:
    """Minimal YAML frontmatter parser — only handles the flat key:value
    style used by back2base skill files. Returns {} on malformed input."""
    if not text.startswith("---\n"):
        return {}
    end = text.find("\n---\n", 4)
    if end < 0:
        end = text.find("\n---", 4)
        if end < 0:
            return {}
    body = text[4:end]
    out = {}
    for line in body.split("\n"):
        if not line or line.startswith("#"):
            continue
        idx = line.find(":")
        if idx < 0:
            continue
        # Skip lines that look like list items or nested values.
        if line[0] in (" ", "\t", "-"):
            continue
        key = line[:idx].strip()
        val = line[idx + 1:].strip()
        # Strip surrounding quotes.
        if len(val) >= 2 and val[0] == val[-1] and val[0] in ("'", '"'):
            val = val[1:-1]
        out[key] = val
    return out


def strip_frontmatter(text: str) -> str:
    """Return the skill body with the leading YAML block removed."""
    if not text.startswith("---\n"):
        return text
    end = text.find("\n---\n", 4)
    if end < 0:
        return text
    return text[end + 5:].lstrip("\n")


# ── Globbing ─────────────────────────────────────────────────────────────


def _path_pruned(rel: str) -> bool:
    """True if a relative path falls under a heavy/uninteresting subtree."""
    parts = rel.split(os.sep)
    return any(p in GLOB_PRUNE or (p + "/") in GLOB_PRUNE for p in parts)


def count_matches(workspace: pathlib.Path, paths_csv: str, cap: int = MATCH_CAP_PER_SKILL) -> tuple[int, bool]:
    """Count files in `workspace` matching any glob in the CSV, up to cap.

    Returns (count, root_match) where root_match is True if at least one
    match sits at the workspace root (no separator in the relative path).
    """
    if not paths_csv:
        return 0, False
    patterns = [p.strip() for p in paths_csv.split(",") if p.strip()]
    matched = 0
    root = False
    seen: set[str] = set()
    for pat in patterns:
        try:
            iterator = workspace.glob(pat)
        except (ValueError, OSError):
            continue
        for path in iterator:
            try:
                rel = str(path.relative_to(workspace))
            except ValueError:
                continue
            if rel in seen:
                continue
            if _path_pruned(rel):
                continue
            seen.add(rel)
            matched += 1
            if os.sep not in rel:
                root = True
            if matched >= cap:
                return matched, root
    return matched, root


# ── Skill loading ────────────────────────────────────────────────────────


class Skill:
    __slots__ = ("name", "paths", "description", "body", "match_count", "root_match")

    def __init__(self, name: str, paths: str, description: str, body: str):
        self.name = name
        self.paths = paths
        self.description = description
        self.body = body
        self.match_count = 0
        self.root_match = False


def load_skills(skills_dir: pathlib.Path) -> list[Skill]:
    out: list[Skill] = []
    if not skills_dir.is_dir():
        return out
    for entry in sorted(skills_dir.iterdir()):
        if not entry.is_file() or entry.suffix != ".md":
            continue
        try:
            text = entry.read_text(encoding="utf-8", errors="replace")
        except OSError:
            continue
        fm = parse_frontmatter(text)
        if not fm:
            continue
        name = fm.get("name") or entry.stem
        paths = fm.get("paths", "")
        desc = fm.get("description", "")
        if not paths:
            continue  # skills with no path declarations can't be auto-detected
        body = strip_frontmatter(text)
        out.append(Skill(name=name, paths=paths, description=desc, body=body))
    return out


# ── Selection ────────────────────────────────────────────────────────────


def select_skills(skills: list[Skill], workspace: pathlib.Path) -> tuple[list[Skill], list[Skill]]:
    """Probe each skill against the workspace, return (high_tier, medium_tier)."""
    for skill in skills:
        skill.match_count, skill.root_match = count_matches(workspace, skill.paths)

    # Eligible = at least one match.
    eligible = [s for s in skills if s.match_count > 0]
    # Sort: root-match first, then by match count desc, then name asc (stable).
    eligible.sort(key=lambda s: (not s.root_match, -s.match_count, s.name))

    high: list[Skill] = []
    medium: list[Skill] = []
    for skill in eligible:
        is_high = skill.match_count >= HIGH_TIER_THRESHOLD or skill.root_match
        if is_high and len(high) < MAX_HIGH_TIER:
            high.append(skill)
        elif len(medium) < MAX_MEDIUM_TIER:
            medium.append(skill)
        if len(high) >= MAX_HIGH_TIER and len(medium) >= MAX_MEDIUM_TIER:
            break
    return high, medium


# ── Block rendering ──────────────────────────────────────────────────────


def render_block(high: list[Skill], medium: list[Skill]) -> str:
    if not high and not medium:
        return ""
    parts = [BEGIN, HEADER, ""]
    if high:
        parts.append("### Pre-loaded — apply without invoking the Skill tool")
        parts.append("")
        for skill in high:
            parts.append("---")
            parts.append("")
            parts.append(f"**Skill: {skill.name}**")
            parts.append("")
            parts.append(skill.body.rstrip())
            parts.append("")
        parts.append("---")
        parts.append("")
    if medium:
        parts.append("### Pointers — invoke via Skill tool when needed")
        parts.append("")
        for skill in medium:
            desc = skill.description or "(see skill body)"
            parts.append(f"- `{skill.name}`: {desc}")
        parts.append("")
    parts.append(END)
    return "\n".join(parts) + "\n"


# ── Splicing ─────────────────────────────────────────────────────────────


def splice(claude_md: str, block: str) -> str:
    """Replace existing block, or append a new one. If block is empty,
    strip any existing block (keeps idempotency)."""
    pattern = re.compile(
        re.escape(BEGIN) + r".*?" + re.escape(END) + r"\n?",
        re.DOTALL,
    )
    if not block:
        if pattern.search(claude_md):
            return pattern.sub("", claude_md, count=1)
        return claude_md
    if pattern.search(claude_md):
        return pattern.sub(block, claude_md, count=1)
    if not claude_md.endswith("\n"):
        claude_md += "\n"
    return claude_md + "\n" + block


def atomic_write(path: pathlib.Path, content: str) -> None:
    target_dir = str(path.parent) or "."
    fd, tmp_path = tempfile.mkstemp(prefix="." + path.name + ".", dir=target_dir)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as out:
            out.write(content)
        os.replace(tmp_path, path)
    except Exception:
        try:
            os.unlink(tmp_path)
        except FileNotFoundError:
            pass
        raise


# ── Main ─────────────────────────────────────────────────────────────────


class _TimeoutError(Exception):
    pass


def _on_alarm(_sig, _frame):
    raise _TimeoutError()


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description="Skill preplanning splicer")
    parser.add_argument("--workspace", required=True, help="Repo to fingerprint")
    parser.add_argument("--skills-dir", required=True, help="~/.claude/skills directory")
    parser.add_argument("--claude-md", required=True, help="CLAUDE.md to splice into")
    parser.add_argument("--timeout", type=int, default=TIMEOUT_SECS,
                        help="Hard timeout for detection (seconds)")
    args = parser.parse_args(argv[1:])

    workspace = pathlib.Path(args.workspace)
    skills_dir = pathlib.Path(args.skills_dir)
    claude_md_path = pathlib.Path(args.claude_md)

    if not claude_md_path.is_file():
        print(f":: ⚠ skill-preplan: CLAUDE.md missing ({claude_md_path})", file=sys.stderr)
        return 0
    if not workspace.is_dir():
        print(f":: ⚠ skill-preplan: workspace missing ({workspace})", file=sys.stderr)
        return 0

    # Run detection under a hard timeout.
    if hasattr(signal, "SIGALRM"):
        signal.signal(signal.SIGALRM, _on_alarm)
        signal.alarm(max(1, args.timeout))
    try:
        skills = load_skills(skills_dir)
        high, medium = select_skills(skills, workspace)
        block = render_block(high, medium)
    except _TimeoutError:
        print(f":: ⚠ skill-preplan: detection timed out after {args.timeout}s; skipping",
              file=sys.stderr)
        return 0
    except Exception as exc:  # any other failure is non-fatal
        print(f":: ⚠ skill-preplan: detection failed ({exc!r}); skipping", file=sys.stderr)
        return 0
    finally:
        if hasattr(signal, "SIGALRM"):
            signal.alarm(0)

    try:
        existing = claude_md_path.read_text(encoding="utf-8")
    except OSError as exc:
        print(f":: ⚠ skill-preplan: cannot read CLAUDE.md ({exc!r}); skipping", file=sys.stderr)
        return 0

    new_content = splice(existing, block)
    if new_content == existing:
        return 0

    try:
        atomic_write(claude_md_path, new_content)
    except OSError as exc:
        print(f":: ⚠ skill-preplan: write failed ({exc!r}); CLAUDE.md unchanged", file=sys.stderr)
        return 0

    if high or medium:
        names_high = ",".join(s.name for s in high)
        names_med = ",".join(s.name for s in medium)
        msg = f":: skill-preplan: high=[{names_high}] medium=[{names_med}]"
        print(msg, file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
