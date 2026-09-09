# Deploying a Cairndex site

A Cairndex site has two shapes on disk, and a client cannot tell them apart. Which
one you want follows from who owns the artifact tree: if Cairndex may write into
it, use one tree; if it must stay untouched, keep two and let the server put
them at one URL prefix.

![Two disk layouts producing the same URLs. In the first, root and out name the
same directory, so /srv/mirror/pool/ holds the artifact and the index.html,
index.json and SHA256SUMS Cairndex wrote beside it; it rsyncs whole and sha256sum
-c works where it sits. In the second, /srv/artifacts/pool/ holds only the
artifact and /srv/site/public/pool/ holds only what Cairndex wrote, so the artifact
tree is never written to. Both serve /pool/ubuntu.iso, /pool/index.html,
/pool/index.json and /pool/SHA256SUMS.](diagrams/mirror-overlay.svg)

Nothing is ever copied in either shape.

There is a third arrangement: `out` as a subdirectory of `root`. Cairndex skips
that subtree whole — it is neither listed nor walked — which is the same
exclusion it already applies to its own generated filenames, one level up. A
directory Cairndex fills is no more part of the tree it describes than a file Cairndex
wrote.

## Write the indexes into the tree

You never copy the repository. Point `root` and `out` at the same directory and
Cairndex writes `index.html`, `index.json`, `index.csv`, `index.txt` and
`SHA256SUMS` beside the files they describe:

```yaml
version: 1
mode: direct
root: /srv/mirror
out:  /srv/mirror
defaults:
  present:  bare
  checksum: sha256
  outputs:  [html, json, csv, txt, sums]
```

```nginx
server {
    root /srv/mirror;
    autoindex off;      # Cairndex's index.html *is* the autoindex
    index index.html;
}
```

One tree. It `rsync`s whole with its indexes attached, and `sha256sum -c` works
where it sits rather than needing a second path:

```sh
cd /srv/mirror/pool && sha256sum -c SHA256SUMS
```

Builds reach a fixed point: Cairndex excludes its own output from the listings, so
a second run produces byte-identical `SHA256SUMS` rather than checksumming the
first run's `index.json`. Re-running is cheap — hashes are cached on
`(path, size, mtime)`, so nothing unchanged is read again.

**If the tree is synced from upstream with `rsync --delete`,** exclude the
generated names or the sync removes them:

```sh
rsync -a --delete \
  --exclude 'index.html' --exclude 'index.json' --exclude 'index.csv' \
  --exclude 'index.txt' --exclude 'SHA256SUMS' --exclude '.cairndex-*' \
  upstream::mirror/ /srv/mirror/
cairndex build --config /srv/mirror/cairndex.yaml
```

Running Cairndex after each sync is what you want anyway: the content changed.

## Styled listings, which need Hugo

`present: styled` needs a theme, so it needs Hugo, and Hugo insists on writing
to its own `public/`. That is the one arrangement with a copy in it — but the
copy goes small into big, never the reverse:

```sh
cairndex build --config /srv/site/cairndex.yaml   # writes content/, ~68 KB
hugo --source /srv/site                    # renders public/
cp -R /srv/site/public/. /srv/mirror/      # deposits pages into the tree
```

Hugo never sees the artifacts. In `hugo` mode Cairndex writes one small `_index.md`
per directory plus `index.txt` and `SHA256SUMS`; the example site's `content/` is
68 KB for a tree Hugo never reads. Nothing in `public/` is a mirrored byte, so
depositing it is proportional to the number of directories, not to the size of
the repository.

If you would rather avoid the copy entirely, use `present: bare` and `direct`
mode. The bare listing is a real autoindex — no JavaScript, readable in `lynx` —
which is what most of a mirror should be anyway.

The two settings are not independent. The styled presenter *is* a Hugo template,
so `direct` mode can only render `bare`. Asking for `outputs: [html]` in `direct`
mode while `present:` is `styled` — which is the default — writes no HTML at all,
and Cairndex says so once per run:

