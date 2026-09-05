#!/usr/bin/env bash
# SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
# SPDX-License-Identifier: MIT
#
# Run every fuzz target briefly.
#
# Short on purpose. The value in CI is not finding new bugs on a twenty-second
# budget — it is that every target still runs, its seed corpus still passes, and
# any input committed under testdata/fuzz/ after a past failure stays fixed. Go
# replays the whole corpus before it starts generating, so a regression fails in
# the first second.
#
# Longer hunts are a local job: make fuzz FUZZTIME=300
set -euo pipefail

cd "$(dirname "$0")/.."
seconds="${FUZZTIME:-20}"

failed=0
for pkg in $(go list ./internal/...); do
  # -list prints the matching function names, then a trailing "ok" line.
  targets=$(go test -list '^Fuzz' "${pkg}" 2>/dev/null | grep '^Fuzz' || true)
  for target in ${targets}; do
    echo "── ${target}  (${pkg##*/}, ${seconds}s)"
    # -run '^$' so the unit suite does not run again for every target.
    if ! go test -run '^$' -fuzz "^${target}\$" -fuzztime="${seconds}s" "${pkg}"; then
      failed=1
    fi
  done
done

[ "${failed}" -eq 0 ] || { echo "FAIL: a fuzz target found an input the code does not handle"; exit 1; }
echo "OK: every fuzz target survived ${seconds}s"
