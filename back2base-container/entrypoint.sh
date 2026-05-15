#!/bin/bash
set -e

# ── Cold-start phase helpers ────────────────────────────────────────────────
# `lib/status.sh` ships in the image at /opt/back2base/status.sh; the source
# fallback to $(dirname "$0")/lib/status.sh exists for `docker compose up`
# direct dev launches where /opt isn't populated. We use an explicit
# if/else rather than `. file 2>/dev/null || . fallback` because bash 3.2
# (macOS default) treats the failed source as fatal under `set -e` even
# inside an `||` list, which breaks host-side bats tests.
# shellcheck source=/dev/null
if [ -r /opt/back2base/status.sh ]; then
  . /opt/back2base/status.sh
else
  . "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/status.sh"
fi

# Once any phase fails, demote every subsequent phase to chatty mode so
# debugging output isn't lost behind the spinner.
_FANCY_FAILED=0
_phase() {
  local title="$1"; shift
  if [ "$_FANCY_FAILED" = "1" ]; then
    BACK2BASE_VERBOSE=1 _status_run "$title" "$@"
    return $?
  fi
  _status_run "$title" "$@"
  local rc=$?
  if [ $rc -ne 0 ]; then
    _FANCY_FAILED=1
  fi
  return $rc
}

# Host-creds staging. Three CLIs (aws, kubectl, gh) all want to write
# under their own config dirs — assume-role caches, exec-plugin token
# refreshes, hosts.yml updates — so a read-only bind from the host
# breaks them. We mount each one's host dir read-only at a sidecar
# path (/home/node/<tool>-host) and copy the tree into a writable
# location on every start. Re-runs are cheap and pick up any host-side
# rotation. Don't run interactive `aws configure` / `kubectl config
# set-context --current` / `gh auth login` from inside the container —
# the writable copy evaporates on next start.
_stage_host_creds() {
  local src="$1" dst="$2"
  [ -d "$src" ] || return 0
  # Skip if the host bind source is empty — docker-compose auto-creates
  # the host path when missing, so an empty dir means the user doesn't
  # actually use this tool. Don't pollute $HOME with an empty config dir.
  [ -z "$(ls -A "$src" 2>/dev/null)" ] && return 0
  mkdir -p "$(dirname "$dst")"
  rm -rf "$dst" 2>/dev/null || true
  cp -RL "$src" "$dst" 2>/dev/null || true
  chmod -R u+w "$dst" 2>/dev/null || true
}
_stage_host_creds /home/node/.aws-host       "$HOME/.aws"
_stage_host_creds /home/node/.kube-host      "$HOME/.kube"
_stage_host_creds /home/node/.config/gh-host "$HOME/.config/gh"

# ── Phase 1: Initializing (firewall) ────────────────────────────────────────
FIREWALL_READY=0
FW_RC=255
if [ "${DISABLE_FIREWALL:-0}" = "1" ]; then
  echo ":: Firewall disabled (DISABLE_FIREWALL=1)" >&2
  FW_RC=0
fi

_phase "Initializing (firewall)" bash -c '
  fw_rc=0
  if [ "${DISABLE_FIREWALL:-0}" != "1" ]; then
    sudo /usr/local/bin/init-firewall.sh 2>&1
    fw_rc=$?
  fi
  echo "$fw_rc" > /tmp/.b2b-fw-rc
  exit 0
' || true

[ -r /tmp/.b2b-fw-rc ] && FW_RC=$(cat /tmp/.b2b-fw-rc) || true
rm -f /tmp/.b2b-fw-rc

if [ "$FW_RC" = "0" ]; then
  FIREWALL_READY=1
fi

if [ "$FW_RC" != "0" ] && [ "${DISABLE_FIREWALL:-0}" != "1" ]; then
  _status_warn "firewall init failed or skipped (no cap_net_admin)"
fi

# ── _is_truthy ──────────────────────────────────────────────────────────────
# Returns 0 if $1 is a recognised truthy string, 1 otherwise. Accepts
# 1, true, yes, on (case-insensitive). `tr` is used instead of ${var,,} so
# this stays portable to bash 3.2.
_is_truthy() {
  case "$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')" in
    1|true|yes|on) return 0 ;;
    *) return 1 ;;
  esac
}

