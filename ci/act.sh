#!/usr/bin/env bash
# SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
# SPDX-License-Identifier: MIT
#
# Run the CI workflow locally with `act`.
#
# Two things need handling that a bare `act` invocation gets wrong here:
#
#   1. The `test` job is an OS matrix. act has no Windows or macOS runner, so
#      those legs would fail on a missing image rather than being skipped. The
#      matrix is filtered to the ubuntu leg.
#   2. Docker must actually be reachable. Without this check the failure surfaces
#      as an opaque act error rather than "start colima".
#
# Usage:
#   ci/act.sh                       # whole workflow, ubuntu leg only
#   ci/act.sh -j security           # one job
#   ci/act.sh --list                # what would run
#
# docker-gate and docker-airgap will not run under act with a colima Docker
# backend: colima's socket lives outside the standard /var/run/docker.sock
# path, and neither --container-daemon-socket nor a manual bind mount via
# --container-options actually lands a working socket inside act's own job
# container (tried both; the mount point exists but nothing is behind it).
# The jobs fail immediately and cleanly with "Cannot connect to the Docker
# daemon", not with a script bug. They need nothing special on a real
# GitHub-hosted runner, which has Docker natively at that path with no act
# or colima involved — this is a local-repro limitation, not a CI one.
set -euo pipefail

act_bin="${ACT_BIN:-act}"
arch="${ACT_ARCH:-}"

if ! command -v "${act_bin}" >/dev/null 2>&1; then
  echo "FAIL: ${act_bin} not found. Install it: brew install act" >&2
  exit 1
fi

if ! docker info >/dev/null 2>&1; then
  echo "FAIL: Docker is not reachable. Start it first (e.g. colima start)." >&2
  exit 1
fi

args=(push --matrix "os:ubuntu-latest")
if [ -n "${arch}" ]; then
  args+=(--container-architecture "${arch}")
fi

exec "${act_bin}" "${args[@]}" "$@"
