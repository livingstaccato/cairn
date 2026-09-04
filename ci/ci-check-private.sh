#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Tim Perkins
# SPDX-License-Identifier: MIT
#
# CI wrapper for check-private.sh: materialise the pattern list from the
# PRIVATE_PATTERNS secret into a file the checker can read, then run it.
# Kept out of the workflow YAML because inline run: blocks are not allowed here.
set -euo pipefail

cd "$(dirname "$0")/.."

if [ -z "${PRIVATE_PATTERNS:-}" ]; then
  echo "ci-check-private: PRIVATE_PATTERNS secret is not set."
  echo "  Add it as a repository secret: one extended-regex term per line."
  exit 1
fi

patterns=$(mktemp)
trap 'rm -f "$patterns"' EXIT
printf '%s\n' "$PRIVATE_PATTERNS" > "$patterns"

CAIRN_PRIVATE_PATTERNS="$patterns" ci/check-private.sh