# ── Phase 2: Setting up workspace ───────────────────────────────────────────
# cd and MEMORY_NAMESPACE detection MUST run in the parent shell so they
# propagate to subsequent phases. Only side-effect-free filesystem prep
# (claude.json, SSH, memory-dir alignment, path-sentinel) goes inside the
# wrapped subshell.

# If a repo is mounted at /repos/<n>, cd into it (must stay in parent shell).
if [ -d /repos ]; then
  REPO_DIR=$(find /repos -maxdepth 1 -mindepth 1 -type d | head -1)
  if [ -n "$REPO_DIR" ]; then
    cd "$REPO_DIR"
    [ "${BACK2BASE_VERBOSE:-0}" = "1" ] && echo ":: Working directory: $REPO_DIR"
  fi
fi

# Memory namespace detection. Auto-derived from the workspace identity:
# git remote basename → /repos scan → basename /workspace. User-supplied
# MEMORY_NAMESPACE always wins.
# (Must stay in parent shell so MEMORY_NAMESPACE is exported for downstream.)
if [ -z "${MEMORY_NAMESPACE:-}" ] && [ -d /workspace ]; then
  # WORKSPACE_NAME is the canonical project identity — set by the back2base
  # CLI from the host CWD basename. Most reliable since it doesn't depend on
  # git, symlinks, or mount-path parsing inside the container.
  if [ -n "${WORKSPACE_NAME:-}" ]; then
    MEMORY_NAMESPACE="$WORKSPACE_NAME"
  else
    # Fallback chain for non-CLI launches (docker compose up, etc.)
    git config --global --add safe.directory /workspace 2>/dev/null || true
    if _remote=$(git -C /workspace remote get-url origin 2>/dev/null) && [ -n "$_remote" ]; then
      MEMORY_NAMESPACE=$(basename "${_remote%.git}")
    elif [ -n "${REPO_PATH:-}" ] && [ "$REPO_PATH" != "." ] && [ "$REPO_PATH" != "/" ]; then
      MEMORY_NAMESPACE=$(basename "$REPO_PATH")
    else
      MEMORY_NAMESPACE=$(basename /workspace)
    fi
  fi
  export MEMORY_NAMESPACE
  [ "${BACK2BASE_VERBOSE:-0}" = "1" ] && echo ":: Memory namespace: $MEMORY_NAMESPACE"
fi