```
no HTML written: the styled presenter needs mode: hugo; use present: bare to render HTML directly
```

The machine formats are unaffected; `index.json`, `index.csv`, `index.txt` and
`SHA256SUMS` are rendered in Go and do not depend on the presenter.

## What scales with a large mirror

Measured with `make bench`, which builds one directory holding N entries — the
shape that scales worst, and the realistic one for a package pool. Numbers from
an M-series laptop:

| Entries | Cairndex direct, cold | warm | Cairndex hugo | hugo render | index.html |
|---|---|---|---|---|---|
| 1,000 | 0.12s, 21 MB | 0.03s | 0.03s, 36 MB | 0.35s, 87 MB | 704 KB |
| 10,000 | 0.69s, 51 MB | 0.16s | 0.15s, 180 MB | 0.75s, 256 MB | 6.8 MB |
| 50,000 | 3.3s, 195 MB | 0.89s | 0.43s, 124 MB | 1.4s, 424 MB | 34 MB |

`direct` mode is comfortable throughout: 50,000 entries in 3.3 seconds cold and
0.89 warm, because the hash cache means an unchanged mirror is re-read only by
`stat`.

### The listing travels as a resource, not as frontmatter

The listing rode in YAML frontmatter, and between 10,000 and 11,000 entries Hugo
refused the page outright:

```
ERROR assemble: failed to create page from pageMetaSource /pool:
content/pool/_index.md:2:1: too many YAML aliases for non-scalar nodes
```

A decoder limit, not memory or time — and a normal apt pool exceeds it. The
entries now travel as an `index.json` resource in the page's bundle, which the
template unmarshals. On the same 50,000-entry directory that would not build:
`_index.md` went from 11 MB to 4 KB, `cairndex hugo` from 901 MB to 124 MB, and
Hugo renders it in 1.4 seconds.

### Page weight is bounded, not proportional

A directory's HTML renders at most `max_rendered` rows — 1,000 by default — and
the machine formats carry every entry. So the page a browser downloads is the
same size whether the directory holds a thousand packages or fifty thousand:

| entries | `index.html` bare | `index.html` styled | `index.json` |
| ------- | ----------------- | ------------------- | ------------ |
| 10,000  | 184 KB            | 680 KB              | 3.5 MB       |
| 50,000  | 184 KB            | 680 KB              | 17 MB        |

Uncapped, those pages are 1.8 MB and 6.6 MB at ten thousand entries and roughly
9 MB and 34 MB at fifty thousand. Nobody reads fifty thousand rows; a person
filters or reaches for the file. The cap makes the page a fixed cost and leaves
the directory completely represented in `index.json`, `index.csv` and
`index.txt`, which are never capped.

A capped page says so, under the listing, and names where the rest is:

> Showing the first **1,000** of 50,000 entries. The whole directory is in
> `index.txt`, one name per line, and `index.json`.

The styled listing's filter searches the rows it rendered, so on a capped page
its placeholder reads *Filter the first 1,000* rather than *Filter by name*. A
search box that silently covers 2% of a pool is worse than no search box.

Set it per rule, or `0` for no cap at all:

```yaml
rules:
  - match: "pool/**"
    max_rendered: 500

  # An archive nobody browses, where the complete listing is the point.
  - match: "releases/**"
    max_rendered: 0
```

## Checksums when the indexes live elsewhere

`SHA256SUMS` names files as they appear to a client, so it verifies from the
artifact tree, not from the directory Cairndex wrote it into:

```sh
cd /srv/artifacts/bootstrap
sha256sum -c /srv/site/public/bootstrap/SHA256SUMS
```

Once both trees are overlaid at one URL prefix, that distinction disappears —
which is the deployment this assumes. A client sees one directory:

