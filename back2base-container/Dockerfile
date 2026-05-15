ARG BASE_IMAGE=back2base-base:latest
FROM ${BASE_IMAGE}

ARG TZ
ENV TZ="$TZ"

# Copy default MCP config into the image (entrypoint seeds it into user state on first run)
COPY --chown=node:node defaults/mcp.json /opt/back2base/defaults/mcp.json
COPY --chown=node:node defaults/profiles.json /opt/back2base/defaults/profiles.json
# Default Claude Code settings. The hooks block is absent here — it is
# written by lib/render-hooks.py from defaults/hooks.json on every container start.
COPY --chown=node:node defaults/settings.json /opt/back2base/defaults/settings.json

# Hook manifest — declares every back2base hook (event, command, timeout,
# kill switch). lib/render-hooks.py writes the corresponding `hooks`
# block of settings.json on every container start.
COPY --chown=node:node defaults/hooks.json /opt/back2base/defaults/hooks.json

# Copy default skills bundle into the image. Used by entrypoint's
# seed_skills_if_missing as the first-install fallback.
COPY --chown=node:node skills/ /opt/back2base/defaults/skills/

# Copy default slash commands. Seeded to ~/.claude/commands/ on first run.
COPY --chown=node:node commands/ /opt/back2base/defaults/commands/

# Copy CLAUDE.md template. Assembled into ~/.claude/CLAUDE.md at every
# container start by generate_claude_md() in entrypoint.sh.
COPY --chown=node:node defaults/CLAUDE.md.template /opt/back2base/defaults/CLAUDE.md.template

# Per-profile memory-guide snippets. generate_claude_md() inlines
# profile-snippets/<CLAUDEBOX_PROFILE>.md in place of the
# @GENERATED:PROFILE_GUIDE marker, giving Claude domain-shaped memory
# examples scoped to the selected profile. Missing snippet drops the
# marker silently (e.g. `full`, `minimal` ship without a guide).
COPY --chown=node:node defaults/profile-snippets/ /opt/back2base/defaults/profile-snippets/

# Hook scripts (power-steering-drain, claude-md-audit, and shared lib).
COPY --chown=node:node --chmod=0755 lib/hooks/ /opt/back2base/hooks/

# Session snapshot daemon — invoked by absolute path from entrypoint.sh
# to protect Claude Code session JSONLs from mid-write corruption.
COPY --chown=node:node --chmod=0755 lib/session-snapshot.sh /opt/back2base/session-snapshot.sh

# Schema-versioned settings.json migrator. Invoked once per startup from
# entrypoint.sh. See lib/migrate-settings.py for the migration registry.
COPY --chown=node:node --chmod=0755 lib/migrate-settings.py /opt/back2base/migrate-settings.py

# Hook manifest renderer (stdlib Python). Invoked from entrypoint.sh
# inside _phase2_5_seed_settings, after migrate-settings.py.
COPY --chown=node:node --chmod=0755 lib/render-hooks.py /opt/back2base/render-hooks.py

# Periodic firewall ipset refresh daemon — re-resolves ALLOWED_DOMAINS
# every 30 minutes so long sessions don't break when GitHub/Cloudflare/
# S3 etc. rotate IPs. Launched via sudo from entrypoint.sh.
COPY --chown=node:node --chmod=0755 lib/firewall-refresh.sh /opt/back2base/firewall-refresh.sh

# Path-naming sentinel — verifies (30s after launch) that Claude Code's
# project-dir derivation still matches our PWD-with-/-replaced-by-- rule.
# If Anthropic ever changes that convention, the entrypoint's memory-dir
# symlink lands in the wrong place; this catches it loudly.
COPY --chown=node:node --chmod=0755 lib/path-sentinel.sh /opt/back2base/path-sentinel.sh

# CLAUDE.md renderer (stdlib Python). Replaces the bash+jq generate_claude_md
# function in entrypoint.sh. Reassembles ~/.claude/CLAUDE.md from the template
# and the live mcp.json/commands dir on every container start.
COPY --chown=node:node --chmod=0755 lib/render-claude-md.py /opt/back2base/render-claude-md.py

# CLAUDE.md staleness-audit helper (stdlib Python). Called by claude-md-audit-trigger.sh
# to decide whether to append a suggestion to pending.md based on edit heuristics.
COPY --chown=node:node --chmod=0755 lib/claude-md-audit.py /opt/back2base/claude-md-audit.py