_phase2_workspace_setup() {
  # Seed claude.json on first run. Claude Code expects it at $HOME/.claude.json
  # (one level above .claude/) but the bind-mounted state dir only persists
  # files INSIDE /home/node/.claude. So we keep the real file at
  # $HOME/.claude/claude.json (inside the persistent dir) and recreate a
  # symlink at $HOME/.claude.json on every container start.
  STATE_CLAUDE_JSON="$HOME/.claude/claude.json"
  mkdir -p "$HOME/.claude"
  if [ ! -f "$STATE_CLAUDE_JSON" ]; then
    echo '{"hasCompletedOnboarding":true}' > "$STATE_CLAUDE_JSON"
  fi
  ln -sf "$STATE_CLAUDE_JSON" "$HOME/.claude.json"

  # SSH: auto-accept host keys so git clone doesn't hang on first connect.
  # The mounted ~/.ssh is read-only, so write config to a writable location.
  if [ -d "$HOME/.ssh" ] && [ ! -f "$HOME/.ssh/config" ]; then
    mkdir -p "$HOME/.ssh_local"
    cp -n "$HOME/.ssh/"* "$HOME/.ssh_local/" 2>/dev/null || true
    printf 'Host *\n  StrictHostKeyChecking accept-new\n  UserKnownHostsFile ~/.ssh_local/known_hosts\n' \
      > "$HOME/.ssh_local/config"
    export GIT_SSH_COMMAND="ssh -F $HOME/.ssh_local/config"
  fi

  # Memory directories.
  #
  # Two paths are involved and they MUST point at the same physical files:
  #
  #   1. The "namespaced" path under MEMORY_NAMESPACE (human-friendly, derived
  #      from the git remote basename, e.g. ~/.claude/projects/myrepo/memory).
  #
  #   2. Claude Code's auto-memory path, which it derives from the current
  #      working directory by replacing every '/' with '-'
  #      (e.g. /workspace → ~/.claude/projects/-workspace/memory). This is
  #      where Claude Code reads MEMORY.md from at session start and writes
  #      new memories to during the session.
  #
  # Without alignment, memories saved under (1) land outside what Claude
  # Code only reads (2) — they ships past each other and "memory doesn't
  # load". We make (1) the canonical store and symlink (2) → (1). If a user
  # already has real files at (2) from a previous version, migrate them into
  # (1) once before replacing (2) with the symlink.
  if [ -n "${MEMORY_NAMESPACE:-}" ]; then
    ns_memory_dir="$HOME/.claude/projects/${MEMORY_NAMESPACE}/memory"
    mkdir -p "$ns_memory_dir"

    # Always start with a blank memory folder so old session memory does not
    # silently bleed into a new session via Claude Code's MEMORY.md autoload.
    if [ -n "$(ls -A "$ns_memory_dir" 2>/dev/null)" ]; then
      rm -rf "${ns_memory_dir:?}"/* "${ns_memory_dir:?}"/.[!.]* 2>/dev/null || true
    fi

    # Claude Code's project dir name = $PWD with '/' → '-'. Compute against
    # the cwd at this point in the entrypoint, which is what claude inherits
    # via the final `exec "$@"` below (after any /repos cd above).
    cwd_dir_name="${PWD//\//-}"
    cc_memory_dir="$HOME/.claude/projects/${cwd_dir_name}/memory"

    # Export the absolute project dir so daemons (power-steering,
    # session-snapshot) can find Claude Code's session JSONLs without
    # re-deriving the slug. Session files live at depth 1 under this dir.
    export CLAUDE_PROJECT_DIR="$HOME/.claude/projects/${cwd_dir_name}"

    if [ "$cc_memory_dir" != "$ns_memory_dir" ]; then
      mkdir -p "$(dirname "$cc_memory_dir")"
      if [ -L "$cc_memory_dir" ]; then
        # Already a symlink — repoint only if it's stale (e.g. namespace
        # changed because the user switched MEMORY_NAMESPACE override).
        if [ "$(readlink "$cc_memory_dir")" != "$ns_memory_dir" ]; then
          ln -sfn "$ns_memory_dir" "$cc_memory_dir"
        fi
      elif [ -d "$cc_memory_dir" ]; then
        # Real directory left over from before the alignment was added.
        # Migrate any existing files into the namespaced location, then
        # replace with a symlink. cp -an preserves perms (-a) and never
        # clobbers existing files in the destination (-n).
        if [ -n "$(ls -A "$cc_memory_dir" 2>/dev/null)" ]; then
          cp -an "$cc_memory_dir"/. "$ns_memory_dir"/ 2>/dev/null || true
        fi
        rm -rf "$cc_memory_dir"
        ln -sfn "$ns_memory_dir" "$cc_memory_dir"
      else
        ln -sfn "$ns_memory_dir" "$cc_memory_dir"
      fi
    fi
  fi

  # Path-naming sentinel. Background daemon: 30s after launch, verifies that
  # Claude Code wrote its session JSONL under the directory name we predicted
  # (PWD with '/' → '-'). If the convention has changed upstream, our memory
  # symlink is dangling and "memory doesn't load" silently — this drops a
  # marker file at ~/.claude/.path-sentinel-mismatch and logs loudly. Only
  # fires for canonical launch locations to avoid noise during dev/test
  # scenarios where PWD is something atypical.
  case "$PWD" in
    /workspace|/repos/*)
      if [ -x /opt/back2base/path-sentinel.sh ]; then
        /opt/back2base/path-sentinel.sh &
        disown
      fi
      ;;
  esac
}

_phase "Setting up workspace" _phase2_workspace_setup

# Custom headers for API routing.
# If CF_AIG_TOKEN is set, route through Cloudflare AI Gateway.
CUSTOM_HEADERS=""
if [ -n "${CF_AIG_TOKEN:-}" ]; then
  CUSTOM_HEADERS="cf-aig-authorization: Bearer ${CF_AIG_TOKEN}"
  echo ":: AI Gateway auth header configured"
fi

# Append optional custom header (e.g. for Cloudflare WAF security rules).
# Format: "header-name: value"
if [ -n "${CUSTOM_API_HEADER:-}" ]; then
  if [ -n "$CUSTOM_HEADERS" ]; then
    CUSTOM_HEADERS="${CUSTOM_HEADERS}
${CUSTOM_API_HEADER}"
  else
    CUSTOM_HEADERS="${CUSTOM_API_HEADER}"
  fi
  echo ":: Custom API header configured"
fi

if [ -n "$CUSTOM_HEADERS" ]; then
  export ANTHROPIC_CUSTOM_HEADERS="$CUSTOM_HEADERS"
fi

if [ -n "${ANTHROPIC_BASE_URL:-}" ]; then
  echo ":: API routing through ${ANTHROPIC_BASE_URL}"
fi

# Docker image pulling is deferred until after MCP profile filtering (below)
# so we only pull images for servers that are actually enabled.
# Only terraform and buildkite remain as Docker-based servers; sequential-thinking,
# brave-search, context7, memory, and lsmcp now run as in-image binaries.

# Seed user-state config files from image defaults. Only writes if the
# target doesn't already exist — user customizations are preserved across
# container runs. Defaults live inside the image at /opt/back2base/defaults/.
seed_if_missing() {
  local default_path="$1" target_path="$2"
  if [ ! -e "$target_path" ] && [ -e "$default_path" ]; then
    mkdir -p "$(dirname "$target_path")"
    cp "$default_path" "$target_path"
    [ "${BACK2BASE_VERBOSE:-0}" = "1" ] && echo ":: seeded $target_path from defaults"
  fi
}

mkdir -p "$HOME/.claude"

# ── Phase 2.5: Seeding settings + MCP defaults ──────────────────────────────
# Runs BEFORE MCP profile filtering (phase 3) so the filter has a file to
# operate on.
_phase2_5_seed_settings() {
  seed_if_missing /opt/back2base/defaults/mcp.json      "$HOME/.claude/.mcp.json"
  seed_if_missing /opt/back2base/defaults/settings.json "$HOME/.claude/settings.json"
  python3 /opt/back2base/migrate-settings.py "$HOME/.claude/settings.json" || true
  python3 /opt/back2base/render-hooks.py "$HOME/.claude/settings.json" || true
}

_phase "Seeding settings + MCP defaults" _phase2_5_seed_settings

# ── MCP profile filtering ───────────────────────────────────────────────────
# When BACK2BASE_PROFILE is set (and not "full"), filter .mcp.json to only
# include servers in that profile. Core servers (filesystem, git, memory) are
# always included. The profiles definition lives at /opt/back2base/defaults/profiles.json.
filter_mcp_by_profile() {
  local profile="${BACK2BASE_PROFILE:-full}"
  local mcp_file="$HOME/.claude/.mcp.json"
  local profiles_file="/opt/back2base/defaults/profiles.json"

  if [ "$profile" = "full" ]; then
    [ "${BACK2BASE_VERBOSE:-0}" = "1" ] && echo ":: MCP profile: full (all servers)"
    return 0
  fi

  if [ ! -f "$profiles_file" ] || [ ! -f "$mcp_file" ] || ! command -v jq &>/dev/null; then
    echo ":: ⚠ Cannot filter MCP servers (missing profiles.json, .mcp.json, or jq)"
    return 0
  fi

  # Validate profile exists
  if ! jq -e --arg p "$profile" '.profiles[$p]' "$profiles_file" >/dev/null 2>&1; then
    echo ":: ⚠ Unknown profile '$profile', using full server set"
    return 0
  fi

  # Build the allowed server list: core + profile servers
  local allowed
  allowed=$(jq -r --arg p "$profile" '
    (.core + .profiles[$p].servers) | unique | .[]
  ' "$profiles_file")

  # Filter .mcp.json to only include allowed servers, sorted by key
  local tmp
  tmp=$(mktemp "$mcp_file.XXXXXX")
  if jq --argjson allowed "$(echo "$allowed" | jq -R -s 'split("\n") | map(select(. != ""))')" \
    '{ mcpServers: (.mcpServers | to_entries | map(select(.key as $k | $allowed | index($k))) | sort_by(.key) | from_entries) }' \
    "$mcp_file" > "$tmp" 2>/dev/null; then
    mv -f "$tmp" "$mcp_file"
    local count
    count=$(jq '.mcpServers | length' "$mcp_file")
    [ "${BACK2BASE_VERBOSE:-0}" = "1" ] && echo ":: MCP profile: $profile ($count servers enabled)"
  else
    rm -f "$tmp"
    echo ":: ⚠ MCP filtering failed, keeping full server set"
  fi
}

_phase "Filtering MCP profile" filter_mcp_by_profile

# ── Pull Docker images for enabled MCP servers ──────────────────────────────
# Only pull images that are actually referenced in the (possibly filtered)
# .mcp.json, rather than a hardcoded list. This respects the active profile.
pull_mcp_images() {
  local mcp_file="$HOME/.claude/.mcp.json"
  if ! command -v docker &>/dev/null; then
    echo ":: Docker CLI not found, skipping MCP image pulls"
    return 0
  fi
  if [ ! -f "$mcp_file" ] || ! command -v jq &>/dev/null; then
    return 0
  fi

  # Extract Docker images from servers whose command is "docker"
  local images
  images=$(jq -r '
    .mcpServers | to_entries[]
    | select(.value.command == "docker")
    | .value.args
    | map(select(test("^[a-z].*/"))) | first // empty
  ' "$mcp_file" 2>/dev/null | sort -u)

  if [ -z "$images" ]; then
    [ "${BACK2BASE_VERBOSE:-0}" = "1" ] && echo ":: No Docker-based MCP servers in profile, skipping image pulls"
    return 0
  fi

  # Only pull images not already present locally; pull missing ones in parallel
  local missing=""
  while IFS= read -r img; do
    [ -z "$img" ] && continue
    if ! docker image inspect "$img" &>/dev/null; then
      missing="$missing $img"
    fi
  done <<< "$images"

  if [ -z "$missing" ]; then
    [ "${BACK2BASE_VERBOSE:-0}" = "1" ] && echo ":: MCP images already present, skipping pulls"
    return 0
  fi

  [ "${BACK2BASE_VERBOSE:-0}" = "1" ] && echo ":: Pulling missing MCP server images..."
  for img in $missing; do
    docker pull -q "$img" &
  done
  wait
  [ "${BACK2BASE_VERBOSE:-0}" = "1" ] && echo ":: MCP images up to date"
}

