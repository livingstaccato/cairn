#!/usr/bin/env bash
# SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
# SPDX-License-Identifier: MIT
#
# Render the templates against a tree built to hit their edges, and assert what
# comes out.
#
# The Go emitters have golden tests. Templates can only be checked by rendering
# them, so this renders them: a directory of one child, one of three, a weighted
# file beside an unweighted one, a title beside a file without. Every defect
# these cover is invisible to a unit test and obvious on a page.
#
# The fixture is generated rather than committed: it exists to be awkward, and an
# awkward tree in the repository is one more thing to explain.
set -euo pipefail

cd "$(dirname "$0")/.."
repo="$PWD"
d="${TEMPLATE_DIR:-$(mktemp -d)}"
fail=0

go build -o "$d/cairn" ./cmd/cairn

mk() { mkdir -p "$(dirname "$d/site/tree/$1")"; printf '%s\n' "$2" > "$d/site/tree/$1"; }

# One child exactly: the count that a float comparison gets wrong.
mk "one/only.txt" "only"
# More than one, so the plural is exercised in the same run.
mk "many/a.txt" "a"
mk "many/b.txt" "b"
mk "many/c.txt" "c"
# Authored metadata beside a file carrying none, so a missing title cannot be
# mistaken for a rendered empty one.
# The weighted file must be one that sorts last. Weight the first alphabetically
# and the assertion passes whether or not weight does anything.
mk "meta/alpha.txt" "alpha"
mk "meta/zebra.txt" "zebra"
mk "meta/_meta.yaml" "zebra.txt:
  title: The weighted one
  summary: First because it is weighted, against the alphabet
  weight: 1"

cat > "$d/site/cairn.yaml" <<YAML
version: 1
mode: hugo
root: ./tree
out: ./content
defaults:
  present: styled
  checksum: sha256
  outputs: [html, json, csv, txt, sums]
rules:
  - match: "many"
    present: bare
YAML

sed "s|__REPO__|$repo|g" ci/bench-site.go.mod.in > "$d/site/go.mod"
printf 'baseURL = "/"\ntitle = "templates"\n\n[module]\n  [[module.imports]]\n    path = "github.com/livingstaccato/cairn"\n  [[module.imports]]\n    path = "github.com/livingstaccato/cairn/themes/reference"\n' > "$d/site/hugo.toml"

(cd "$d/site" && "$d/cairn" build --config cairn.yaml >/dev/null 2>&1)
(cd "$d/site" && hugo --quiet >/dev/null 2>&1)

pub="$d/site/public"
say() { echo "FAIL: $1"; fail=1; }

# A directory holding one thing holds one item. JSON numbers unmarshal to
# float64, so a comparison against an untyped 1 is true for every count.
if grep -qE '>[[:space:]]*1 items' "$pub/index.html"; then
  say "a directory of one child rendered \"1 items\""
fi
if ! grep -qE '1 item[^s]' "$pub/index.html"; then
  say "the singular count is not on the page at all"
fi

# The plural still works, in the same render.
if ! grep -qE '3 items' "$pub/index.html"; then
  say "a directory of three children did not render \"3 items\""
fi

# weight: leads, so the authored order beats the sort key.
first=$(python3 -c "
import re,sys
h = open('$pub/meta/index.html').read()
m = re.search(r'cairn-item-name\">\s*([^<\s]+)', h)
print(m.group(1) if m else 'none')")
if [ "$first" != "zebra.txt" ]; then
  say "weight: did not lead the listing (first row: ${first:-none})"
fi

# An authored title renders, and a file without one is not given an empty shell.
if ! grep -q 'The weighted one' "$pub/meta/index.html"; then
  say "an authored title did not render"
fi
if grep -qE 'cairn-item-summary"></' "$pub/meta/index.html"; then
  say "a file with no summary rendered an empty summary element"
fi

# The two presenters keep their contract: bare runs in a text browser.
if grep -qi '<script' "$pub/many/index.html"; then
  say "the bare presenter emitted a script tag"
fi
if ! grep -qi '<script' "$pub/one/index.html"; then
  say "the styled presenter did not emit its enhancement script"
fi

# Every listing links its own machine formats, and they exist.
for f in index.json index.csv index.txt SHA256SUMS; do
  [ -f "$pub/many/$f" ] || say "many/$f was not published"
done

[ "$fail" -eq 0 ] && echo "OK: templates render correctly"
exit "$fail"
