# oss-back2base

> The open-source release of **[back2base](https://back2base.net)** — the CLI and container runtime, without the hosted services.

A sandboxed [Claude Code](https://docs.anthropic.com/en/docs/claude-code) container. Claude runs inside Docker; you keep the host clean. The container ships with a starter set of MCP servers, slash commands, and skills — all defined in plain JSON / Markdown files you can edit.

## About back2base

[**back2base**](https://back2base.net) is a hosted product that gives Claude Code a managed home: a proxy gateway with shared rate limits and observability, cross-device config sync, durable-object memory that persists across sessions and machines, and Auth0-backed sign-in.

This repository is the open-source slice: the Go CLI, the Docker container payload, the outbound firewall, and the starter configs for MCP servers, skills, and slash commands. Everything you need to run a sandboxed Claude Code session locally — no account required.

| Feature | `oss-back2base` (this repo) | [back2base.net](https://back2base.net) |
|---|:---:|:---:|
| Containerized Claude Code | ✓ | ✓ |
| MCP server registry + named profiles | ✓ | ✓ |
| Outbound iptables firewall | ✓ | ✓ |
| Starter skills + slash commands | ✓ | ✓ |
| `claudeproxy` API gateway | — | ✓ |
| Cross-device config sync | — | ✓ |
| Durable-object / R2 memory | — | ✓ |
| Auth0 sign-in | — | ✓ |

## Requirements

- Docker (Desktop, Colima, or any compatible daemon)
- Go 1.23+ (only needed to build from source; release binaries are available on the [Releases page](https://github.com/VoltCyclone/oss-back2base/releases))
- An Anthropic credential — either a Claude Code OAuth token (uses your subscription) or an `ANTHROPIC_API_KEY`

## Quickstart

```bash
# 1. Build (or download a release binary from the Releases page)
go build -o oss-back2base .

# 2. Authenticate. Either:
claude setup-token                                     # OAuth, uses your Claude subscription
# …or grab an Anthropic API key from https://console.anthropic.com

# 3. Drop the credential into the env file
mkdir -p ~/.config/back2base
cat > ~/.config/back2base/env <<'EOF'
BACK2BASE_CLAUDE_CODE_OAUTH_TOKEN=<paste-token-here>
# or, if using an API key instead:
# BACK2BASE_ANTHROPIC_API_KEY=sk-ant-...
EOF

# 4. Launch. First run extracts the container payload to ~/.local/share/back2base
#    and builds the image (~500 MB pull + a thin local layer).
./oss-back2base
```

Other useful entry points:

```bash
./oss-back2base shell      # drop into a container shell instead of Claude
./oss-back2base profile    # pick which MCP servers load (full / go / frontend / infra / …)
./oss-back2base doctor     # health-check Docker, the payload, and your settings
./oss-back2base status     # show install paths, image, running containers
```

## Configuration

Two on-disk locations matter:

| Path | What it holds | How to edit |
|---|---|---|
| `~/.config/back2base/env` | Auth tokens, model overrides, profile selection, custom API headers, firewall toggle. | Edit directly. Sourced fresh on every launch. See [`back2base-container/defaults/env.example`](back2base-container/defaults/env.example) for the full catalog. |
| `~/.config/back2base/state/` | The container's `~/.claude` directory. Holds `.mcp.json` (MCP server registry), `settings.json` (Claude Code settings), `skills/`, `commands/`, session history, memory. | Edit directly. Seeded from the image-baked defaults on first launch, then user-owned forever. |

### Adding or removing MCP servers

Edit `~/.config/back2base/state/.mcp.json`. The starter file lists the default servers (`context7`, `filesystem`, `git`, `github`, `datadog`, etc.). Add your own, remove the ones you don't want — the file is plain JSON. Drop in the corresponding token in `~/.config/back2base/env` if the server needs one.

To temporarily narrow which subset loads, use profiles:

```bash
./oss-back2base --profile go        # backend Go work: context7, fetch, github, godevmcp, …
./oss-back2base --profile frontend  # frontend / TypeScript
./oss-back2base --profile minimal   # filesystem + git only
./oss-back2base profile             # interactive picker
```

Profiles are defined in [`back2base-container/defaults/profiles.json`](back2base-container/defaults/profiles.json). Each profile names a subset of the MCP servers from `.mcp.json` plus an optional model pin.

### Editing skills and slash commands

The container seeds `~/.config/back2base/state/skills/` and `~/.config/back2base/state/commands/` from the image defaults on first launch. After that they're yours — add, remove, or rewrite anything. The defaults are starting points, not a contract.

### Disabling the firewall

The container applies an iptables outbound allowlist (see [`back2base-container/init-firewall.sh`](back2base-container/init-firewall.sh)). To bypass it entirely — useful for debugging or networks the allowlist doesn't cover — set `DISABLE_FIREWALL=1` in `~/.config/back2base/env`.

## Repository layout

- `*.go` — the CLI source.
- `back2base-container/` — the Docker image payload (Dockerfile, compose, entrypoint, firewall, defaults). Extracted to `~/.local/share/back2base` on first run.
- `.github/workflows/` — CI and release pipeline.

## License

MIT. See [LICENSE](LICENSE).

The upstream hosted product at [back2base.net](https://back2base.net) is a separate offering and is not covered by this license.
