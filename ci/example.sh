#!/usr/bin/env bash
# SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
# SPDX-License-Identifier: MIT
#
# End-to-end gate: cairndex build -> hugo -> assert the documented output paths and
# that the Go and Hugo halves agree.
#
# This is the only check that can catch the two drifting. Everything else tests
# one side or the other.
set -euo pipefail

cd "$(dirname "$0")/.."

# A filename holding < > and " cannot exist in the repository: git on Windows
# refuses to check such a path out, which fails the whole matrix leg before any
# test runs. It is created here instead, so the escaping assertions below still
# have something hostile to work on.
hostile='exampleSite/tree/bootstrap/report<&>"quoted".txt'
printf 'quarterly numbers\n' > "$hostile"

# A real, minimal wheel for the PEP 503 fixture below, generated rather than
# committed for the same reason the hostile filename above is: this is a
# synthetic artifact for the test, not tree content, and a binary blob in git
# history has no provenance a diff can show.
pypi_dir='exampleSite/tree/pypi/samplepkg'
mkdir -p "$pypi_dir"
python3 ci/gen_pep503_wheel.py "$pypi_dir/samplepkg-1.0.0-py3-none-any.whl"

trap 'rm -f "$hostile"; rm -rf exampleSite/tree/pypi' EXIT
repo="$PWD"

rm -rf exampleSite/content exampleSite/public
go run ./cmd/cairndex build --config exampleSite/cairndex.yaml
(cd exampleSite && hugo --quiet)

# Overlay the artifact tree onto the published site so one http.server can
# serve both, and so the link check below sees what a visitor sees.
#
# This is a DEMO CONVENIENCE, not the deployment. Hugo never copies artifacts —
# it only ever sees the _index.md files cairndex wrote — and in production the web
# server serves the artifact tree from its own root at the same URL prefix. See
# docs/deployment.md. Copying a real mirror would defeat the point.
# cp rather than rsync: rsync is absent from the CI runner image.
cp -R exampleSite/tree/. exampleSite/public/

pub="exampleSite/public"
fail=0
check() { [ -e "$pub/$1" ] || { echo "MISSING $pub/$1"; fail=1; }; }

# Every covered directory publishes all three formats at the same URL.
for d in bootstrap bootstrap/linux pool docs external; do
  check "$d/index.html"
  check "$d/index.json"
  check "$d/index.csv"
  check "$d/index.txt"
done

# Hugo publishes a bundle resource verbatim, so what a reader fetches must be
# the exact bytes cairndex wrote. This is the check that keeps it that way: while
# Hugo re-rendered these there were two producers of one file and they drifted.
for f in index.json index.csv index.txt SHA256SUMS; do
  a="exampleSite/content/bootstrap/$f"
  b="$pub/bootstrap/$f"
  if [ -f "$a" ] && ! cmp -s "$a" "$b"; then
    echo "FAIL: $f published differs from what cairndex wrote"
    fail=1
  fi
done

# The CSV column order is a contract shell consumers index into.
go_header="$(go run ./ci/csvheader)"
csv_header="$(head -1 "$pub/bootstrap/index.csv")"
if [ "$go_header" != "$csv_header" ]; then
  echo "FAIL: CSV header drift"
  echo "  declared:  $go_header"
  echo "  published: $csv_header"
  fail=1
fi

# Frontmatter must stay small. Carrying the listing in it capped a directory at
# roughly ten thousand entries.
fm_lines="$(awk '/^---$/{n++; next} n==1{c++} END{print c+0}' exampleSite/content/bootstrap/_index.md)"
if [ "$fm_lines" -gt 40 ]; then
  echo "FAIL: _index.md frontmatter is $fm_lines lines; the listing has leaked back into it"
  fail=1
fi

# bare is JS-free by contract; styled is progressive enhancement over a listing
# that is already complete.
if grep -qi '<script' "$pub/pool/index.html"; then
  echo "FAIL: the bare presenter emitted a script tag"; fail=1
fi
if ! grep -qi '<script' "$pub/bootstrap/index.html"; then
  echo "FAIL: the styled presenter did not emit its enhancement script"; fail=1
fi

# The digest is the one element this design is built around; a styled listing
# with checksums on must actually render it.
if ! grep -q 'cairndex-sum-head' "$pub/bootstrap/index.html"; then
  echo "FAIL: the styled listing rendered no digest"; fail=1
fi

# A recursive directory publishes its whole subtree as one file, and the format
# switcher has to offer it — it was written and linked from nowhere for a while.
if ! grep -q 'href="tree.json"' "$pub/pool/index.html"; then
  echo "FAIL: the recursive listing is not reachable from the page"; fail=1
fi

# A capped page states what it is showing and points at the complete listing. A
# truncated listing that does not say so misrepresents the server.
if ! grep -q 'cairndex-truncated' "$pub/docs/index.html"; then
  echo "FAIL: a capped page carried no truncation notice"; fail=1
