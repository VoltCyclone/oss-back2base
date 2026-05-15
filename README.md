# oss-back2base

> The open-source release of **[back2base](https://back2base.net)** — the CLI and container runtime, without the hosted services.

Containerized [Claude Code](https://docs.anthropic.com/en/docs/claude-code) with a curated set of MCP servers, network isolation, and prompt-cache optimizations. No local dependencies beyond Docker.

## About back2base

[**back2base**](https://back2base.net) is a hosted product that gives Claude Code a managed home: a proxy gateway with shared rate limits and observability, cross-device config sync, durable-object memory that persists across sessions and machines, and Auth0-backed sign-in.

This repository contains the parts of back2base that are **open source**: the Go CLI, the Docker container payload, the firewall, the MCP profile defaults, and the bundled skills + slash commands. Everything you need to run a sandboxed Claude Code session locally.

| Feature | `oss-back2base` (this repo) | [back2base.net](https://back2base.net) |
|---|:---:|:---:|
| Containerized Claude Code | ✓ | ✓ |
| Curated MCP servers + profiles | ✓ | ✓ |
| Outbound iptables firewall | ✓ | ✓ |
| Bundled skills + slash commands | ✓ | ✓ |
| `claudeproxy` API gateway | — | ✓ |
| Cross-device config sync | — | ✓ |
| Durable-object / R2 memory | — | ✓ |
| Auth0 sign-in | — | ✓ |

If you want the hosted features, [sign up at back2base.net](https://back2base.net). If you want a local, self-contained Claude Code container — read on.

## Quick start

```bash
# Build the binary
go build -o oss-back2base .

# Sign in to Claude (host-side) and stash the token where the container will read it:
claude setup-token
echo 'BACK2BASE_CLAUDE_CODE_OAUTH_TOKEN=<paste-here>' > ~/.config/back2base/env

# First run extracts the embedded container payload to $HOME/.local/share/back2base
# and builds the image:
./oss-back2base                       # launch Claude Code in the current directory
./oss-back2base shell                 # drop into a container shell
./oss-back2base doctor                # health checks
./oss-back2base profile               # pick an MCP profile
```

An Anthropic API key works too — set `BACK2BASE_ANTHROPIC_API_KEY` in `~/.config/back2base/env` instead of the OAuth token. See `back2base-container/defaults/env.example` for the full env-var catalog.

## What's included

- **CLI commands**: `run` (default), `shell`, `doctor`, `profile`, `prune`, `mcp`, `overview`, `update`, `selfupdate`, `install`, `setup`, `clean`, `resume`, `status`, `build` / `rebuild`, `session`, `version`.
- **Container payload** (`back2base-container/`): Dockerfile, docker-compose, entrypoint, iptables-based outbound firewall, MCP profile defaults, baseline `CLAUDE.md` template, skills + slash-command bundle.
- **GitHub Actions**: `ci.yml` (go test / vet / lint / cross-build matrix) and `release.yml` (goreleaser on tag push).

## What's not included

The hosted features in the comparison table above are not in this repo. Concretely, the following code paths from the upstream back2base codebase were removed:

- `auth0/`, `auth.go`, `login.go`, the device-flow code, and the host keychain credential store.
- `workers/` — the Cloudflare Workers that power the proxy, config sync, durable-object memory, and the landing site.
- `lib/cloud-sync.sh`, `lib/hooks/memory-push.sh`, `lib/hooks/memory-pull.sh` — removed from the container payload.
- Hosted telemetry defaults. OTel is disabled unless you point `BACK2BASE_OTEL_ENDPOINT` at your own collector.

## Repository layout

- `*.go` at the repo root — the Cobra-based CLI, flat `package main`. `assets.go` embeds the container payload via `//go:embed all:back2base-container`.
- `back2base-container/` — the shipped Docker image payload, extracted to `$BACK2BASE_HOME` on first run.
- `.github/workflows/` — CI and goreleaser release pipeline.

## License

MIT. See [LICENSE](LICENSE).

The upstream hosted product at [back2base.net](https://back2base.net) is a separate offering and is not covered by this license.
