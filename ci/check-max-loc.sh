#!/usr/bin/env bash
# SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
# SPDX-License-Identifier: MIT
#
# Fail if any tracked Go file exceeds the line budget. A file past this limit is
# doing more than one thing; split it by responsibility, not by layer.
set -euo pipefail

max="${1:-777}"
fail=0
while IFS= read -r f; do
  n="$(wc -l < "$f" | tr -d ' ')"
  if [ "$n" -gt "$max" ]; then
    echo "FAIL: $f is $n lines, limit $max"
    fail=1
  fi
done < <(git ls-files '*.go')

[ "$fail" -eq 0 ] && echo "OK: no Go file over $max lines"
exit "$fail"