_phase "Pulling MCP server images" pull_mcp_images

# Skills live directly inside the persistent state dir at ~/.claude/skills.
# On first install, seed_skills_if_missing copies the image-baked defaults
# from /opt/back2base/defaults/skills/. After that the user owns the tree.
seed_skills_if_missing() {
  if [ ! -d "$HOME/.claude/skills" ] || [ -z "$(ls -A "$HOME/.claude/skills" 2>/dev/null)" ]; then
    if [ -d /opt/back2base/defaults/skills ]; then
      mkdir -p "$HOME/.claude"
      # Remove any existing entry at the target (dangling symlink, empty
      # dir that cp would nest into, or a regular file). Safe because the
      # outer if already verified the path is NOT a populated dir.
      rm -rf "$HOME/.claude/skills"
      cp -RL /opt/back2base/defaults/skills "$HOME/.claude/skills"
      [ "${BACK2BASE_VERBOSE:-0}" = "1" ] && echo ":: seeded $HOME/.claude/skills from image defaults"
    fi
  fi
}

# Commands live at ~/.claude/commands/ — seeded from image defaults on first
# install, then user-owned.
seed_commands_if_missing() {
  if [ ! -d "$HOME/.claude/commands" ] || [ -z "$(ls -A "$HOME/.claude/commands" 2>/dev/null)" ]; then
    if [ -d /opt/back2base/defaults/commands ]; then
      rm -rf "$HOME/.claude/commands"
      cp -RL /opt/back2base/defaults/commands "$HOME/.claude/commands"
      [ "${BACK2BASE_VERBOSE:-0}" = "1" ] && echo ":: seeded $HOME/.claude/commands from image defaults"
    fi
  fi
}