```sh
curl -sO http://mirror.internal/bootstrap/linux/SHA256SUMS
curl -sO http://mirror.internal/bootstrap/linux/ubuntu-24.04.iso
sha256sum -c SHA256SUMS --ignore-missing
```

That matters more here than usual: over plain HTTP the transport guarantees
nothing, so the checksums are the integrity story.

## Cloudflare Pages

```toml
# wrangler.toml
pages_build_output_dir = "public"
```

Path variants work: `index.json`, `index.csv`, `tree.json`. `?format=` does
**not** — Cloudflare Pages `_redirects` cannot match on a query string. Use the
paths.

## nginx or caddy on a mirror you own

```nginx
server {
    listen 80;
    root /srv/site/public;

    # Cairndex's index.html *is* the autoindex, and it is static.
    autoindex off;
    index index.html;

    # Artifacts, served from their own tree at the same prefix.
    location /bootstrap/ {
        alias /srv/artifacts/bootstrap/;
        try_files $uri $uri/index.html =404;
    }
}
```

### Serving metadata as text

Each listing links to the `_meta.yaml` or `.cairndex.yaml` that describes it, so a
reader can see why an entry is titled the way it is. Without a MIME type those
download instead of opening:

```nginx
types {
    text/plain  yaml yml cfg list ipxe;
}
```

### Optional: `?format=` on a server you own

Pure file selection, not computation — which is why it works on a static tree,
and why `?depth=3` cannot be added the same way.

```nginx
map $arg_format $cairndex_ext {
    default "index.html";
    csv     "index.csv";
    json    "index.json";
}
location / {
    autoindex off;
    try_files $uri $uri/$cairndex_ext $uri/index.html =404;
}
```

Available only where you control the server. The path variants always work.

## Machine discovery

The reason the JSON exists — a provisioning script finding what it needs
instead of hardcoding paths:

```sh
curl -s http://mirror.internal/bootstrap/tree.json \
  | jq -r '.entries[] | select(.kind=="script" and .depth<=2) | .path'
```

For a shell with no `jq`, `index.txt` is one name per line and nothing else:

```sh
curl -s http://mirror.internal/bootstrap/index.txt | grep -v / \
  | xargs -n1 -I{} curl -sO "http://mirror.internal/bootstrap/{}"
```

Depth filtering is client-side. `tree.json` carries every descendant once with
its depth, so any depth query is a `jq` expression rather than a pre-generated
variant per level. No static host can interpret `?depth=`, on any deployment.

`recursive: true` produces it in both modes, and the directory's own page links
it from the format switcher, so it is reachable by a person reading the listing
as well as by a script that was told the path. The recursive listing is data and
gets no page of its own: a directory has one page, and that page is the listing
of the directory itself.

## Re-running, and what happens when files go away

Builds are repeatable. Cairndex records what it generated in `.cairndex-manifest.json`,
replaces its own output on a later run, and still refuses to touch a file it did
not create.

Removals are handled too. Anything the previous run wrote and this one did not is
deleted, and a directory left holding nothing goes with it — so a removed file
loses its digest, and a removed directory loses the whole listing published for
it rather than serving a page of links to things that are gone.

Only paths the manifest recorded are ever considered, so Cairndex can delete nothing
it did not create. Two consequences worth knowing:

- **Losing the manifest stops the build.** An `rsync --delete` or a `git clean`
  over the output directory is enough, as is a manifest written by a Cairndex old
  enough to have recorded output in a shape this one does not read. Cairndex will
  not overwrite files it can no longer prove it wrote. Recover with
  `cairndex build --adopt`, below. The run says `the manifest could not be read`
  before anything else — without that line the conflicts that follow name a path
  and say it already exists, which is true and points at the wrong thing
  entirely.
- **Two configs must not share one output root.** Each would prune the other's
  files, and there is no way to tell that apart from a directory that was
  legitimately removed.

- **A build that dies partway is recoverable.** Cairndex records what it managed to
  write before the error, alongside everything the previous run claimed. Without
  that the partial output would belong to nobody and `on_conflict: error` would
  refuse every later run until someone deleted the files by hand.

