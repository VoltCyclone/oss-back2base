# back2base-container

**This directory is the shipped Docker image payload. Everything here runs inside an end-user's container.**

## How this directory is consumed

1. `assets.go` at the repo root embeds the entire `back2base-container/` subtree via `//go:embed all:back2base-container`.
2. At first `oss-back2base` run (or via `oss-back2base install`), `extractFS` walks that embedded FS (via `shipFS()`, which strips the `back2base-container/` prefix) and writes every file to `$BACK2BASE_HOME`.
3. `docker compose build` reads `Dockerfile` and `docker-compose.yml` from `$BACK2BASE_HOME`.

The Dockerfile's `COPY` directives assume the build context root contains `defaults/`, `lib/`, `init-firewall.sh`, and `entrypoint.sh` at the top level.

## File inventory

| File | Purpose |
|---|---|
| `Dockerfile` | Builds the image on top of `back2base-base`. |
| `docker-compose.yml` | Runtime service definition — mounts, env vars, resource limits. |
| `entrypoint.sh` | Container boot: firewall → namespace → seed defaults → `exec "$@"`. |
| `init-firewall.sh` | iptables allowlist applied when `DISABLE_FIREWALL` is unset. |
| `lib/hooks/power-steering-drain.sh` | `UserPromptSubmit` hook — drains drift hints into context. |
| `lib/hooks/power-steering-tick.sh` | `PostToolUse` hook — wakes the power-steering daemon. |
| `lib/hooks/claude-md-audit-*.sh` | Hooks for the CLAUDE.md staleness audit. |
| `defaults/CLAUDE.md.template` | Template assembled at runtime into `~/.claude/CLAUDE.md`. |
| `defaults/env.example` | Seed for `~/.config/back2base/env` on first install. |
| `defaults/mcp.json` | Baseline MCP server registry, seeded to `~/.claude/.mcp.json`. |
| `defaults/profiles.json` | Named MCP profiles. |
| `defaults/settings.json` | Baseline Claude Code settings. |
| `skills/` | User-facing skills bundle; seeded to `~/.claude/skills/`. |
| `commands/` | User-facing slash commands; seeded to `~/.claude/commands/`. |
| `test/` | Bats tests for individual scripts (run via `bats test/`). |

## Testing this subtree locally

```bash
# Bats tests
cd back2base-container
bats test/

# Image build smoke test
docker build -t oss-back2base:local --build-arg BASE_IMAGE=ramseymcgrath/back2base-base:latest .
```
