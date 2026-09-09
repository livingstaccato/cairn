#!/usr/bin/env bash
# SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
# SPDX-License-Identifier: MIT
#
# Run the CI gate inside a container pinned to the exact toolchain versions
# .github/workflows/ci.yml installs, rather than whatever Go, Hugo and Node
# happen to be on this machine. `make act` runs the workflow itself under
# Docker; this runs the same checks straight from the Dockerfile, which is
# far cheaper to iterate on than an `act` job.
#
# A failing check fails `docker build` the same way it fails a commit.
set -euo pipefail

cd "$(dirname "$0")/.."

docker build --target gate -f Dockerfile .
