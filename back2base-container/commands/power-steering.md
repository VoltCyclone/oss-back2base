---
description: Show or adjust the power-steering supervisor daemon's runtime config
allowed-tools: [Bash, Read, Write, Edit, AskUserQuestion]
---

Adjust the power-steering supervisor daemon at runtime. Edits land in
`~/.claude/power-steering/config.json` and take effect on the next poll
tick (no daemon restart). The daemon falls back to env defaults for any
key the file omits.

**Subcommand (optional):** $ARGUMENTS — one of `show`, `set`, `reset`. Default is `show`.

## Routing

Pick the branch based on $ARGUMENTS:

- empty or `show` → run **show**
- `set` → run **set**
- `reset` → run **reset**
- anything else → tell the user the valid subcommands and stop

## show

1. Read `~/.claude/power-steering/config.json` if it exists (it may not — that's fine).
2. Check the daemon's effective config by running:
   ```bash
   python3 /opt/back2base/power-steering.py --print-effective-config 2>/dev/null \
     || python3 -c "
import importlib.util, json
spec = importlib.util.spec_from_file_location('ps', '/opt/back2base/power-steering.py')
ps = importlib.util.module_from_spec(spec); spec.loader.exec_module(ps)
print(json.dumps(ps.effective_config(), indent=2))
"
   ```
3. Tail the last 5 lines of `/run/back2base/power-steering/power-steering.log` so the user can see whether the daemon is alive and what its recent verdicts were.
4. Tail the last few power-steering events from `/run/back2base/power-steering/api-events.jsonl` (lines where `"source":"power-steering"`) to show recent token spend.
5. Summarize: which keys are coming from the file vs. env defaults, the last verdict, and how long ago the last check ran.

6. Show the CLAUDE.md staleness audit state. Run:
   ```bash
   STATE_DIR="${POWER_STEERING_DIR:-/run/back2base/power-steering}"
   AUDIT_STATE="$STATE_DIR/audit-state.json"
   EDITS="$STATE_DIR/edits.jsonl"

   if [ -f "$EDITS" ]; then
     total=$(wc -l < "$EDITS" | tr -d ' ')
   else
     total=0
   fi

   threshold="${BACK2BASE_CLAUDE_MD_AUDIT_EVERY_N_EDITS:-20}"

   if [ -f "$AUDIT_STATE" ]; then
     reset=$(jq -r '.edits_at_last_reset // 0' "$AUDIT_STATE" 2>/dev/null || echo 0)
     last_sug=$(jq -r '.last_suggested_ts // 0' "$AUDIT_STATE" 2>/dev/null || echo 0)
     ref_count=$(jq -r '.referenced_paths | length // 0' "$AUDIT_STATE" 2>/dev/null || echo 0)
   else
     reset=0
     last_sug=0
     ref_count=0
   fi

   since=$((total - reset))
   echo "CLAUDE.md audit:"
   echo "  edits since last reset: ${since} / threshold ${threshold}"
   echo "  CLAUDE.md-referenced paths cached: ${ref_count}"
   if [ "$last_sug" -gt 0 ]; then
     ago=$(( $(date +%s) - last_sug ))
     echo "  last suggested: ${ago}s ago"
   else
     echo "  last suggested: never"
   fi
   ```
   Note: this is the audit subsystem that prompts the user to run `/revise-claude-md`
   when CLAUDE.md may be stale. Disable with `BACK2BASE_HOOK_CLAUDE_MD_AUDIT=off`.

## set

Use `AskUserQuestion` to elicit one or more knob changes. Show the user the **current effective values** first (from the show step) so they have context.

Knobs (all optional — only write keys the user actually sets):

| Key | Type | Sensible range | Notes |
|---|---|---|---|
| `model` | string | `claude-haiku-4-5` (default), `claude-sonnet-4-6`, `claude-opus-4-7` | Sonnet/Opus for harder reasoning at higher cost |
| `max_tokens` | int | 1024–16000 | Total output budget; must exceed `thinking_budget` |
| `thinking_budget` | int | 0–8000 | 0 disables extended thinking; >0 must be `< max_tokens` |
| `every_n_tools` | int | 3–20 | Trigger a check every N new tool calls; lower = more checks, more cost |
| `window_turns` | int | 5–50 | Recent transcript entries shipped to the supervisor |
| `min_sleep` | float | 30–300 | Floor on supervisor-suggested sleep |
| `max_sleep` | float | 300–3600 | Ceiling on supervisor-suggested sleep |

**Validation before writing:**
- If `thinking_budget >= max_tokens`, warn the user and either bump `max_tokens` or set `thinking_budget=0`. The daemon silently drops the thinking block in that case so it won't error, but the user probably didn't mean it.
- `min_sleep <= max_sleep` — refuse to write if violated.

**Write process:**
1. Read existing `~/.claude/power-steering/config.json` if any (use `{}` otherwise).
2. Merge the user's chosen overrides into the dict.
3. `mkdir -p ~/.claude/power-steering` then write atomically:
   ```bash
   tmp=$(mktemp); echo "$JSON" > "$tmp" && mv "$tmp" ~/.claude/power-steering/config.json
   ```
4. Confirm the new effective config by re-running the show step's effective-config query.
5. Tell the user: "takes effect on the next poll (within `POWER_STEERING_POLL_SEC`s, default 10s)."

## reset

Delete `~/.claude/power-steering/config.json` so the daemon falls back to env defaults on the next tick. Confirm with the user before deleting if the file has more than 2 keys (avoids accidental wipes).

```bash
[ -f ~/.claude/power-steering/config.json ] && rm ~/.claude/power-steering/config.json
```

Then run the show step so the user sees the env defaults.

## Notes

- The daemon reads `~/.claude/power-steering/config.json` at the top of every poll iteration. There's no SIGHUP, no restart needed.
- Malformed JSON is logged to `/run/back2base/power-steering/power-steering.log` and the daemon falls back to env defaults — a typo can't take it offline.
- Only whitelisted keys are honored (`model`, `max_tokens`, `thinking_budget`, `every_n_tools`, `window_turns`, `min_sleep`, `max_sleep`). Other keys are silently dropped.
- To disable the daemon entirely, set `BACK2BASE_POWER_STEERING=off` in the environment — that's an env-only switch, not a runtime knob, because it gates the daemon from ever starting.