The manifest is replaced by a rename rather than rewritten in place, so a build
interrupted mid-save leaves either the old manifest or the new one and never a
half-written file. That matters more than it sounds: a manifest that will not
parse is read as Cairndex claiming nothing, and the next build then refuses every
file the last one wrote as somebody else's and prunes none of it. The hash cache
is written the same way, where the cost of losing it is a full re-hash.

A build also drops the cache records for files that are no longer in the tree,
and reports the count as `forgot`. Nothing used to remove one, so a mirror that
churns accumulated an entry for every file that had ever been in it — on a tree
of two million files, a cache of hundreds of megabytes, read and rewritten on
every build, almost all of it describing files that are gone. Only the region
the run actually rebuilt is swept: a full build sweeps from `root:`, and a
`cairndex watch` rebuild sweeps its own subtree, so the digests for the rest of the
mirror survive a change instead of being re-computed on the next full build. A
build that failed sweeps nothing, because it never reached the rest of its
scope.

### Getting a wedged tree back

Ownership has two states, and one edge leads back out of the bad one.

![A state diagram. A first build reaches Owned, where the manifest records every
file Cairndex wrote. An rsync --delete, a git clean, or a manifest this Cairndex cannot
parse moves it to Unclaimed, where the output is still there and the manifest is
not. From Unclaimed a plain build is refused on the first file, and a build with
on_conflict skip writes nothing and claims nothing; both return to Unclaimed
without changing the tree. Only build --adopt leads from Unclaimed back to
Owned.](diagrams/ownership.svg)

The two boxes along the bottom are the trap. They are what the first two
remedies an operator reaches for actually do, and both loop straight back:
nothing about the tree has changed.

The diagram is generated — `docs/diagrams/ownership.puml` is the source, and
`make diagrams` re-renders it.

```sh
cairndex build --dry-run --adopt   # read what it would take
cairndex build --adopt             # take it
```

`--adopt` claims output paths that already exist and Cairndex does not own, instead
of refusing them. It exists for one state: the manifest is gone or unreadable,
so every file Cairndex wrote is a file it no longer claims, and `on_conflict: error`
refuses all of them.

There was no way out of that before. Deleting the output is not one where `root:`
and `out:` are the same directory, because that deletes the artifacts. Neither is
`on_conflict: skip` — it leaves each conflicting path alone, so nothing is
written and nothing is ever claimed, and the mirror is frozen at whatever it held
when the manifest was lost.

What it claims is exactly the set of paths this build produces that already
exist. It never walks `out:` looking for files that seem generated, so it cannot
take one this build does not itself write, and `protect:` and path containment
are checked ahead of it and are not affected. Every claim is reported at warning
level with a count, because waiving the conflict check is not something to
discover afterwards.

One thing it does not do: output from an earlier config that this build no longer
generates stays unclaimed, and is therefore never pruned — `Prune` only removes
what the manifest records. `cairndex check` reports those as output Cairndex does not
own, and `cairndex check --remove-orphaned` deletes the ones it can show are Cairndex's
once you have read the list. That flag refuses while the manifest claims nothing,
since everything generated looks unowned in that state; adopt first, then remove.

Expect it to leave some behind, and expect that rather than reading it as a
failed removal. The report is name-based, which is what makes it useful in a
mirror; deletion is content-based, because in a mirror a foreign `index.html` is
an ordinary thing to find and losing it is unrecoverable. A file is removed only
when its own bytes identify it — a listing's shape, Cairndex's CSV header,
`_index.md` frontmatter, or the `generator` meta tag in a page Cairndex rendered.

`index.txt` and `SHA256SUMS` never qualify: one filename per line is what any
listing looks like, and `SHA256SUMS` is coreutils format, so Cairndex's is
indistinguishable from the one a Debian or release mirror ships. Stale ones
survive every `--remove-orphaned` run, stay in the report, and keep the check
exiting non-zero until you delete them yourself. That last part is worth knowing
in a pipeline: a run that deleted everything it could still exits non-zero when
anything was kept.

