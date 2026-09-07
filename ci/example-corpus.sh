#!/usr/bin/env bash
# SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
# SPDX-License-Identifier: MIT
#
# End-to-end gate for exampleSite-corpus: cairndex build -> hugo -> assert the
# listing rendered inside a real, separately-published theme.
#
# Unlike ci/example.sh, this one reaches the network: corpus-hugo is a real
# module import, not a local replace, so hugo/go fetch it from GitHub via
# go.sum's pinned pseudo-version. That's the point of this example — it's the
# only thing in this repo that proves cairndex against a theme it doesn't
# ship — but it means this check depends on GitHub being reachable and that
# repo staying up, unlike everything else here.
set -euo pipefail

cd "$(dirname "$0")/.."

# go run resolves against the repo's own go.mod (cairndex's real dependency
# graph); it has to run from here, not from inside exampleSite-corpus, whose
# go.mod only carries the corpus-hugo import.
rm -rf exampleSite-corpus/content exampleSite-corpus/public exampleSite-corpus/resources
go run ./cmd/cairndex build --config exampleSite-corpus/cairndex.yaml
(cd exampleSite-corpus && hugo --minify --quiet)

pub="exampleSite-corpus/public"
fail=0
check() { [ -e "$pub/$1" ] || { echo "MISSING $pub/$1"; fail=1; }; }

check index.html
check downloads/index.html
check downloads/linux/index.html

# The home page is corpus-hugo's own hero/search template, not a cairndex
# listing — this is what proves the theme's normal pages and a
# cairndex-generated section coexist rather than one replacing the other.
if ! grep -q 'search-panel\|home-inner' "$pub/index.html"; then
  echo "FAIL: home page did not render corpus-hugo's own template"; fail=1
fi

# The token bridge is the thing most likely to silently stop working if
# corpus-hugo renames a custom property.
if ! grep -q -- '--accent:var(--link)' "$pub"/css/cairndex-theme*.css; then
  echo "FAIL: the token bridge is missing from the built stylesheet"; fail=1
fi

# The sort control has to be a real button, not a span — that's the
# keyboard-operability fix this example exists partly to guard.
if ! grep -oE '<button[^>]*data-sort=.?name' "$pub/downloads/index.html" >/dev/null; then
  echo "FAIL: the sortable header is not a real button"; fail=1
fi

[ "$fail" -eq 0 ] && echo "OK: corpus-hugo example passed"
exit "$fail"
