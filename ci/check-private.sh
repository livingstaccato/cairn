#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Tim Perkins
# SPDX-License-Identifier: MIT
#
# Refuse to let identifying strings into this repository.
#
# Everything here ships inside a Go module zip. proxy.golang.org caches a
# published version permanently and it cannot be withdrawn, so a name that
# reaches a tag is public forever. Fixtures and test data count: an employer
# name once shipped in seven releases as the Origin: field of an APT fixture.
#
# The pattern file is deliberately not in this repo — naming what must not
# appear would itself put it here. It lives outside, and CI supplies it.
set -euo pipefail

cd "$(dirname "$0")/.."

patterns="${CAIRN_PRIVATE_PATTERNS:-$HOME/.config/cairn/private-patterns.txt}"
if [ ! -f "$patterns" ]; then
  echo "check-private: no pattern file at $patterns"
  echo "  Set CAIRN_PRIVATE_PATTERNS, or create the file: one extended-regex"
  echo "  term per line, '#' for comments. Refusing to pass without it."
  exit 1
fi

terms=$(grep -vE '^\s*(#|$)' "$patterns" || true)
[ -n "$terms" ] || { echo "check-private: pattern file is empty"; exit 1; }
alt=$(printf '%s' "$terms" | paste -sd'|' -)

# Report locations only, never the matching text. This runs in public CI, and
# echoing the match would put the string in a public log.
fail=0

# Tracked content, including test fixtures and testdata.
hits=$(git grep -niE "$alt" -- . 2>/dev/null | cut -d: -f1,2 || true)
if [ -n "$hits" ]; then
  echo "check-private: identifying strings at these locations:"
  printf '%s\n' "$hits" | sed 's/^/  /'
  fail=1
fi

# Commit messages and tag messages ship too, and are not part of the tree.
for c in $(git log --format='%H'); do
  if git log -1 --format='%s%n%b' "$c" | grep -qiE "$alt"; then
    echo "check-private: identifying strings in commit message ${c:0:8}"
    fail=1
  fi
done

for t in $(git tag); do
  if git tag -l --format='%(contents)' "$t" | grep -qiE "$alt"; then
    echo "check-private: identifying strings in tag message: $t"
    fail=1
  fi
done

[ "$fail" -eq 0 ] || exit 1
echo "check-private: clean"
