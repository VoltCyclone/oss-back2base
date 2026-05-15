# oss-back2base

Containerized [Claude Code](https://docs.anthropic.com/en/docs/claude-code) with a curated set of MCP servers, network isolation, and prompt-cache optimizations. No local dependencies beyond Docker.

This is the open-source release of [`back2base`](https://back2base.net). It includes the CLI and the container runtime but **not** the hosted features:

- No proxy gateway (no `claudeproxy`)
- No cloud config sync
- No durable-object memory / cross-session memory sync
- No Auth0 sign-in flow

If you want hosted memory, multi-device config sync, and a managed Anthropic proxy, use the upstream `back2base` distribution. If you want a local, self-contained dev container for Claude Code with sensible MCP defaults and a firewall — read on.

## Quick start

```bash
# Build the binary
go build -o oss-back2base .

# First run extracts the embedded container payload to $HOME/.local/share/back2base
# and builds the image. Sign in to Claude (host-side) and copy the token:
claude setup-token
echo 'BACK2BASE_CLAUDE_CODE_OAUTH_TOKEN=<paste-here>' > ~/.config/back2base/env

./oss-back2base                       # launch Claude Code in the current directory
./oss-back2base shell                 # drop into a container shell
./oss-back2base doctor                # health checks
./oss-back2base profile               # pick an MCP profile
```

Anthropic API key works too — set `BACK2BASE_ANTHROPIC_API_KEY` in `~/.config/back2base/env` instead of the OAuth token.

## What's included

- **CLI commands**: `run` (default), `shell`, `doctor`, `profile`, `prune`, `mcp`, `overview`, `update`, `selfupdate`, `install`, `setup`, `clean`, `resume`, `status`, `build`/`rebuild`, `session`, `version`.
- **Container payload** (`back2base-container/`): Dockerfile, docker-compose, entrypoint, iptables-based outbound firewall, MCP profile defaults, baseline `CLAUDE.md` template, skills + slash-command bundle.
- **GitHub Actions**: `ci.yml` (go test/vet, lint, cross-build matrix) and `release.yml` (goreleaser on tag push).

## What's not included

- `auth0/`, `auth.go`, `login.go`, the device-flow code, and the host keychain credential store — all removed.
- `workers/` (Cloudflare Workers for proxy, config, memory, landing site) — never copied.
- `lib/cloud-sync.sh`, `lib/hooks/memory-push.sh`, `lib/hooks/memory-pull.sh` — removed from the container payload.
- Hosted telemetry defaults — removed. OTel is disabled unless users point
  `BACK2BASE_OTEL_ENDPOINT` at their own collector.

## Repository layout

- `*.go` at the repo root — the Cobra-based CLI, flat `package main`. `assets.go` embeds the container payload.
- `back2base-container/` — the shipped Docker image payload, extracted to `$BACK2BASE_HOME` on first run.
- `.github/workflows/` — CI (test, vet, lint, cross-build) and release (goreleaser on tag push).

## License

MIT. See [LICENSE](LICENSE).
