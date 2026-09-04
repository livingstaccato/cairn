#!/usr/bin/env bash
# SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
# SPDX-License-Identifier: MIT
#
# Fail if a directory's page grows with the directory.
#
# max_rendered exists because a flat pool of fifty thousand packages rendered a
# 34 MB page. Nothing caught that except running the benchmark by hand, so the
# ceiling could come back silently. This builds one big directory twice, at two
# sizes, and asserts the page is the same size both times — proportional growth
# fails even if the absolute numbers move.
set -euo pipefail

cd "$(dirname "$0")/.."
work="${PAGEWEIGHT_DIR:-$(mktemp -d)}"
small=2000
large=8000
# Generous enough to survive ordinary markup changes; tight enough that dropping
# the cap fails. Uncapped, 8000 entries is well over a megabyte.
ceiling=$((900 * 1024))

go build -o "$work/cairn" ./cmd/cairn

page_bytes() {
  local n="$1"
  local dir="$work/n$n"
  rm -rf "$dir" && mkdir -p "$dir/tree/pool"
  for i in $(seq 1 "$n"); do
    printf 'payload %d\n' "$i" > "$dir/tree/pool/pkg_$(printf '%06d' "$i").deb"
  done
  cat > "$dir/cairn.yaml" <<YAML
version: 1
mode: direct
root: ./tree
out: ./out
defaults:
  present: bare
  outputs: [html, json]
YAML
  (cd "$dir" && "$work/cairn" build -config cairn.yaml >/dev/null 2>&1)
  wc -c < "$dir/out/pool/index.html" | tr -d ' '
}

a=$(page_bytes "$small")
b=$(page_bytes "$large")

printf '%6s entries  index.html %8s bytes\n' "$small" "$a"
printf '%6s entries  index.html %8s bytes\n' "$large" "$b"

fail=0
if [ "$a" -ne "$b" ]; then
  echo "FAIL: page weight tracks directory size ($a vs $b); the render cap is not holding"
  fail=1
fi
if [ "$b" -gt "$ceiling" ]; then
  echo "FAIL: index.html is $b bytes, over the $ceiling ceiling"
  fail=1
fi

[ "$fail" -eq 0 ] && echo "OK: page weight is constant at $b bytes"
exit "$fail"