fi
if ! grep -q 'placeholder="Filter the first' "$pub/docs/index.html"; then
  echo "FAIL: the filter on a capped page did not narrow its stated scope"; fail=1
fi
if grep -q 'cairndex-truncated' "$pub/bootstrap/index.html"; then
  echo "FAIL: an uncapped page carried a truncation notice"; fail=1
fi

# A filename is attacker-influenced on a mirror, and the templates interpolate
# it into markup and into an href. Both have to escape it: the raw form
# appearing anywhere means one of them let it through.
raw='report<&>"quoted".txt'
if grep -qF "$raw" "$pub/bootstrap/index.html"; then
  echo "FAIL: a hostile filename reached the page unescaped"; fail=1
fi
if ! grep -qF 'report&lt;&amp;&gt;&#34;quoted&#34;.txt' "$pub/bootstrap/index.html"; then
  echo "FAIL: the escaped filename is not on the page at all"; fail=1
fi

# weight: in a sidecar leads the sort. It is applied after the walk, so the
# ordering the walker produced has to be re-established — it was not, and only
# the built output showed it.
first=$(python3 -c "
import json;print(json.load(open('$repo/exampleSite/content/bootstrap/index.json'))['entries'][0]['name'])")
if [ "$first" != "bootstrap.sh" ]; then
  echo "FAIL: weight: did not lead the sort (first entry: $first)"; fail=1
fi

# url: points an entry somewhere else without moving the file.
if ! grep -q 'https://example.invalid/netboot/boot.ipxe' "$pub/bootstrap/index.html"; then
  echo "FAIL: url: did not redirect the entry"; fail=1
fi

# Nothing may reach the network at render time.
if grep -rniE 'fontawesome|fa-solid|cdnjs|googleapis|//cdn\.' "$pub/" >/dev/null; then
  echo "FAIL: external asset reference in output"; fail=1
fi

# Sizes are formatted in the template, and a sub-kilobyte file must not read as
# zero — the defect this project exists downstream of.
if grep -qE '>0 ?KB<|>0\.0 KiB<' "$pub"/*/index.html; then
  echo "FAIL: a sub-kilobyte size rendered as zero"; fail=1
fi

# Every internal link must resolve. A listing whose entries 404 is not a
# listing, and the overlay above is what makes them resolve.
python3 ci/checklinks.py "$pub" || fail=1

# Emitted JSON is valid, non-empty, and uses the contract's key names.
python3 - "$pub/bootstrap/index.json" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
assert d["entries"], "no entries"
required = {"name", "path", "is_dir", "size", "modified", "kind", "depth"}
missing = required - set(d["entries"][0])
assert not missing, f"missing keys: {missing}"
PY

# Checksums verify against the real tool. SHA256SUMS names files relative to the
# served root, which is the artifact tree, so verification runs from there.
sums="$repo/exampleSite/content/bootstrap/SHA256SUMS"
if [ -f "$sums" ]; then
  if command -v sha256sum >/dev/null; then
    (cd exampleSite/tree/bootstrap && sha256sum -c "$sums" >/dev/null) \
      || { echo "FAIL: sha256sum -c"; fail=1; }
  elif command -v shasum >/dev/null; then
    (cd exampleSite/tree/bootstrap && shasum -a 256 -c "$sums" >/dev/null) \
      || { echo "FAIL: shasum -c"; fail=1; }
  fi
fi

# PEP 503 verifies against the real client, the same way checksums verify
# against the real sha256sum -c above. A file:// index-url cannot exercise
# this: an absolute href like "/samplepkg-1.0.0-py3-none-any.whl" resolves
# against the OS filesystem root under file://, not against the served
# tree's own root the way it does over real HTTP -- so this serves the built
# site and points pip at it for real, over a loopback socket.
if command -v pip3 >/dev/null || python3 -m pip --version >/dev/null 2>&1; then
  pip_port=19187
  python3 -m http.server "$pip_port" --directory "$pub" >/dev/null 2>&1 &
  pip_server=$!
  trap 'kill "$pip_server" 2>/dev/null; rm -f "$hostile"; rm -rf exampleSite/tree/pypi' EXIT
  for _ in $(seq 1 20); do
    curl -sf "http://127.0.0.1:$pip_port/pypi/" >/dev/null 2>&1 && break
    sleep 0.25
  done
  pip_out=$(mktemp -d)
  if ! python3 -m pip download --no-deps -d "$pip_out" \
      --index-url "http://127.0.0.1:$pip_port/pypi/" samplepkg >/tmp/pip-download.log 2>&1; then
    echo "FAIL: pip download could not fetch the PEP 503 fixture package"
    cat /tmp/pip-download.log
    fail=1
  elif [ ! -e "$pip_out/samplepkg-1.0.0-py3-none-any.whl" ]; then
    echo "FAIL: pip download reported success but the wheel is not where it should be"
    fail=1
  fi
  rm -rf "$pip_out"
  kill "$pip_server" 2>/dev/null || true
fi

[ "$fail" -eq 0 ] && echo "OK: end-to-end passed"
exit "$fail"
