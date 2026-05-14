#!/usr/bin/env python3
"""Render the `hooks` block of settings.json from defaults/hooks.json.

Runs at container start, after migrate-settings.py. Idempotent.

Failure modes (manifest missing, parse error, schema violation, OS
error) all log to ~/.claude/.back2base-hooks/render.log and exit 0 —
settings.json is left untouched, so a typo can never take Claude Code
offline.
"""

import argparse
import json
import os
import pathlib
import sys
import time

DEFAULT_MANIFEST = "/opt/back2base/defaults/hooks.json"

REQUIRED_KEYS = {"name", "event", "command", "timeout", "kill_switch", "handler"}
ALLOWED_HANDLERS = {"command", "prompt", "http", "agent"}


def log(msg):
    home = os.environ.get("HOME", "/home/node")
    log_dir = pathlib.Path(home) / ".claude" / ".back2base-hooks"
    log_dir.mkdir(parents=True, exist_ok=True)
    ts = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    try:
        with (log_dir / "render.log").open("a") as f:
            f.write(f"{ts} render-hooks {msg}\n")
    except OSError:
        pass


def load_manifest(path):
    """Return the parsed manifest dict or None on any failure."""
    try:
        with open(path) as f:
            data = json.load(f)
    except FileNotFoundError:
        log(f"manifest not found: {path}")
        return None
    except (json.JSONDecodeError, OSError) as e:
        log(f"manifest parse error: {e}")
        return None
    if not isinstance(data, dict):
        log("manifest is not an object")
        return None
    if data.get("version") != 1:
        log(f"manifest version mismatch: {data.get('version')!r}")
        return None
    hooks = data.get("hooks")
    if not isinstance(hooks, list):
        log("manifest.hooks is not a list")
        return None
    return data


def validate_entry(entry):
    """Return True iff the entry is well-formed."""
    if not isinstance(entry, dict):
        return False
    missing = REQUIRED_KEYS - set(entry.keys())
    if missing:
        log(f"entry missing keys {missing}: {entry.get('name')}")
        return False
    if entry["handler"] not in ALLOWED_HANDLERS:
        log(f"entry has unknown handler {entry['handler']!r}: {entry['name']}")
        return False
    if not isinstance(entry["timeout"], (int, float)):
        log(f"entry timeout not numeric: {entry['name']}")
        return False
    if not isinstance(entry["command"], str) or not entry["command"]:
        log(f"entry command is not a non-empty string: {entry.get('name')}")
        return False
    if "matcher" in entry and not isinstance(entry["matcher"], str):
        log(f"entry matcher is not a string: {entry.get('name')}")
        return False
    return True


def build_hooks_block(manifest):
    """Group manifest entries into the Claude Code hooks block shape."""
    block = {}
    for entry in manifest["hooks"]:
        if not validate_entry(entry):
            return None
        event = entry["event"]
        # Each event entry is {matcher, hooks: [{type, command, timeout}]}.
        # PreToolUse/PostToolUse use the optional "matcher" field; other
        # events get matcher="" so the envelope is uniform.
        matcher = entry.get("matcher", "")
        envelope = {
            "matcher": matcher,
            "hooks": [
                {
                    "type": entry["handler"],
                    "command": entry["command"],
                    "timeout": entry["timeout"],
                }
            ],
        }
        block.setdefault(event, []).append(envelope)
    return block


def render(settings_path, manifest_path):
    manifest = load_manifest(manifest_path)
    if manifest is None:
        return  # settings.json untouched

    hooks_block = build_hooks_block(manifest)
    if hooks_block is None:
        log("schema violation; manifest rejected")
        return

    p = pathlib.Path(settings_path)
    if not p.exists():
        log(f"settings.json not found: {settings_path}")
        return

    try:
        d = json.loads(p.read_text())
    except (json.JSONDecodeError, OSError) as e:
        log(f"settings.json read error: {e}")
        return
    if not isinstance(d, dict):
        log("settings.json is not an object")
        return

    d["hooks"] = hooks_block

    # Atomic write — same pattern as migrate-settings.py.
    try:
        tmp = p.with_suffix(p.suffix + ".tmp")
        tmp.write_text(json.dumps(d, indent=2) + "\n")
        tmp.rename(p)
    except OSError as e:
        log(f"write error: {e}")
        return

    summary = ", ".join(
        f"{event}({len(entries)})" for event, entries in sorted(hooks_block.items())
    )
    log(f"rendered hooks block: {summary}")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("settings", help="path to ~/.claude/settings.json")
    parser.add_argument("--manifest", default=DEFAULT_MANIFEST,
                        help="path to hooks.json (default: %(default)s)")
    args = parser.parse_args()
    render(args.settings, args.manifest)
    return 0


if __name__ == "__main__":
    sys.exit(main())
