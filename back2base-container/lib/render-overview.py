#!/usr/bin/env python3
"""Splice a pre-generated overview file into ~/.claude/CLAUDE.md between
HTML-comment markers.

Usage: render-overview.py <overview-file> <claude-md-path>

- If markers exist in the target, replace everything between them.
- If markers are absent, append a new section at the end.
- Atomic write via os.replace.
- Idempotent: running twice with the same input yields the same file.
"""
import os
import re
import sys
import tempfile

BEGIN = "<!-- back2base:overview-begin -->"
END = "<!-- back2base:overview-end -->"
HEADER = "## Repo overview (this session)"


def build_section(body: str) -> str:
    body = body.strip("\n")
    return f"{BEGIN}\n{HEADER}\n\n{body}\n{END}\n"


def splice(claude_md: str, body: str) -> str:
    section = build_section(body)
    pattern = re.compile(
        re.escape(BEGIN) + r".*?" + re.escape(END) + r"\n?",
        re.DOTALL,
    )
    if pattern.search(claude_md):
        return pattern.sub(section, claude_md, count=1)
    if not claude_md.endswith("\n"):
        claude_md += "\n"
    return claude_md + "\n" + section


def main(argv: list[str]) -> int:
    if len(argv) != 3:
        print(f"usage: {argv[0]} <overview-file> <claude-md-path>", file=sys.stderr)
        return 2

    overview_path, claude_md_path = argv[1], argv[2]
    with open(overview_path, "r", encoding="utf-8") as f:
        body = f.read()
    with open(claude_md_path, "r", encoding="utf-8") as f:
        existing = f.read()

    new_content = splice(existing, body)

    target_dir = os.path.dirname(os.path.abspath(claude_md_path)) or "."
    fd, tmp_path = tempfile.mkstemp(prefix=".CLAUDE.md.", dir=target_dir)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as out:
            out.write(new_content)
        os.replace(tmp_path, claude_md_path)
    except Exception:
        try:
            os.unlink(tmp_path)
        except FileNotFoundError:
            pass
        raise
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