# Pre-launch overview helpers. Sourced by entrypoint.sh between
# generate_claude_md and the daemon launches when CLAUDEBOX_OVERVIEW=1.
COPY --chown=node:node lib/prelaunch-overview.sh /opt/back2base/prelaunch-overview.sh
COPY --chown=node:node --chmod=0755 lib/render-overview.py /opt/back2base/render-overview.py
COPY --chown=node:node lib/prelaunch-prompt.txt /opt/back2base/prelaunch-prompt.txt

# Skill preplanner — runs in Phase 6.5 of entrypoint.sh. Fingerprints the
# workspace against each skill's declared `paths:` glob and splices a
# preplanned-skills block into ~/.claude/CLAUDE.md so high-confidence
# skills are usable without a Skill tool roundtrip. Stdlib Python only.
COPY --chown=node:node --chmod=0755 lib/render-skill-preplan.py /opt/back2base/render-skill-preplan.py

# Cold-start phase helpers + in-session statusLine renderer.
COPY --chown=node:node lib/status.sh /opt/back2base/status.sh
COPY --chown=node:node --chmod=0755 lib/statusline.sh /opt/back2base/statusline.sh

# Power-steering supervisor — async drift-detection daemon. Auths the
# same way Claude Code does; idles cleanly when no auth is set.
COPY --chown=node:node --chmod=0755 lib/power-steering.py /opt/back2base/power-steering.py
COPY --chown=node:node lib/power-steering-prompt.md /opt/back2base/power-steering-prompt.md

# Belt + suspenders for legacy docker builders that silently ignore --chmod.
RUN chmod +x /opt/back2base/hooks/_common.sh /opt/back2base/hooks/power-steering-drain.sh /opt/back2base/hooks/power-steering-tick.sh /opt/back2base/hooks/claude-md-audit-tally.sh /opt/back2base/hooks/claude-md-audit-trigger.sh /opt/back2base/session-snapshot.sh /opt/back2base/migrate-settings.py /opt/back2base/render-hooks.py /opt/back2base/firewall-refresh.sh /opt/back2base/path-sentinel.sh /opt/back2base/render-claude-md.py /opt/back2base/claude-md-audit.py /opt/back2base/prelaunch-overview.sh /opt/back2base/render-overview.py /opt/back2base/render-skill-preplan.py /opt/back2base/status.sh /opt/back2base/statusline.sh /opt/back2base/power-steering.py 2>/dev/null || true

# Copy firewall and entrypoint scripts
COPY init-firewall.sh entrypoint.sh /usr/local/bin/
COPY lib/init-runtime-dir.sh /usr/local/bin/init-runtime-dir.sh
USER root
RUN chmod +x /usr/local/bin/init-firewall.sh /usr/local/bin/entrypoint.sh /usr/local/bin/init-runtime-dir.sh

# Allow `node` to recreate /run/back2base via sudo without a password —
# Docker mounts /run as tmpfs at container start, wiping the build-time
# dir, and the entrypoint (running as node) needs to recreate it before
# daemons launch. Without this NOPASSWD entry the entrypoint's sudo
# prompts for a password and `gum spin` swallows the prompt, hanging
# phase 8 ("Start daemons") indefinitely.
RUN echo "node ALL=(root) NOPASSWD: /usr/local/bin/init-runtime-dir.sh" \
      > /etc/sudoers.d/node-runtime-dir \
    && chmod 0440 /etc/sudoers.d/node-runtime-dir

# Per-container runtime dir for power-steering's ephemeral state
# (metrics.json, pending.md, log, cache, tick, api events). Lives in
# the container's writable layer (not on the ~/.claude bind mount), so
# concurrent containers don't contend on these files and a fresh
# container starts with no stale values. The entrypoint clears
# /run/back2base/* on startup as defense-in-depth. Created under
# USER root because /run is root-owned in the base image.
RUN mkdir -p /run/back2base && chown node:node /run/back2base

# Install gum (Charm) for cold-start phase spinners. Pinned to avoid
# surprise upgrades; bump intentionally when needed.
ARG GUM_VERSION=0.17.0
RUN curl -fsSL "https://github.com/charmbracelet/gum/releases/download/v${GUM_VERSION}/gum_${GUM_VERSION}_Linux_$(dpkg --print-architecture).tar.gz" \
    | tar -xz -C /usr/local/bin --strip-components=1 --wildcards '*/gum' \
    && chmod +x /usr/local/bin/gum

USER node
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["zsh"]