# Plugins live at ~/.claude/plugins/ — seeded from image defaults the same
# way skills and commands are. The image build (.devcontainer/Dockerfile.base)
# runs claudepluginhub then stages the resulting tree at
# /opt/back2base/defaults/plugins/ because the build-time install path
# ($HOME/.claude/plugins) is shadowed at runtime by the state mount.
# Plugins the user installs later via Claude Code's /plugins UI land in this
# same directory and persist across restarts via BACK2BASE_STATE.
seed_plugins_if_missing() {
  if [ ! -d "$HOME/.claude/plugins" ] || [ -z "$(ls -A "$HOME/.claude/plugins" 2>/dev/null)" ]; then
    if [ -d /opt/back2base/defaults/plugins ]; then
      rm -rf "$HOME/.claude/plugins"
      cp -RL /opt/back2base/defaults/plugins "$HOME/.claude/plugins"
      [ "${BACK2BASE_VERBOSE:-0}" = "1" ] && echo ":: seeded $HOME/.claude/plugins from image defaults"
    fi
  fi
}

# ── Phase 5: Seeding image defaults ─────────────────────────────────────────
# Skills, commands, and plugins are seeded AFTER image pulls (phase 4) so we
# don't race on directories that might be populated by the pulled images.
_phase5_seed_image_defaults() {
  seed_skills_if_missing
  seed_commands_if_missing
  seed_plugins_if_missing
}

