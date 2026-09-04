#!/usr/bin/env bash
# SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
# SPDX-License-Identifier: MIT
#
# Measure one directory holding N entries.
#
# That shape scales worst, because the whole listing lands in one page: a flat
# package pool is the realistic case, and it is where the frontmatter and the
# rendered HTML both grow without bound.
#
# Usage: ci/bench.sh [n ...]      default: 1000 10000 50000
set -euo pipefail

cd "$(dirname "$0")/.."
repo="$PWD"
bench="${BENCH_DIR:-$(mktemp -d)}"
sizes=("$@")
[ "${#sizes[@]}" -eq 0 ] && sizes=(1000 10000 50000)

# /usr/bin/time -l on BSD, -v on GNU. Peak RSS is reported in bytes and KB
# respectively, so both are normalised to MB here.
timed() {
  if /usr/bin/time -l true >/dev/null 2>&1; then
    /usr/bin/time -l "$@" 2>&1
  else
    /usr/bin/time -v "$@" 2>&1
  fi
}

metric() {
  awk '
    { for (i=1;i<=NF;i++) if ($i=="real") t=$(i-1) }
    /maximum resident/ { rss=$1/1048576 }
    /Maximum resident/ { rss=$NF/1024 }
    /Elapsed .wall/    { t=$NF }
    END { printf "%-6s %5.0fMB", (t==""?"?":t"s"), rss }'
}

go build -o "$bench/cairn" ./cmd/cairn

printf '%-8s %-22s %-14s %s\n' "ENTRIES" "STAGE" "TIME  PEAK RSS" "OUTPUT"
for n in "${sizes[@]}"; do
  d="$bench/n$n"
  rm -rf "$d"
  mkdir -p "$d/tree/pool" "$d/site"
  python3 -c "
import sys
d, n = sys.argv[1], int(sys.argv[2])
for i in range(n):
    open(f'{d}/pkg-{i:06d}_1.0.{i%97}_amd64.deb','w').write('x'*((i%512)+1))
" "$d/tree/pool" "$n"

  cat > "$d/direct.yaml" <<YAML
version: 1
mode: direct
root: ./tree
out: ./out
defaults:
  present: bare
  checksum: sha256
  outputs: [html, json, csv, txt, sums]
YAML
  m="$(timed "$bench/cairn" build -config "$d/direct.yaml" | metric)"
  printf '%-8s %-22s %-14s %s\n' "$n" "cairn direct, cold" "$m" \
    "html $(du -h "$d/out/pool/index.html" | cut -f1)  json $(du -h "$d/out/pool/index.json" | cut -f1)"

  m="$(timed "$bench/cairn" build -config "$d/direct.yaml" | metric)"
  printf '%-8s %-22s %-14s %s\n' "$n" "cairn direct, warm" "$m" "hash cache hit"

  sed "s|__REPO__|$repo|g" ci/bench-site.go.mod.in > "$d/site/go.mod"
  cp ci/bench-site.hugo.toml.in "$d/site/hugo.toml"
  cat > "$d/hugo.yaml" <<YAML
version: 1
mode: hugo
root: ./tree
out: ./site/content
defaults:
  present: styled
  outputs: [html, json, csv, txt]
YAML
  m="$(timed "$bench/cairn" build -config "$d/hugo.yaml" | metric)"
  printf '%-8s %-22s %-14s %s\n' "$n" "cairn hugo" "$m" \
    "_index.md $(du -h "$d/site/content/pool/_index.md" | cut -f1)"

  m="$(cd "$d/site" && timed hugo --quiet | metric)"
  printf '%-8s %-22s %-14s %s\n' "$n" "hugo render" "$m" \
    "html $(du -h "$d/site/public/pool/index.html" | cut -f1)"
  echo
done

echo "bench tree: $bench"
