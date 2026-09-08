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

go build -o "$d/cairndex" ./cmd/cairndex

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

cat > "$d/site/cairndex.yaml" <<YAML
version: 1
mode: hugo
root: ./tree
out: ./content
defaults:
  present: styled
  checksum: sha256
  outputs: [html, json, csv, txt, sums, search]
rules:
  - match: "many"
    present: bare
YAML

sed "s|__REPO__|$repo|g" ci/bench-site.go.mod.in > "$d/site/go.mod"
printf 'baseURL = "/"\ntitle = "templates"\n\n[outputs]\n  home = ["html", "json"]\n\n[module]\n  [[module.imports]]\n    path = "github.com/livingstaccato/cairndex"\n  [[module.imports]]\n    path = "github.com/livingstaccato/cairndex/themes/reference"\n' > "$d/site/hugo.toml"

# search-integration.md's own worked example: a host folding cairndex/entries.html
# into its own search index at layouts/index.json.
mkdir -p "$d/site/layouts"
cat > "$d/site/layouts/index.json" <<'GOTMPL'
{{- $out := slice -}}
{{- range partial "cairndex/entries.html" . -}}
  {{- $out = $out | append (dict "title" .title "path" .path) -}}
{{- end -}}
{{- $out | jsonify -}}
GOTMPL

(cd "$d/site" && "$d/cairndex" build --config cairndex.yaml >/dev/null 2>&1)
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
m = re.search(r'cairndex-item-name\">\s*([^<\s]+)', h)
print(m.group(1) if m else 'none')")
if [ "$first" != "zebra.txt" ]; then
  say "weight: did not lead the listing (first row: ${first:-none})"
fi

# An authored title renders, and a file without one is not given an empty shell.
if ! grep -q 'The weighted one' "$pub/meta/index.html"; then
  say "an authored title did not render"
fi
if grep -qE 'cairndex-item-summary"></' "$pub/meta/index.html"; then
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

# outputs: [search] gives a visitor a working search box, not just the JSON.
# search-index.json and search.js are bundle resources, published verbatim
# like SHA256SUMS; the box itself is a real page one level under them —
# Hugo refuses to publish a raw .html bundle resource as a plain file, so it
# needs its own standalone layout the same way pep503 does.
for f in search-index.json search.js search/index.html; do
  [ -f "$pub/$f" ] || say "$f was not published"
done
if ! grep -q 'data-cairndex-search="../search-index.json"' "$pub/search/index.html"; then
  say "the search page does not point at ../search-index.json"
fi
if ! grep -q 'rankResults' "$pub/search.js"; then
  say "search.js published is not the real script"
fi
doctypes=$(grep -oi '<!doctype html>' "$pub/search/index.html" | wc -l | tr -d ' ')
if [ "$doctypes" != "1" ]; then
  say "the search page is not one complete document (found $doctypes <!DOCTYPE> declarations)"
fi
# The page is reachable only by knowing the URL unless the listing that owns
# it says so, the same as every other machine format in its own switcher.
if ! grep -q 'href="search/"' "$pub/index.html"; then
  say "the listing's own format switcher does not link to search/"
fi

# entries.html: a search-integration host folds these into its own index.
# Listing data moved from frontmatter to an index.json resource once entries
# started exceeding Hugo's YAML alias limit; entries.html was never updated
# to read it from there, so it silently returned zero entries ever since.
if ! grep -q '/one/only.txt' "$pub/index.json"; then
  say "entries.html did not surface a known file across site.Pages"
fi

# The parent row, in the renderer hugo mode actually uses. emitHugo never calls
# BareHTML, so the Go template's AtRoot switch proves nothing about this partial:
# the rule has to arrive as at_root frontmatter and the template has to read it.
# It needs its own site because the fixture above renders a styled root, and the
# top of the tree is the only place the row is wrong.
b="$d/bare"
mkdir -p "$b/tree/sub"
printf 'top\n' > "$b/tree/top.txt"
printf 'nested\n' > "$b/tree/sub/nested.txt"

cat > "$b/cairndex.yaml" <<YAML
version: 1
mode: hugo
root: ./tree
out: ./content
# Published under a prefix, so the breadcrumb's site-absolute root anchor is
# exercised where being wrong actually shows: it was hardcoded to "/", which is
# above the mirror here.
base_path: /mirror
defaults:
  present: bare
  outputs: [html, json]
YAML

sed "s|__REPO__|$repo|g" ci/bench-site.go.mod.in > "$b/go.mod"
printf 'baseURL = "/"\ntitle = "bare"\n\n[module]\n  [[module.imports]]\n    path = "github.com/livingstaccato/cairndex"\n  [[module.imports]]\n    path = "github.com/livingstaccato/cairndex/themes/reference"\n' > "$b/hugo.toml"

(cd "$b" && "$d/cairndex" build --config cairndex.yaml >/dev/null 2>&1)
(cd "$b" && hugo --quiet >/dev/null 2>&1)

# At the top of the tree the parent points at something cairndex never indexed: it
# leaves the mirror under base_path and walks out of the tree from file://.
if grep -q 'href="\.\./"' "$b/public/index.html"; then
  say "the top bare listing offers a link above the tree"