Pages published before the `generator` meta tag existed do not carry it, and a
rebuild will not give them one: Cairndex writes the paths it currently generates,
and a stale page is by definition at a path it no longer does. Those are kept
permanently and have to be deleted by hand. Only HTML written by a version that
emits the marker can be removed automatically later, so this is a one-time cost
against trees published before it.

### Seeing what a run would do first

```sh
cairndex build --dry-run
```

Every decision runs against the tree as it stands — what would be written, which
of those bodies differ from what is on disk, and every path `Prune` would
delete — and nothing under `out:` changes, including the manifest and the hash
cache. Reach for it after moving `out:`, after changing `index_basename` or
`outputs:`, and any time a build is about to run against a directory whose
manifest you are not sure of.

`--changed-to` still writes the file it names, so the deployment's transfer list
can be read before anything moves:

```sh
cairndex build --dry-run --changed-to /tmp/would-change.txt
```

## Verifying a published mirror

`cairndex check` reads back what a build recorded. It re-hashes every file
`SHA256SUMS` names, reports what the manifest claims and the disk no longer has,
finds output Cairndex does not own, and catches its own output being changed
after it was written.

```sh
cairndex check --config cairndex.yaml
```

The third finding is the one nothing else can produce. `sha256sum -c` confirms
the artifacts a client was told about; only the manifest knows which files Cairndex
actually wrote, so only Cairndex can tell a current index from one left behind when
`index_basename` or `outputs:` changed. Stale output is the dangerous kind — it
is still served, it still looks authoritative, and it describes a directory as
it was.

That last one needs the manifest's digests, and nothing else can see it.
Generated files appear in no `SHA256SUMS` — a listing leaves Cairndex's own output
out, or the build would never reach a fixed point — so a hand-edited
`index.json` is invisible to a client verifying checksums. The watcher cannot
see it either: it discards events on its own output by name, which is what stops
a rebuild loop, and a name cannot tell Cairndex's write from anyone else's. Running
`build` again repairs it.

Nothing is repaired. An operator unsure about a mirror needs to know what changed
before anything touches it, and a command that fixes what it finds cannot be run
to answer that question. A check that fails exits non-zero, so a deploy can
refuse to publish.

Two things it deliberately does not do. It never consults `.cairndex-cache.json`:
that cache is keyed by path, size and mtime, all three of which survive a
same-size edit with the timestamp restored, so it is the wrong oracle for a
tamper check. And it never follows a symlink — one standing where a file is
claimed is a finding, not something to hash through.

## Publishing only what moved

A rebuild of an unchanged tree writes nothing: identical output is skipped, and
`Listing.generated` holds the newest modification time among the entries rather
than the build clock, so two builds of one tree produce identical bytes.

That makes a delta deploy possible, and `--changed-to` is how a deployment finds
out which files those are:

```sh
cairndex build --config cairndex.yaml --changed-to /tmp/changed.txt
rsync -a --files-from=/tmp/changed.txt out/ mirror:/srv/mirror/
```

The format is rsync's `--files-from`: paths relative to the output directory,
one per line. A build that changed nothing writes an empty file rather than no
file, so a script can tell "nothing moved" from "the build never ran".

Deletions are not in the list. `Prune` has already removed them from the output
directory, so a sync of that directory carries them — a `--files-from` transfer
does not, and needs its own `--delete` pass.

## When the indexed tree is not the web root

`Entry.path` is rooted at Cairndex's `root:`. That is the site root only when the
two coincide, and on a site that indexes a subtree they do not:

```yaml
root: ./static/_odds     # where the files sit
base_path: /_odds        # where the site serves them
```

