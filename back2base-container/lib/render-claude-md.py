#!/usr/bin/env python3
"""Render ~/.claude/CLAUDE.md from defaults/CLAUDE.md.template.

Replaces the bash + jq generate_claude_md function in entrypoint.sh with
a stdlib-only Python implementation. Three @GENERATED markers in the
template are replaced with rendered tables/sections; every other line
passes through unchanged.

Markers:
  * <!-- @GENERATED:MCP_SERVERS -->   → docker + binary/remote tables
  * <!-- @GENERATED:COMMANDS -->      → slash command table
  * <!-- @GENERATED:PROFILE_GUIDE --> → per-profile snippet (or marker
                                        line dropped entirely when the
                                        snippet file is missing)

Cache: if --output exists and every input file's mtime ≤ output's mtime,
exits 0 without writing (preserving mtime). Inputs are --template, --mcp,
and every *.md under --commands-dir.

Tolerant of edge cases (missing files, malformed JSON, etc.) — the
original bash version was, and a CLAUDE.md regen failure must never
block container startup.
"""

import argparse
import json
import os
import pathlib
import re
import sys
import tempfile


# ── MCP rendering ──────────────────────────────────────────────────────


def _env_note(env):
    r"""Build the ` — requires: \`X\`, \`Y\``-style suffix.

    Keys with empty/None values are filtered. Output is sorted
    alphabetically — matches the bash via jq's `to_entries` (which
    preserves order) plus a final sort by key for stability.
    """
    if not isinstance(env, dict):
        return ""
    keys = sorted(k for k, v in env.items() if v not in (None, ""))
    if not keys:
        return ""
    return " — requires: " + ", ".join(f"`{k}`" for k in keys)


def _docker_image(args):
    """Pick the Docker image arg for a `command: docker` server.

    Original bash:
      .args | map(select(test("^[a-z].*/"))) | first
      || [.args[] | select(test("^[a-zA-Z0-9._/-]+$") and (test("^-")|not))] | last
    """
    if not isinstance(args, list):
        return ""
    for a in args:
        if isinstance(a, str) and re.match(r"^[a-z].*/", a):
            return a
    fallback = ""
    for a in args:
        if (
            isinstance(a, str)
            and re.match(r"^[a-zA-Z0-9._/-]+$", a)
            and not a.startswith("-")
        ):
            fallback = a  # "last" match — keep overwriting
    return fallback


def _npx_pkg(args):
    """Pick the npx package arg.

    Original bash:
      .args | map(select(test("^@|^[a-z]") and (test("^-")|not))) | first // "npx"
    """
    if not isinstance(args, list):
        return "npx"
    for a in args:
        if (
            isinstance(a, str)
            and re.match(r"^(@|[a-z])", a)
            and not a.startswith("-")
        ):
            return a
    return "npx"


def render_mcp_section(mcp_path):
    if not mcp_path or not pathlib.Path(mcp_path).is_file():
        return f"_MCP server list unavailable: .mcp.json not found at `{mcp_path}`._"
    try:
        data = json.loads(pathlib.Path(mcp_path).read_text())
    except Exception:
        return "_MCP server list unavailable: .mcp.json contains malformed JSON._"

    servers = data.get("mcpServers") or {}
    if not isinstance(servers, dict):
        return "_MCP server list unavailable: .mcp.json missing top-level `mcpServers` object._"

    docker_rows = []
    binary_rows = []
    for name in sorted(servers.keys()):
        srv = servers[name] if isinstance(servers[name], dict) else {}
        transport = srv.get("type") or "stdio"
        command = srv.get("command") or ""
        args = srv.get("args") or []
        env = srv.get("env") or {}
        note = _env_note(env)

        if transport == "http":
            binary_rows.append(f"| `{name}` | HTTP (remote) |{note} |")
        elif command == "docker":
            image = _docker_image(args)
            docker_rows.append(f"| `{name}` | `{image}` |{note} |")
        elif command == "npx":
            pkg = _npx_pkg(args)
            bin_display = f"npx {pkg}"
            binary_rows.append(f"| `{name}` | `{bin_display}` |{note} |")
        else:
            bin_display = command
            if args and isinstance(args[0], str) and args[0]:
                bin_display = f"{command} {args[0]}"
            binary_rows.append(f"| `{name}` | `{bin_display}` |{note} |")

    out = ["**Docker containers** (ephemeral, pre-pulled at startup):", ""]
    out.append("| Server | Image | Notes |")
    out.append("|---|---|---|")
    out.extend(docker_rows)
    out.append("")
    out.append("**Binaries and remote** (no Docker overhead):")
    out.append("")
    out.append("| Server | Binary / Transport | Notes |")
    out.append("|---|---|---|")
    out.extend(binary_rows)
    # Trailing empty line: matches bash `printf '%b\n' "$mcp_section"` where
    # $mcp_section ends with "\n" (so the printf emits two newlines back-to-
    # back, leaving a blank line before whatever follows in the template).
    out.append("")
    return "\n".join(out)


# ── Commands rendering ─────────────────────────────────────────────────


_FRONTMATTER_DELIM = "---"


