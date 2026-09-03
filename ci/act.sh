#!/usr/bin/env bash
# SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
# SPDX-License-Identifier: Apache-2.0
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