fi
# Everywhere else it is the only way back up.
if ! grep -q 'href="\.\./"' "$b/public/sub/index.html"; then
  say "a bare listing below the top has no way back up"
fi

# The breadcrumb's root anchor is site-absolute, so under base_path it has to be
# the top of the mirror. Hardcoded to "/" it left the tree cairndex published.
# Read out of the nav element rather than grepped: the anchors sit on the line
# after it, and "/" on its own would also match the theme's home link.
crumbs=$(python3 -c "
import re,sys
h = open('$b/public/sub/index.html').read()
m = re.search(r'<nav class=\"cairndex-breadcrumb\".*?</nav>', h, re.S)
print(' '.join(re.findall(r'href=\"([^\"]*)\"', m.group(0))) if m else 'NO-NAV')")
case "$crumbs" in
  "NO-NAV") say "the bare listing rendered no breadcrumb" ;;
  "/mirror/"*) ;;
  *) say "the breadcrumb root anchor is not the top of the mirror (hrefs: $crumbs)" ;;
esac

# pep503 in hugo mode: a root-level directory of project directories, and a
# project-level directory of distribution files, rendered by the Hugo
# template rather than mode: direct's Go one — the shape has to match.
p="$d/pep503"
mkdir -p "$p/tree/simple/My_Package" "$p/tree/simple/requests"
printf 'placeholder\n' > "$p/tree/simple/My_Package/placeholder.txt"
printf 'dist\n' > "$p/tree/simple/requests/requests-2.32.3.tar.gz"
printf 'dist\n' > "$p/tree/simple/requests/weird#file?name.tar.gz"

cat > "$p/cairndex.yaml" <<YAML
version: 1
mode: hugo
root: ./tree
out: ./content
defaults:
  present: bare
  checksum: sha256
  outputs: [json, pep503]
rules:
  - match: "simple"
    pep503_level: root
  - match: "simple/requests"
    pep503_level: project
YAML

sed "s|__REPO__|$repo|g" ci/bench-site.go.mod.in > "$p/go.mod"
printf 'baseURL = "/"\ntitle = "pep503"\n\n[module]\n  [[module.imports]]\n    path = "github.com/livingstaccato/cairndex"\n  [[module.imports]]\n    path = "github.com/livingstaccato/cairndex/themes/reference"\n' > "$p/hugo.toml"

(cd "$p" && "$d/cairndex" build --config cairndex.yaml >/dev/null 2>&1)
(cd "$p" && hugo --quiet >/dev/null 2>&1)

# The href keeps the real path; only the anchor text is PEP 503-normalized —
# a client resolves a project by name, and two spellings of the same name
# must read as the same entry regardless of the directory's actual casing.
if ! grep -q '<a href="/simple/My_Package/">my-package</a>' "$p/public/simple/index.html"; then
  say "the pep503 root page did not normalize a project directory name"
fi
if grep -qi 'cairndex-breadcrumb\|cairndex-head' "$p/public/simple/index.html"; then
  say "the pep503 page rendered the normal listing's chrome"
fi
sum=$(sha256sum "$p/tree/simple/requests/requests-2.32.3.tar.gz" | cut -d' ' -f1)
if ! grep -q "requests-2.32.3.tar.gz#sha256=$sum" "$p/public/simple/requests/index.html"; then
  say "the pep503 project page did not carry the distribution file's sha256 fragment"
fi

# A filename carrying URL-reserved characters must not break the href: pip
# parses everything after an unescaped '#' or '?' as a fragment or query,
# not part of the path, and requests the wrong thing entirely.
if ! grep -q 'weird%23file%3Fname.tar.gz' "$p/public/simple/requests/index.html"; then
  say "the pep503 href did not percent-encode a reserved character in the filename"
fi
if grep -q 'href="[^"]*weird#file' "$p/public/simple/requests/index.html"; then
  say "the pep503 href broke on an unescaped '#' in the filename"
fi

# A pep503 page must be one complete document, not the theme's baseof.html
# wrapped around a second one pep503.html brings its own — a nested
# <!DOCTYPE>/<html> is invisible to the chrome-name grep above but still
# means the page has two <title> tags and a browser parses it strangely.
doctypes=$(grep -oi '<!doctype html>' "$p/public/simple/index.html" | wc -l | tr -d ' ')
if [ "$doctypes" != "1" ]; then
  say "the pep503 page is not one complete document (found $doctypes <!DOCTYPE> declarations)"
fi

# The build-info banner is on by default and carries whatever --version this
# binary reports; pep503 is a minimal machine-facing page and must not carry
# it, the same reason it has no breadcrumb or format switcher.
if ! grep -q 'cairndex-buildinfo' "$pub/index.html"; then
  say "the styled listing does not carry the build-info banner"
fi
if grep -q 'cairndex-buildinfo' "$p/public/simple/index.html"; then
  say "the pep503 page carries the build-info banner; it should have none of the usual chrome"
fi

[ "$fail" -eq 0 ] && echo "OK: templates render correctly"
exit "$fail"