def _frontmatter_description(text):
    """Extract `description:` value from a `---`-delimited frontmatter block.

    Mirrors the awk in the bash:
      /^---/{c++;next} c==1 && /^description:/{ ... print; exit }
    Quote stripping removes a leading and trailing single OR double quote
    (one pair only — matches the bash sed).
    """
    lines = text.splitlines()
    if not lines or lines[0].strip() != _FRONTMATTER_DELIM:
        return ""
    for line in lines[1:]:
        if line.strip() == _FRONTMATTER_DELIM:
            return ""
        m = re.match(r"^description:[ \t]*(.*)$", line)
        if m:
            value = m.group(1)
            # Strip ONE leading and ONE trailing quote (single or double).
            # Match the awk's gsub(/^["\x27]|["\x27]$/,"") — does NOT trim
            # trailing whitespace, so we don't either.
            value = re.sub(r'^["\']', "", value)
            value = re.sub(r'["\']$', "", value)
            return value
    return ""


def _first_heading(text):
    for line in text.splitlines():
        if line.startswith("#"):
            stripped = re.sub(r"^#+\s*", "", line)
            return stripped[:80]
    return ""


def render_commands_section(commands_dir):
    if not commands_dir:
        return "_Commands directory not found._"
    p = pathlib.Path(commands_dir)
    if not p.is_dir():
        return "_Commands directory not found._"

    rows = []
    md_files = sorted(p.rglob("*.md"))
    for md in md_files:
        rel = md.relative_to(p)
        # /a/b/c.md → /a:b:c
        cmd_name = "/" + str(rel)[:-3].replace(os.sep, ":")
        try:
            text = md.read_text()
        except Exception:
            text = ""
        desc = _frontmatter_description(text)
        if not desc:
            desc = _first_heading(text)
        if not desc:
            desc = "—"
        rows.append(f"| `{cmd_name}` | {desc} |")

    if not rows:
        return "_No slash commands installed._"

    out = ["| Command | Description |", "|---|---|"]
    out.extend(rows)
    # Trailing empty line — see render_mcp_section for the rationale.
    out.append("")
    return "\n".join(out)


# ── Cache check ────────────────────────────────────────────────────────


def cache_fresh(output, inputs):
    """Return True if `output` exists and is at least as new as every input."""
    out = pathlib.Path(output)
    if not out.is_file():
        return False
    out_mtime = out.stat().st_mtime
    for inp in inputs:
        if inp is None:
            continue
        ip = pathlib.Path(inp)
        if not ip.exists():
            continue
        if ip.is_dir():
            for sub in ip.rglob("*.md"):
                try:
                    if sub.stat().st_mtime > out_mtime:
                        return False
                except OSError:
                    continue
        else:
            try:
                if ip.stat().st_mtime > out_mtime:
                    return False
            except OSError:
                continue
    return True


# ── Driver ─────────────────────────────────────────────────────────────


def parse_args(argv):
    ap = argparse.ArgumentParser()
    ap.add_argument("--template", required=True)
    ap.add_argument("--mcp", default=None)
    ap.add_argument("--commands-dir", default=None)
    ap.add_argument("--profile-snippet", default=None)
    ap.add_argument("--output", required=True)
    return ap.parse_args(argv)


def main(argv):
    args = parse_args(argv)

    template_path = pathlib.Path(args.template)
    if not template_path.is_file():
        # Match bash: silently no-op if the template is gone.
        return 0

    if cache_fresh(
        args.output,
        [args.template, args.mcp, args.commands_dir, args.profile_snippet],
    ):
        return 0

    mcp_section = render_mcp_section(args.mcp)
    commands_section = render_commands_section(args.commands_dir)

    profile_guide = None
    if args.profile_snippet and pathlib.Path(args.profile_snippet).is_file():
        profile_guide = pathlib.Path(args.profile_snippet).read_text()
        # rstrip the trailing newline so we don't double up when we re-add one
        if profile_guide.endswith("\n"):
            profile_guide = profile_guide[:-1]

    # Marker matching strips surrounding whitespace so trailing spaces or
    # leading indentation don't silently break substitution (the marker
    # comment would otherwise pass through to the rendered output).
    out_lines = []
    for line in template_path.read_text().splitlines():
        stripped = line.strip()
        if stripped == "<!-- @GENERATED:MCP_SERVERS -->":
            out_lines.append(mcp_section)
        elif stripped == "<!-- @GENERATED:COMMANDS -->":
            out_lines.append(commands_section)
        elif stripped == "<!-- @GENERATED:PROFILE_GUIDE -->":
            if profile_guide is not None:
                out_lines.append(profile_guide)
            # else: drop the line entirely (don't even append empty)
        else:
            out_lines.append(line)

    rendered = "\n".join(out_lines) + "\n"

    out_path = pathlib.Path(args.output)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    fd, tmp_path = tempfile.mkstemp(
        prefix=out_path.name + ".", dir=str(out_path.parent)
    )
    try:
        with os.fdopen(fd, "w") as f:
            f.write(rendered)
        os.replace(tmp_path, out_path)
    except Exception:
        try:
            os.unlink(tmp_path)
        except OSError:
            pass
        raise
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