_phase "Seeding image defaults" _phase5_seed_image_defaults

# ── Generate ~/.claude/CLAUDE.md ─────────────────────────────────────────────
# Rebuilt on every container start so the MCP server inventory, skills list,
# and slash commands section always reflect what's actually installed.
# Static content comes from /opt/back2base/defaults/CLAUDE.md.template.
generate_claude_md() {
  # Delegated to lib/render-claude-md.py (stdlib Python). The script handles
  # mtime-based cache invalidation, MCP/commands table rendering, and
  # per-profile snippet inlining. Bash is just the path resolver here.
  local template=/opt/back2base/defaults/CLAUDE.md.template
  local out="$HOME/.claude/CLAUDE.md"
  local mcp="$HOME/.claude/.mcp.json"
  local commands_dir="$HOME/.claude/commands"
  local profile_snippet="/opt/back2base/defaults/profile-snippets/${BACK2BASE_PROFILE:-full}.md"
  python3 /opt/back2base/render-claude-md.py \
    --template "$template" \
    --mcp "$mcp" \
    --commands-dir "$commands_dir" \
    --profile-snippet "$profile_snippet" \
    --output "$out" \
    || echo ":: ⚠ CLAUDE.md regen failed; previous file preserved"
}

_phase "Generating CLAUDE.md" generate_claude_md

# ── Phase 6.5: Skill preplanning ────────────────────────────────────────────
# Fingerprints the workspace against each skill's declared `paths:` glob and
# splices a block listing relevant skills into ~/.claude/CLAUDE.md. Skills
# with strong matches (≥2 file matches or a root-level match) get full
# bodies inlined for zero-roundtrip use; weaker matches become one-line
# pointers. Opt out with BACK2BASE_SKILL_PREPLAN=0; auto-skipped under
# BACK2BASE_PROFILE=minimal.
run_skill_preplan() {
  python3 /opt/back2base/render-skill-preplan.py \
    --workspace "$PWD" \
    --skills-dir "$HOME/.claude/skills" \
    --claude-md "$HOME/.claude/CLAUDE.md" \
    || echo ":: ⚠ skill-preplan exited non-zero; CLAUDE.md unchanged" >&2
}

if [ "${BACK2BASE_SKILL_PREPLAN:-1}" != "0" ] && [ "${BACK2BASE_PROFILE:-full}" != "minimal" ]; then
  _phase "Fingerprinting workspace" run_skill_preplan
fi

# ── Phase 7: Generating repo overview ───────────────────────────────────────
# Opt-in (BACK2BASE_OVERVIEW=1): runs `claude -p` against the repo and splices
# a markdown overview into ~/.claude/CLAUDE.md so it's in context for the
# user's first prompt. Blocking; never propagates a failure.
# shellcheck source=/dev/null
. /opt/back2base/prelaunch-overview.sh 2>/dev/null || \
  . "$(dirname "$0")/lib/prelaunch-overview.sh"
if [ "${BACK2BASE_OVERVIEW:-0}" = "1" ]; then
  _phase "Generating repo overview" run_prelaunch_overview
fi

