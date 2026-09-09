#!/usr/bin/env bash
# SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
# SPDX-License-Identifier: MIT
#
# Prove the "zero external runtime assets, works airgapped" claim in
# AGENTS.md by removing the network, not by grepping the output for CDN
# URLs (ci/example.sh already does that; this is the live version of the
# same check). Builds the runtime image, runs it with --network none so the
# container has nothing but loopback, and fetches the composed example site
# from inside its own network namespace with docker exec.
#
# A failure here means a page needed the network to render.
set -euo pipefail

cd "$(dirname "$0")/.."

image=cairndex-airgap-demo
port=8080

docker build --target runtime --build-arg "DEMO_PORT=${port}" -t "$image" .

cid="$(docker run -d --rm --network none "$image")"
trap 'docker stop "$cid" >/dev/null' EXIT

fetch() {
  docker exec "$cid" wget -q -T 3 -O /dev/null "http://127.0.0.1:${port}$1"
}

# httpd starts well before docker exec can even be dispatched, but poll
# rather than assume it, so this isn't flaky on a loaded machine.
ready=0
for _ in $(seq 1 20); do
  if fetch /bootstrap/linux/; then
    ready=1
    break
  fi
  sleep 0.25
done
if [ "$ready" -ne 1 ]; then
  echo "FAIL: the server never came up inside the airgapped container"
  exit 1
fi

fail=0
for path in / /bootstrap/ /bootstrap/linux/; do
  if ! fetch "$path"; then
    echo "FAIL: $path did not load with no network"
    fail=1
  fi
done

# Fetch the real asset URLs the page itself references, rather than guessing
# fixed names: resources.Fingerprint hashes cairndex.css/js into the path
# (see listing-styled.html), so there is no static "/cairndex/cairndex.css"
# to check, and icons.svg is not a fetchable URL at all -- it is an inline
# <symbol> sprite in the page itself, per the "no external runtime assets"
# rule in AGENTS.md. This checks whatever the page actually points at.
served="$(docker exec "$cid" wget -q -O - "http://127.0.0.1:${port}/bootstrap/linux/")"
assets="$(echo "$served" | grep -oE '(href|src)="[^"]*\.(css|js)"' | sed -E 's/^[a-z]+="//; s/"$//')"
asset_count="$(echo "$assets" | grep -c . || true)"
if [ "$asset_count" -lt 2 ]; then
  echo "FAIL: expected at least a css and a js asset reference on the page, found $asset_count"
  fail=1
fi
for asset in $assets; do
  if ! fetch "$asset"; then
    echo "FAIL: $asset did not load with no network"
    fail=1
  fi
done

# example.invalid is reserved by RFC 2606 for exactly this: a name that must
# never resolve. --network none means the container has no resolver and no
# route either, so this has to fail regardless of what DNS would have said.
if docker exec "$cid" wget -q -T 3 -O /dev/null "http://example.invalid/" 2>/dev/null; then
  echo "FAIL: the airgapped container reached the network"
  fail=1
fi

# Belt and suspenders alongside ci/example.sh's static grep: check the bytes
# actually served, not just the source template.
if echo "$served" | grep -qiE 'fontawesome|fa-solid|cdnjs|googleapis|//cdn\.'; then
  echo "FAIL: a served page referenced an external asset"
  fail=1
fi

[ "$fail" -eq 0 ] && echo "OK: served with no network"
exit "$fail"
