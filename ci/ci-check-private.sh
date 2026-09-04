#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Tim Perkins
# SPDX-License-Identifier: MIT
#
# The public-repo guard, as CI runs it.
#
# check-leaks.sh needs no configuration and always runs. check-private.sh needs
# a list of specific terms, and that list is sensitive in its own right, so it
# is not in this repo and not required here: set a PRIVATE_PATTERNS secret to
# enable it, or rely on the pre-commit hook, which reads the list from the
# machine that has it.
set -euo pipefail

cd "$(dirname "$0")/.."

ci/check-leaks.sh

if [ -z "${PRIVATE_PATTERNS:-}" ]; then
  echo "check-private: no PRIVATE_PATTERNS secret; term list not checked here"
  exit 0
fi

patterns=$(mktemp)
trap 'rm -f "$patterns"' EXIT
printf '%s\n' "$PRIVATE_PATTERNS" > "$patterns"
CAIRN_PRIVATE_PATTERNS="$patterns" ci/check-private.sh