Without `base_path` a file the site serves at `/_odds/mockups/x.html` is
published in `index.json` as `/mockups/x.html`, so every link a consumer renders
from it is a 404 — and nothing says so, because the JSON is internally
consistent. Set it whenever `root:` is not what the web server treats as `/`.

It applies to every producer and to the listing's own path, so breadcrumbs and
entries agree. An authored `manifest` path that is already a full URL is left
alone: it names somewhere else on purpose.

## What a listing leaves out

`hide:` is a list of globs, matched against the path relative to `root:`. The
default hides the filesystem's own convention and nothing else:

```yaml
defaults:
  hide: ["**/.*"]        # the default
```

Everything else is yours to state:

```yaml
rules:
  - match: "site/**"
    hide: ["**/.*", "**/_*", "**/*.tmp", "drafts/**"]
```

Globs rather than a convention, because Cairndex cannot know which prefixes mean
"internal" in someone else's tree. A dot is the filesystem's own answer and is
the default; an underscore is a Hugo convention about pages, and a tree of
static artifacts is not pages — a published `_tradewars/` belongs in its
parent's listing, and `.DS_Store` does not.

A malformed glob matches nothing rather than everything: a pattern typo must not
silently empty a listing. A config carrying the removed `hidden:` key is refused
with a pointer to `hide:`, rather than having the key dropped in silence by the
YAML decoder.

One file can also opt out on its own, without a glob:

```yaml
# _meta.yaml
draft-notes.md:
  hidden: true
```

## Pointing an entry somewhere else

A file on disk can link elsewhere. The bytes stay where they are and keep their
size and digest; only the link moves:

```yaml
# _meta.yaml
the-talk.md:
  title: The talk
  url: https://example.invalid/watch
```

`base_path` leaves it alone, the way it leaves an authored `manifest` path that
is already a URL alone. Use it for a stub checked in beside its siblings whose
real home is somewhere else.

## Ordering by hand

`weight:` in a sidecar leads the sort, ahead of `sort:` and `dirs_first:`:

```yaml
# _meta.yaml
install-first.sh:
  weight: 1
```

Zero means unweighted rather than "weight zero", so weighting one entry does not
silently reorder the directory around it.

## A CDN that rewrites HTML breaks HTML checksums

Measured on Cloudflare Pages, on the same file at three stages:

```
source          38b11e8e   58,979 bytes
hugo public/    38b11e8e   58,979 bytes
production      8631d212   60,131 bytes
```

Cloudflare injects its Pages Analytics beacon and a bot-detection script before
`</body>`, so every HTML response is 1,152 bytes longer than the file Cairndex
hashed. `SHA256SUMS` is right and the served bytes are not the served file.

Non-HTML is untouched. PDFs, audio, archives and disk images fetched from the
same deployment verify normally — which is the case that matters, because those
are the artifacts anyone checksums. A mockup page is not.

Two settings cause it, and they are separate: Pages Web Analytics is a project
setting with no path scoping, and JavaScript Detections is a zone setting that a
Configuration Rule can disable for one path prefix. Turning off only one leaves
the other injecting. Weigh that against what it buys: on a mirror the binaries
already verify, and trading site-wide analytics and a bot signal to checksum
HTML is rarely worth it.

## Beside a package repository

This is the deployment Cairndex was built for: an APT or YUM mirror that already
has an owner. `dists/` is signed, `repodata/repomd.xml` is authoritative, and
both are verified by tools that did not ask for Cairndex's opinion. `protect:`
names what belongs to them:

```yaml
protect:
  - "dists/**"
  - "repodata/**"
```

A protected path is skipped, not an error. The glob is you declaring which paths
another tool owns, so refusing to write there is the whole point; failing the
build over it would make `protect:` useless for its only job. Cairndex reports the
count on every run, so a glob wider than you meant shows up as a directory with
no listing and a number that explains it:

```
build complete directories=14 files=11 outputs=39 pruned=0 protected=23
```

