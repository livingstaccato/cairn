#!/usr/bin/env bash
# SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
# SPDX-License-Identifier: Apache-2.0
#
# Enforce a minimum total from a Go coverage profile.
# Usage: ci/check-coverage.sh <profile> [minimum-percent]
#
# The total is read from the "total:" field rather than grepped as a substring,
# so a function whose name contains "total" cannot supply the number. An empty
# total — a profile `go tool cover` could not summarise — fails rather than
# comparing empty against the minimum and silently passing.
set -euo pipefail

profile="${1:?coverage profile required}"
minimum="${2:-90.0}"

# A declaration-only package (structs, constants, no function bodies) yields a
# profile holding just its mode line, and `go tool cover` reports that as 0.0%.
# That is "nothing to cover", not "nothing covered" — the two are only
# distinguishable here, before the percentage is read, because a package with
# real code always emits at least one statement line.
statements="$(grep -cve '^mode:' "${profile}" || true)"
if [ "${statements}" -eq 0 ]; then
  echo "OK: ${profile} contains no statements to cover"
  exit 0
fi

total="$(go tool cover -func="${profile}" | awk '$1 == "total:" { print $3 }' | tr -d '%')"
if [ -z "${total}" ]; then
  echo "FAIL: no total reported in ${profile}"
  exit 1
fi

echo "Total coverage: ${total}%"
if ! awk -v t="${total}" -v m="${minimum}" 'BEGIN { exit (t + 0 >= m + 0) ? 0 : 1 }'; then
  echo "FAIL: coverage ${total}% is below the ${minimum}% minimum"
  exit 1
fi
echo "OK: coverage ${total}% meets the ${minimum}% minimum"
