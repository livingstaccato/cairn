#!/usr/bin/env bash
# SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
# SPDX-License-Identifier: MIT
#
# Build ./cairndex with the version embedded via ldflags, so `cairndex
# --version` reports the tag it was built from instead of a source literal
# that drifts the moment a release ships and nobody remembers to bump it.
set -euo pipefail

cd "$(dirname "$0")/.."
version="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
go build -ldflags "-X main.version=$version" -o cairndex ./cmd/cairndex
echo "OK: ./cairndex ($version)"
