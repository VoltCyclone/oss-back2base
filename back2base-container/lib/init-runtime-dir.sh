#!/usr/bin/env bash
# Recreate /run/back2base after Docker mounts /run as a tmpfs.
#
# Docker mounts /run as tmpfs at container start, wiping the build-time
# /run/back2base directory created in the Dockerfile. Phase 8 of the
# entrypoint (running as node) needs to recreate it before the daemons
# start, but /run is root-owned. This wrapper exists so the entrypoint
# can invoke it via sudo against a single NOPASSWD sudoers entry
# (/etc/sudoers.d/node-runtime-dir), mirroring the firewall-refresh.sh
# pattern.
#
# Don't add functionality here — every command this script runs is
# implicitly NOPASSWD-allowed via sudo, so the surface area must stay
# tightly scoped to /run/back2base.

set -e

rm -rf /run/back2base
mkdir -p /run/back2base/power-steering
chown -R node:node /run/back2base
