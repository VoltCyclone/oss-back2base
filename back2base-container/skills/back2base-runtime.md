---
name: back2base-runtime
model: sonnet
context: fork
agent: Plan
paths: ".aws/*,*power-steering*,*statusline*,/run/back2base/*"
description: "Where back2base puts runtime state inside the container, and which paths are writable vs read-only. Use when troubleshooting: AWS CLI 'Read-only file system' errors, blank statusline segments (pwr/tok/cache/cost showing —), missing power-steering log, missing /run/back2base/, the boot 'runtime …' diagnostic line, or any question about which files persist across container restarts vs vanish."
---

# back2base runtime layout

This container is launched by the back2base CLI on the host. It bind-mounts a few host directories in, but most of the filesystem is per-container and ephemeral. Knowing which is which prevents whole categories of bugs.

## State classes

| Path inside container | Class | Survives `docker rm`? | Shared with concurrent containers? | Writable? |
|---|---|---|---|---|
| `/workspace` | Bind: host repo | Yes (it's the host) | Yes (same dir on host) | Yes |
| `~/.claude/` | Bind: `~/.config/back2base/state` on host | Yes | **Yes — coordinate carefully** | Yes |
| `~/.claude/power-steering/config.json` | Bind (subset) | Yes | Yes — intentionally global | Yes |
| `/run/back2base/power-steering/` | Container layer (tmpfs after entrypoint) | No | **No — strictly per-container** | Yes (owned by `node`) |
| `/home/node/.aws-host/` | Bind: host `~/.aws` | Yes (it's the host) | Yes | **No — read-only** |
| `~/.aws/` | Container layer | No | No | Yes |
| `/home/node/.kube-host/` | Bind: host `~/.kube` | Yes (it's the host) | Yes | **No — read-only** |
| `~/.kube/` | Container layer | No | No | Yes |
| `/home/node/.config/gh-host/` | Bind: host `~/.config/gh` | Yes (it's the host) | Yes | **No — read-only** |
| `~/.config/gh/` | Container layer | No | No | Yes |

**Mental model:** anything under `~/.claude/` is shared user state. Anything under `/run/back2base/` is per-container ephemeral runtime state for power-steering. AWS creds get copied from a read-only host mount into a writable container-local `~/.aws/` at startup so the CLI can manage its own cache.

## power-steering state files

The power-steering daemon writes here:

```
/run/back2base/power-steering/
  metrics.json           cumulative tokens / cost across this container's session
  pending.md             drift queue; UserPromptSubmit hook drains it
  power-steering.log     last verdict, daemon heartbeat
  api-events.jsonl       per-API-call events; metrics.json aggregates this
  .cache.json            local supervisor-response cache (5min TTL)
  .tick                  PostToolUse hook touches this; daemon watches mtime
  .locked-session.json   `{session_id, path}` of the JSONL the daemon locked onto
```

The statusline reads `metrics.json`, `pending.md`, and `power-steering.log` on each render (~5s cadence). If any of those are missing or empty the corresponding segment shows `—` — that's not a bug, just no data yet.

The daemon's runtime config (model, sleep bounds, etc., editable via `/power-steering`) lives at `~/.claude/power-steering/config.json` — that path is shared across containers on purpose.

## Host-creds staging (aws, kubectl, gh)

Three CLIs need a writable home and have host config we want available inside the container. Each follows the same pattern: read-only bind at a sidecar `*-host` path, copied to a writable canonical path at every entrypoint pass.

| CLI | Host source (RO) | In-container writable copy |
|---|---|---|
| `aws` | `/home/node/.aws-host/` | `~/.aws/` |
| `kubectl` | `/home/node/.kube-host/` | `~/.kube/` |
| `gh` | `/home/node/.config/gh-host/` | `~/.config/gh/` |

```bash
$ ls -la ~/.aws-host/         # read-only — host source of truth
$ ls -la ~/.aws/              # writable — copy + the CLI's own cache state
$ ls -la ~/.kube-host/        # read-only kubeconfig + exec-plugin scripts
$ ls -la ~/.kube/             # writable — kubectl can refresh tokens here
$ ls -la ~/.config/gh-host/   # read-only — host's gh auth state
$ ls -la ~/.config/gh/        # writable — gh's normal home
```

**Don't run interactive auth commands inside the container** — `aws configure`, `kubectl config set-context --current`, `gh auth login` would all write to the writable copy, which evaporates on next start. Edit on the host, restart the container.

Why these three need writable copies:
- `aws`: assume-role and SSO write `~/.aws/cli/cache/`, `~/.aws/sso/cache/`.
- `kubectl`: exec-plugin auth (e.g. `aws eks get-token`, `gke-gcloud-auth-plugin`) caches tokens in `~/.kube/cache/`. `kubectl config use-context` rewrites `~/.kube/config`.
- `gh`: token refresh and host config writes go to `~/.config/gh/hosts.yml`.

If your host doesn't have one of these dirs, the back2base CLI omits its bind mount entirely (the Go side stats `~/.aws`, `~/.kube`, `~/.config/gh` before `docker compose up` and writes a per-run override file with only the present dirs — see `compose.go:writeHostCredsOverride`). So a host without `~/.kube` won't get an empty `~/.kube/` auto-created, and the corresponding `*-host` mount simply won't exist inside the container.

## Boot diagnostic line

The entrypoint emits one line at the end of phase 8 to stderr:

```
:: runtime /run/back2base (tmpfs, owner=node:node 755, dir=ready) | power-steering alive pid=42
```

Format reference:

| Field | Healthy | Broken signal |
|---|---|---|
| filesystem type | `tmpfs` (Docker default) or `unknown` | If `unknown`, `/proc/mounts` was unreadable |
| owner | `node:node 755` | `missing` → entrypoint couldn't `mkdir`; sudo broken |
| dir | `ready` | `MISSING` → /run/back2base/power-steering wasn't created |
| power-steering | `alive pid=N` | `EXITED immediately` (binary crashed), `not launched (binary missing?)`, `off (kill switch)` |

## Troubleshooting recipes

**Statusline shows `pwr:— tok:— cache:— cost:—` and never updates.** Read the boot diagnostic line. If `power-steering EXITED immediately`, the daemon crashed at startup — no auth env vars set is the most common cause and is a clean idle state, not a crash; if it's actually crashing, run `python3 /opt/back2base/power-steering.py` interactively to see the traceback. If `dir=MISSING`, sudo or tmpfs setup is broken. Otherwise check `/run/back2base/power-steering/power-steering.log` for entries — no entries means the daemon hasn't completed its first poll yet (give it 10–30 seconds).

**`aws ...` / `kubectl ...` / `gh ...` returns `Read-only file system` against `~/.aws`, `~/.kube`, or `~/.config/gh`.** You're somehow looking at a bind mount, not the staged copy. Confirm with `mount | grep -E ' /home/node/(.aws|.kube|.config/gh) '` — only the `*-host` variants should appear. If the canonical path itself is bound, the entrypoint's staging step didn't run (check the start-up log) or the source dir was empty when the container booted.

**Two containers from the same repo are showing each other's metrics.** Shouldn't be possible after v0.24.1 — the daemon's writes go to per-container `/run/back2base/power-steering/`, not the shared `~/.claude/`. If you see this, the boot diagnostic line will reveal whether `/run/back2base/` was set up correctly; if both containers show `dir=ready` and you still see contamination, file an issue.

**`/power-steering show` reports nothing.** The slash command tails `/run/back2base/power-steering/{power-steering.log, api-events.jsonl}`. Empty files = daemon hasn't written yet. Missing files = daemon never started — see boot diagnostic.

## Override env vars (mostly for tests)

| Var | Default | Purpose |
|---|---|---|
| `BACK2BASE_RUNTIME_DIR` | `/run/back2base/power-steering` | Daemon's runtime dir |
| `STATUSLINE_RUNTIME_DIR` | `/run/back2base/power-steering` | Statusline reader's runtime dir |
| `POWER_STEERING_TICK` | `/run/back2base/power-steering/.tick` | PostToolUse hook target |
| `POWER_STEERING_PENDING` | `/run/back2base/power-steering/pending.md` | Drain hook source |
| `BACK2BASE_POWER_STEERING` | (unset) | Set to `off` to disable the daemon entirely |

Production runs should not set the path overrides. They exist so the bats tests can target a writable tmpdir on macOS where `/run` isn't node-writable.