# ── Phase 8: Starting daemons ────────────────────────────────────────────────
# Starts session-snapshot, firewall-refresh, power-steering, and other background daemons.
_phase8_start_daemons() {
  # Daemons MUST detach stdin/stdout/stderr from this shell. Phase 8 runs
  # under `gum spin -- bash -c …`, which pipes the wrapped command's
  # stdout/stderr — gum waits for that pipe to close before the spinner
  # exits. A backgrounded daemon that inherits the pipe keeps the write
  # end open forever, so the spinner hangs even after `disown`. Each
  # daemon writes its own log file already, so </dev/null + redirection
  # to /dev/null here is safe.

  # Clear per-container runtime state (metrics, pending, log, cache,
  # tick, api events, session lock). The directory is per-container by
  # virtue of living on the writable layer, not the ~/.claude bind
  # mount, but a `docker start` of an existing container would inherit
  # yesterday's contents — wipe to guarantee a fresh statusline.
  #
  # /run is typically a tmpfs that Docker mounts on top of our build-time
  # /run/back2base, so the entrypoint (running as node) can't recreate
  # the dir directly. Invoke a NOPASSWD-allowed wrapper via sudo —
  # bare `sudo rm/mkdir/chown` is NOT in the sudoers allow-list, would
  # prompt for a password, and gum spin would hang phase 8 silently.
  sudo /usr/local/bin/init-runtime-dir.sh 2>/dev/null || \
    echo "warn: init-runtime-dir failed; statusline may show blanks" >&2

  # Session-snapshot daemon.
  if [ -x /opt/back2base/session-snapshot.sh ]; then
    /opt/back2base/session-snapshot.sh </dev/null >/dev/null 2>&1 &
    disown
  fi

  # Periodic firewall ipset refresh — only when phase 1 confirmed firewall is up.
  if [ "$FIREWALL_READY" = "1" ] && [ -x /opt/back2base/firewall-refresh.sh ]; then
    sudo /opt/back2base/firewall-refresh.sh </dev/null >/dev/null 2>&1 &
    disown
  fi

  # Power-steering supervisor daemon — async drift checks against the
  # session JSONL. Idles cleanly without auth env vars or namespace, so
  # the launch is unconditional except for the explicit kill switch.
  ps_pid=""
  case "${BACK2BASE_POWER_STEERING:-}" in
    off|0|false|no) ;;
    *)
      if [ -x /opt/back2base/power-steering.py ]; then
        /opt/back2base/power-steering.py </dev/null >/dev/null 2>&1 &
        ps_pid=$!
        disown
      fi
      ;;
  esac

  # Re-apply Claude Code banner rebrand.
  /usr/local/bin/patch-claude-banner.sh >/dev/null 2>&1 || true

  # Boot diagnostic — single line surfacing the per-container runtime
  # state. Helps debug "statusline shows blanks" failures from the
  # start-up log without needing to exec into the container.
  rt_fs=$(awk '$2=="/run" {print $3; exit}' /proc/mounts 2>/dev/null)
  rt_fs=${rt_fs:-unknown}
  rt_perms=$(stat -c '%U:%G %a' /run/back2base 2>/dev/null || echo missing)
  rt_state=$([ -d /run/back2base/power-steering ] && echo ready || echo MISSING)
  case "${BACK2BASE_POWER_STEERING:-}" in
    off|0|false|no) ps_state="off (kill switch)" ;;
    *)
      if [ -n "$ps_pid" ] && kill -0 "$ps_pid" 2>/dev/null; then
        ps_state="alive pid=$ps_pid"
      elif [ -n "$ps_pid" ]; then
        ps_state="EXITED immediately"
      else
        ps_state="not launched (binary missing?)"
      fi
      ;;
  esac
  echo ":: runtime /run/back2base ($rt_fs, owner=$rt_perms, dir=$rt_state) | power-steering $ps_state" >&2
}

_phase "Starting daemons" _phase8_start_daemons

# docker-compose's `KEY=${BACK2BASE_KEY:-}` interpolation always emits
# the unprefixed auth env var, even when the BACK2BASE_-prefixed source
# is unset — which leaves Claude Code looking at a present-but-empty
# ANTHROPIC_API_KEY / ANTHROPIC_AUTH_TOKEN / CLAUDE_CODE_OAUTH_TOKEN.
# That's enough to flip its auth-mode detection into "Claude API" mode
# (pay-per-use) instead of "Claude Max" (subscription OAuth), because
# the CLI treats the variable's mere presence as intent to use it.
# Drop any that are empty so only the populated source is visible.
for _auth_var in ANTHROPIC_API_KEY ANTHROPIC_AUTH_TOKEN CLAUDE_CODE_OAUTH_TOKEN; do
  if [ -z "${!_auth_var:-}" ]; then
    unset "$_auth_var"
  fi
done
unset _auth_var

exec "$@"
