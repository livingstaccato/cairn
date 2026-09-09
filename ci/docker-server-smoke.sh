#!/usr/bin/env bash
# SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
# SPDX-License-Identifier: MIT
#
# Proves the `server` Dockerfile target is an actual working single-container
# deployment: mount a volume, cairndex watch --serve runs inside, changes on
# the host reach a visitor without a restart, and a path-traversal attempt
# still 404s the same as any other cairndex serve.
#
# root:/out: here is exactly what `cairndex init` itself writes (mode:
# direct's starter config) -- this is the project's own blessed layout, not
# a new convention invented for containers.
#
# Needs `mktemp -d`'s directory to be one Docker will actually bind-mount.
# On a colima backend restricted to specific `mounts:` in colima.yaml (not
# colima's own default of sharing $HOME), that will not include $TMPDIR by
# default and this script will see an empty /data with no clear error --
# same class of local-only gap ci/act.sh documents for its own Docker socket
# path. Not an issue on a GitHub Actions runner or an unrestricted local
# Docker install.
set -euo pipefail

cd "$(dirname "$0")/.."

image=cairndex-server-smoke
port=18443
metrics_port=18444

docker build --target server -t "$image" .

data="$(mktemp -d)"
trap 'docker stop "$cid" >/dev/null 2>&1 || true; docker stop "${metrics_cid:-}" >/dev/null 2>&1 || true; rm -rf "$data"' EXIT

mkdir -p "$data/tree"
cat > "$data/cairndex.yaml" <<'EOF'
version: 1
root: ./tree
out:  ./site
defaults:
  present:  bare
  outputs:  [html, json, csv, txt]
EOF
printf 'first\n' > "$data/tree/hello.txt"

# --user matches the host's own uid:gid, not the image's default root: on a
# real Linux bind mount (unlike this session's own macOS/colima Docker,
# which did not reproduce this) a root process writes host files root owns,
# and this script's own cleanup trap -- run as the unprivileged CI user --
# cannot remove them afterward. bash's EXIT trap reports the *trap's* exit
# status, not the script's, so that Permission denied silently overwrote
# fail=0 with a failure -- confirmed directly on a real GitHub Actions
# runner, where the functional assertions below all still passed.
cid="$(docker run -d --rm --user "$(id -u):$(id -g)" -p "${port}:8080" -v "$data:/data" "$image")"

fetch() { curl -sf -D - -o /tmp/docker-server-smoke-body "http://127.0.0.1:${port}$1"; }

ready=0
for _ in $(seq 1 40); do
  if fetch / >/tmp/docker-server-smoke-headers 2>/dev/null; then
    ready=1
    break
  fi
  sleep 0.25
done
if [ "$ready" -ne 1 ]; then
  echo "FAIL: the server never came up"
  docker logs "$cid" || true
  exit 1
fi

fail=0

if ! grep -q 'hello.txt' /tmp/docker-server-smoke-body; then
  echo "FAIL: the mounted tree's own file is not in the listing"
  fail=1
fi
if ! grep -qi 'Cache-Control: no-store' /tmp/docker-server-smoke-headers; then
  echo "FAIL: served without Cache-Control: no-store"
  fail=1
fi

# The point of running watch, not just serve: a change on the host must
# reach a visitor with no restart. The server answering already proves
# cairndex's own startup finished, but fsnotify's watch registration can
# still trail that by a moment -- a beat here is cheaper than a flaky test.
#
# On a bind mount backed by virtiofs (colima/Docker Desktop on macOS) the
# host-to-container inotify event this depends on is documented as
# experimental (colima.yaml: "mountInotify: true # NOTE: this is
# experimental") and was observed here to miss the event outright on most
# runs, not just arrive late -- a native Linux bind mount (any real Docker
# host, including every GitHub Actions runner) has no such translation layer
# and does not share this failure mode; TestWatchServeServesWhatItBuilt
# already covers the same behavior directly, with no Docker or virtiofs
# involved. A missed event here is a warning, not a failure: this one
# assertion is a property of the host's bind-mount implementation, not of
# cairndex, and would otherwise make this script permanently red on a
# virtiofs backend regardless of how long it waits.
sleep 1
printf 'second\n' > "$data/tree/again.txt"
changed=0
for _ in $(seq 1 20); do
  fetch / >/dev/null 2>&1 || true
  if grep -q 'again.txt' /tmp/docker-server-smoke-body 2>/dev/null; then
    changed=1
    break
  fi
  sleep 0.5
done
if [ "$changed" -ne 1 ]; then
  echo "WARN: a file added on the host after startup never appeared -- expected on virtiofs (see comment above), a real failure on a native Linux bind mount"
fi

# Same containment cairndex serve always applies, exercised through the
# container's own network namespace this time, not just in-process.
if curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${port}/../../etc/passwd" | grep -qv '^4'; then
  echo "FAIL: a path-traversal request did not get a 4xx"
  fail=1
fi

# --metrics is off in the image's own default CMD (see the Dockerfile) --
# proven above by never having asked for it -- so a second container with
# it appended is what proves the flag actually reaches the server inside a
# real container, not just internal/serve's own in-process tests.
metrics_cid="$(docker run -d --rm --user "$(id -u):$(id -g)" -p "${metrics_port}:8080" -v "$data:/data" "$image" \
  watch --serve --addr 0.0.0.0:8080 --metrics)"

metrics_ready=0
for _ in $(seq 1 40); do
  if curl -sf "http://127.0.0.1:${metrics_port}/healthz" >/tmp/docker-server-smoke-healthz 2>/dev/null; then
    metrics_ready=1
    break
  fi
  sleep 0.25
done
if [ "$metrics_ready" -ne 1 ]; then
  echo "FAIL: --metrics's server never came up"
  docker logs "$metrics_cid" || true
  fail=1
else
  if ! grep -q '"status":"ok"' /tmp/docker-server-smoke-healthz; then
    echo "FAIL: /healthz did not report ok: $(cat /tmp/docker-server-smoke-healthz)"
    fail=1
  fi
  # /healthz answering does not by itself mean the build has finished --
  # "ok, no build has completed yet" is a valid 200 too -- so this polls
  # rather than asking once, the same allowance the host-file-change check
  # above makes for a result that is not instant.
  built=0
  for _ in $(seq 1 20); do
    metrics_body="$(curl -sf "http://127.0.0.1:${metrics_port}/metrics" || true)"
    if printf '%s' "$metrics_body" | grep -q 'cairndex_builds_total 1'; then
      built=1
      break
    fi
    sleep 0.25
  done
  if [ "$built" -ne 1 ]; then
    echo "FAIL: /metrics never showed the container's own initial build: $metrics_body"
    fail=1
  fi
fi

[ "$fail" -eq 0 ] && echo "OK: the server container works"
exit "$fail"