Protected paths are never recorded as written, so a later run cannot overwrite
them and pruning cannot delete them.

What you get is one tree. `pool/` and `Packages/` are indexed, browsable, and
carry `SHA256SUMS` that verifies where it sits:

```
$ cd pool/main/n/nginx && sha256sum -c SHA256SUMS
nginx_1.24.0-1_amd64.deb: OK
```

`dists/` and `repodata/` are untouched — every byte of the signed metadata is
what apt or dnf published — but they still appear in their parent's listing, so
a browser can walk into them and a machine reading `index.json` can discover
them. `apt-get update` and a human with a URL bar work against the same mirror.

## Run as a container

`cairndex watch --serve` already runs the rebuild-on-change watcher and the
HTTP server in one process, so a single container mounting one volume is a
real, working deployment for a homelab or a small internal mirror — not a
new mode built for this, the same command described earlier in this
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
(Caddy or nginx, both covered earlier in this document) in front for
anything internet-facing; this is what runs behind it.

### Health checks and metrics

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

### Exit codes

`build`, `watch` and `check` all exit `130` (`128 + SIGINT`, the shell's own
convention) when a Ctrl-C or `docker stop`'s `SIGINT` lands before they are
done — an operator's own interruption, not a failure to investigate. Every
other error is `1`. `0` is success.

This is deliberately not finer-grained than that: a bad config, a permission
error and a build that failed partway through are all `1`, distinguished
from each other by the log line on stderr rather than by the code, since a
script branching on "did this need a human" only ever needed the one real
split — interrupted versus broken.

One case reads as `0` on purpose, not `130`: `watch`'s own steady-state loop
(after the first build has already succeeded) treats being told to stop as
the expected end of "watches until interrupted", not as a build cut short —
`docker stop` on a long-running `watch --serve` container exits `0`. Only an
interruption *before* a build finishes — a plain `build`, `check`, or
`watch`'s own first build — is `130`.

### Build provenance

`provenance: true` writes `provenance.json` at the root of `out:` on every
full build — cairndex's own version, a SHA-256 hash of the `cairndex.yaml`
that drove the run, and a digest of every file the run wrote:

```json
{
  "cairndex_version": "v0.14.0",
  "config_sha256": "e3b0c4...",
  "started_at": "2026-09-10T02:15:00Z",
  "finished_at": "2026-09-10T02:15:04Z",
  "dirs": 42,
  "files": 137,
  "outputs": [
    {"path": "bootstrap/index.html", "sha256": "aa11..."},
    {"path": "bootstrap/index.json", "sha256": "bb22..."}
  ]
}
```

`outputs` lists name+digest pairs rather than one aggregate hash — closer to
an SLSA provenance predicate's own `subject` list than to a single
checksum-of-checksums — so a mismatch names the exact file that changed, and
`config_sha256` stays checkable by hand with nothing but `sha256sum
cairndex.yaml`. Off by default: unlike `build_info:`, this names the exact
config that produced a tree, which is not something to publish from a
mirror whose config might describe a private source layout.

`outputs` is JSON, not coreutils' `sha256sum -c` format, so verifying it
takes one reshape first — from the directory `provenance.json` sits in:

```sh
jq -r '.outputs[] | "\(.sha256)  \(.path)"' provenance.json | sha256sum -c -
```

This is a different check than `cairndex check` already does against its
own `.cairndex-manifest.json` (same digests, same purpose) — the point of
`provenance.json` is that this one-liner needs no cairndex install at all,
for a verifier who only has the published tree.

Written only by a full build (`cairndex build`, or `watch`'s own first
build) — `watch`'s later, incremental rebuilds leave an existing
`provenance.json` untouched rather than regenerating it for a subtree, the
same boundary the hash cache already draws at "the region the run
rebuilt". A long-running `watch --serve --metrics --config ...` process's
manifest therefore ages as files change underneath it; re-run `cairndex
build` (or restart `watch`) to refresh it.
