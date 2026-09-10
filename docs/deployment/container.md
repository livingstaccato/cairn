# Running as a container

`cairndex watch --serve` already runs the rebuild-on-change watcher and the
HTTP server in one process, so a single container mounting one volume is a
real, working deployment for a homelab or a small internal mirror — not a
new mode built for this, the same command described in [Operating a published mirror](operating.md)
document. `docker build --target server` builds an image with nothing in it
but the `cairndex` binary:

```sh
docker build --target server -t cairndex-server .
docker run -d -p 8080:8080 -v /path/to/your/tree:/data cairndex-server
```

Builds natively on amd64 or arm64 — a homelab NAS or a Raspberry Pi needs
nothing extra, just `docker build` run on that machine. CI proves both
architectures on real hardware (`docker-server-multiarch` in
`.github/workflows/ci.yml`), not through QEMU emulation.

The mounted volume is where `/data/cairndex.yaml` lives, alongside whatever
`root:`/`out:` it names — `root: ./tree`, `out: ./site` is exactly what
`cairndex init` itself writes, and needs nothing extra for this. There is no
Hugo in this image, only the binary, so the config must be `mode: direct`
(the default); a `mode: hugo` config will index correctly but publish no
browsable HTML, since nothing in this container renders Hugo templates.

Read before pointing this at the internet: `internal/serve`'s own package
comment says what this is —

> It is a viewer, not a deployment. Nothing here negotiates content,
> terminates TLS, authenticates anyone or writes to the tree.

Concretely: no TLS, no gzip, no access log, no rate limiting, no IP
allow/deny. `ci/docker-server-smoke.sh` (`make docker-server-smoke`) proves
the container itself works — serving the mounted tree, picking up a change
on the host without a restart, and refusing a path-traversal request — none
of which implies it is safe to expose directly. Put a real reverse proxy
(Caddy or nginx, both covered in [Static hosting](static-hosting.md), and below) in front for
anything internet-facing; this is what runs behind it.

## Health checks and metrics

`watch --serve --metrics` reserves two extra paths on the server, ahead of
whatever the served tree holds under those names:

- `GET /healthz` — JSON, `{"status":"ok"}` once the watcher's first build
  has succeeded, or `{"status":"degraded","reason":"..."}` (HTTP 503) if the
  served directory has disappeared or the last build failed. Point a
  container orchestrator's liveness/readiness probe at this.
- `GET /metrics` — Prometheus text exposition format: `cairndex_up`,
  request counts and durations by status class
  (`cairndex_http_requests_total{status="2xx|3xx|4xx|5xx"}`), and, since the
  build loop is what has this data, `cairndex_build_success`,
  `cairndex_build_timestamp_seconds`, `cairndex_build_duration_seconds`,
  `cairndex_build_files`, `cairndex_build_dirs`, `cairndex_builds_total` and
  `cairndex_build_failures_total`.

```sh
docker run -d -p 8080:8080 -v /path/to/your/tree:/data cairndex-server \
  watch --serve --addr 0.0.0.0:8080 --metrics --config /data/cairndex.yaml
```

Off by default (plain `--serve`, and the `server` image's own default
`CMD`): a directory that happens to hold a real `metrics/` or `healthz`
entry is served faithfully unless an operator explicitly trades those two
names away for this. `provide-telemetry`, the OTLP client cairndex's own
logs already go through (`internal/obs`), has no scrape endpoint of its
own to reuse here — it is a push client, and this project confines it to
one file on purpose (see `CLAUDE.md`'s Logging section) — so these two
paths are cairndex's own counters, not a pass-through.

A `docker-compose.yml` Prometheus scrape config for the container above:

```yaml
scrape_configs:
  - job_name: cairndex
    static_configs:
      - targets: ["cairndex-server:8080"]
```

## Choosing a mount shape

The container has no opinion about your disk layout; the config on the mounted
volume decides it. Two shapes are worth naming, and the difference is whether
Cairndex may write into the artifact tree.

**One volume, indexes written into the tree.** The mount *is* the artifact
directory, and `root:` and `out:` both name it. This is the shape to use when
you point a container at a directory that already holds files:

```yaml
# /srv/mirror/cairndex.yaml, mounted at /data
version: 1
mode: direct
root: /data
out:  /data
defaults:
  present:  bare
  checksum: sha256
  outputs:  [html, json, csv, txt, sums]
```

```sh
docker run -d -p 8080:8080 -v /srv/mirror:/data cairndex-server \
  watch --serve --addr 0.0.0.0:8080 --metrics --config /data/cairndex.yaml
```

`index.html`, `index.json`, `index.csv`, `index.txt` and `SHA256SUMS` land
beside the artifacts, and `.cairndex-manifest.json` and `.cairndex-cache.json`
at the root of the mount. If that tree is synced from upstream with `rsync
--delete`, exclude the generated names or the next sync removes them — see
[the overview](../deployment.md).

Note that `cairndex init` writes `root: ./tree`, `out: ./site`, which is a
different shape: it expects to *create* a tree under the mount rather than
describe one that is already there. Pointing that config at a directory of
existing artifacts indexes nothing, because the files are not under `./tree`.

**Two volumes, artifact tree read-only.** When the artifacts have another owner
and must not be written to, mount them read-only and give `out:` a volume of
its own:

```yaml
# mounted at /data/cairndex.yaml
version: 1
mode: direct
root: /artifacts
out:  /site
defaults:
  present:  bare
  checksum: sha256
  outputs:  [html, json, csv, txt, sums]
```

```sh
docker run -d -p 8080:8080 \
  -v /srv/artifacts:/artifacts:ro \
  -v /srv/site:/site \
  -v /srv/cairndex.yaml:/data/cairndex.yaml:ro \
  cairndex-server \
  watch --serve --addr 0.0.0.0:8080 --metrics --config /data/cairndex.yaml
```

The server then serves `out:`, which holds only what Cairndex wrote — so the
container alone does not serve the artifacts. Put the two at one URL prefix
with a reverse proxy, exactly as in the two-tree layout in
[the overview](../deployment.md), or accept that this container publishes
metadata and something else publishes bytes.

## When `/healthz` reports degraded

`{"status":"degraded"}` and HTTP 503 mean one of two things, and the response
says which:

```json
{"status":"degraded","reason":"the last build failed",
 "last_build":{"ok":false,"at":"...","error":"refusing to write pool/index.html: path already exists and cairndex did not create it (...)","duration_ms":5,"files":0,"dirs":0}}
```

The `error` field carries the build's own message, so the cause is in the
response rather than only in `docker logs`.

**The site keeps serving while degraded.** A failed rebuild does not remove the
last good indexes: `/`, the listings and the artifacts all keep answering 200.
Degraded means "what is published is going stale", not "the server is broken".

That distinction decides which probe to point at it. `/healthz` is a
**readiness** signal, not a liveness one — a container serving the last good
tree perfectly well will report 503 after one bad build, and a liveness probe
would restart it for that. Restarting does not fix a build that fails on the
tree's contents; it just fails again.

```yaml
# docker-compose.yml
services:
  cairndex:
    image: cairndex-server
    ports: ["8080:8080"]
    volumes:
      - /srv/mirror:/data
    command: >
      watch --serve --addr 0.0.0.0:8080 --metrics --config /data/cairndex.yaml
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://127.0.0.1:8080/healthz"]
      interval: 30s
      timeout: 3s
      retries: 3
      start_period: 10s
```

### Recovering, and the step that is easy to miss

The usual cause is a lost or unreadable manifest, which leaves every file
Cairndex wrote unclaimed — [Getting a wedged tree back](operating.md) explains
the state and why `--adopt` is the only edge out of it. In a container:

```sh
docker exec <name> cairndex build --dry-run --adopt --config /data/cairndex.yaml
docker exec <name> cairndex build --adopt --config /data/cairndex.yaml
```

**That repairs the tree but leaves `/healthz` degraded.** The `docker exec`
build is a separate process; the watcher's own record of the last build is
untouched, so the endpoint keeps reporting the failure that is now fixed. The
watcher has to run a build of its own before the status clears:

```sh
docker exec <name> touch /data/.cairndex-poke && docker exec <name> rm /data/.cairndex-poke
```

Any write under the watched tree does it. Restarting the container also works,
since `watch` builds once on startup. Confirm with `/healthz`, not with the
exit code of the repair:

```sh
curl -s http://127.0.0.1:8080/healthz
```

`cairndex_build_failures_total` in `/metrics` is cumulative and does not reset
when the tree recovers; `cairndex_build_success` is the current state.

### The other cause: the served directory is gone

`{"status":"degraded"}` also means `out:` has disappeared underneath the
process — an unmounted volume, or a `docker run` whose `-v` no longer resolves
on the host. `docker logs` names it. Nothing inside the container fixes this;
the mount does.

## Rolling back a bad build

There is no rollback command, and the reason is worth stating: a failed build
changes nothing, so there is usually nothing to roll back. The tree keeps
whatever the last successful build wrote.

What needs undoing is a build that *succeeded* and published something you did
not want — a widened `hide:`, a changed `index_basename`, a `max_rendered` you
regret. Restore the config and rebuild; the output is a function of the config
and the tree, so the previous config reproduces the previous output.

Two things make that safe to do in place:

- `cairndex build --dry-run` reports every write and every prune without
  touching `out:`, including the manifest and the hash cache. Run it before the
  rebuild, not after.
- Output that the restored config no longer generates is not pruned
  automatically — `Prune` only removes what the manifest records. `cairndex
  check` lists it and `cairndex check --remove-orphaned` deletes what it can
  prove is Cairndex's. Both are in [Operating a published mirror](operating.md).

## A reverse proxy in front

The container is what runs behind a proxy, never the thing facing the internet.
Both of these terminate TLS and pass through to `:8080`; neither serves files
itself, which is the difference between these and the static-hosting configs in
[Static hosting](static-hosting.md).

```caddy
mirror.example.com {
    encode zstd gzip
    reverse_proxy 127.0.0.1:8080
}
```

```nginx
server {
    listen 443 ssl;
    server_name mirror.example.com;

    ssl_certificate     /etc/ssl/mirror.crt;
    ssl_certificate_key /etc/ssl/mirror.key;

    gzip on;
    gzip_types text/html text/plain text/csv application/json;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host              $host;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # Neither endpoint should be public.
    location /metrics { deny all; }
    location /healthz { deny all; }
}
```

`--metrics` reserves `/healthz` and `/metrics` ahead of the served tree, so
anything the proxy does not block is reachable by anyone who can reach the
proxy. Scrape and probe them from inside the network, on the container port.

Compression belongs here: the server does no gzip of its own, and `index.json`
for a large pool is the response that most wants it.

## Stopping the container

`watch --serve` claims the disposition of both SIGINT and SIGTERM, so a
Ctrl-C, a `docker stop`, a `systemctl stop` and a Kubernetes pod shutdown all
end it the same way: the steady-state loop treats being told to stop as the
expected end of "watches until interrupted", logs `stopped watching`, and the
container exits `0`.

```sh
docker stop <name>                    # exits 0
docker kill -s SIGINT <name>          # exits 0
```

No `stop_signal:` is needed in a compose file, and an orchestrator reading the
exit code sees a graceful stop rather than a crash.

A stop that lands *before* the first build has finished is a different case and
exits `130`, because that build was cut short — see [Exit codes](operating.md).

Nothing is corrupted either way: the manifest and the hash cache are written by
rename, so an interrupted save leaves the old file or the new one and never a
half-written one.
