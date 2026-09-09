![Cairndex](https://raw.githubusercontent.com/livingstaccato/cairndex/main/docs/images/brand/cairndex-banner.png)

# Cairndex

Static directory-index and artifact-repo generator. A Go binary and a Hugo
module in one repo, so the data it emits and the templates that render it share
a single version pin.

> Pre-1.0: the config schema and the JSON contract may still change —
> `version: 1` is checked, and a build refuses a schema it does not understand
> rather than guessing.

## Quickstart

```sh
go install github.com/livingstaccato/cairndex/cmd/cairndex@main
```

Put the files you want indexed under `./tree`, then:

```sh
cairndex init      # a commented cairndex.yaml, ready to build
cairndex build     # writes ./site
cairndex serve     # read it back at http://127.0.0.1:22476
```

Already running a Hugo site? `cairndex init --mode hugo` writes the other starter
instead: Hugo renders the HTML, so there is no `present:`/`outputs:` to pick — see
[Hugo setup](docs/hugo-setup.md).

`cairndex init` writes this, with the reasoning alongside each setting. It refuses
to replace a config that is already there.

```yaml
version: 1
root: ./tree
out:  ./site

defaults:
  present:  bare
  outputs:  [html, json, csv, txt, sums]
  checksum: sha256
```

Every command reads `./cairndex.yaml` unless `--config` says otherwise, and a key
Cairndex does not recognise is refused rather than ignored — a mistyped `checksum:`
used to mean no `SHA256SUMS` and a build that called itself complete.
`cairndex init` writes a `# yaml-language-server:` line pointing an editor at
[`cairndex.schema.json`](cairndex.schema.json), so a mistyped key shows up inline,
before the build ever runs. Working from a clone instead? `cairndex build --config
testdata/example/cairndex.yaml` runs against the example tree in this repo.

`present: bare` is the one line worth understanding on day one. It is a real
autoindex — no JavaScript, no icon font, renders in `lynx` — and it needs
nothing but the binary. The default is `styled`, which themes with your site but
needs the Hugo module; asked for without Hugo it writes no HTML and says so.

Run the build a second time and nothing moves. Cairndex keeps its own output out of
the listings and records what it wrote in `.cairndex-manifest.json`, so a rebuild is
byte-identical, replaces only what Cairndex created, and refuses to touch anything
it did not.

## What it does

Point it at a tree of files. For every directory it covers, it writes a human
page, machine-readable indexes, and optional checksums:

```
/bootstrap/linux/index.html     browsable listing
/bootstrap/linux/index.json     this directory
/bootstrap/linux/index.csv      this directory, as CSV
/bootstrap/linux/index.txt      one name per line, for xargs
/bootstrap/linux/tree.json      recursive, opt-in per rule
/bootstrap/linux/tree.csv       recursive, flattened
/bootstrap/linux/SHA256SUMS     coreutils format
/bootstrap/linux/search-index.json  every entry, searchable at search/
```

The page is capped at 1,000 rows by default and the machine formats are not, so
`index.html` stays a fixed size on a fifty-thousand-package pool while
`index.json` still describes every one of them.

The output is plain files. It works behind nginx, on Cloudflare Pages, under
`python -m http.server`, from `file://` on a USB stick, and from an object
storage bucket with no server in front of it at all.

## Why not just use autoindex

Everything in this space is a *server* — nginx and Apache autoindex,
`miniserve`, `dufs`, `gossa`, `filebrowser`. Static generators mostly stop at
`tree -H`. None of them give you a JSON or CSV view of a directory, per-file
checksums, or a listing that themes with the rest of your site.

That combination is what a file mirror actually needs: humans browse it, and
scripts discover from it.

Object storage — S3, GCS, R2 — has no autoindex to turn on: a bucket serves
exactly the files it holds and nothing else, so a directory with no
`index.html` is just a 404 or an XML listing meant for a client library, not a
person. Cairndex writes the index pages as ordinary files alongside the ones
they describe, so the bucket gets real browsable listings the same way
nginx's autoindex does, without running anything else — on a static-website
endpoint or CDN configured to resolve directory URLs to `index.html`. A plain
object endpoint (no directory-index resolution) needs the explicit
`/index.html` in the link.

```sh
# a provisioning script finding what it needs, without hardcoded paths
curl -s http://mirror.internal/bootstrap/tree.json \
  | jq -r '.entries[] | select(.kind=="script" and .depth<=2) | .path'

# integrity over plain HTTP, with no bespoke verifier
curl -sO http://mirror.internal/bootstrap/linux/SHA256SUMS
sha256sum -c SHA256SUMS --ignore-missing
```

## Configuring

Each directory is configured by root defaults, path-glob rules, and an optional
per-directory override file. Three axes:

- **`source`** — `fs` (walk a real tree), `pages` (an existing Hugo section), or
  `manifest` (an authored list, for contents not on disk at build time).
- **`present`** — `styled` (themed, sortable, filterable) or `bare` (a genuine
  autoindex: no JavaScript, no icon font, renders in `lynx`).
- **`outputs`** — `html`, `json`, `csv`, `txt`, `sums`, `pep503`, `search`, `atom`.

`pep503` renders both levels PEP 503 defines with the same code — a
directory entry as a normalized project link, a file entry as a download —
trusting that a directory this matches holds only one kind. A mixed one
only warns by default; `pep503_level: root` or `pep503_level: project`
turns that into a build error if the directory does not actually hold only
projects or only files, catching a misconfigured rule before it publishes.

`atom` writes `atom.xml` beside the normal listing: the directory's most
recently modified entries, newest first, capped at 100 — a feed says what
changed recently, not everything that exists, which `index.json`/`csv`/`txt`
already carry in full. No `base_url:` setting exists to build an absolute
feed from, so every link in it is site-root-relative like everywhere else
cairndex writes one.

```yaml
version: 1
root: ./tree
out:  ./web/content

defaults:
  source:   fs
  present:  styled
  outputs:  [html, json, csv]

rules:
  - match: "bootstrap/**"
    present:   bare
    checksum:  sha256
    recursive: true
    outputs:   [html, json, csv, sums]

  - match: "docs/**"
    source: pages
```

Files that cannot carry frontmatter get metadata from beside them — a
`_meta.yaml` keyed by filename, or a `<file>.meta.yaml` sidecar — so an ISO can
have a title and a summary.

Every listing page carries a small footer banner by default — when the build
ran, and the `--version` cairndex reports — a fact about the whole run rather
than a per-directory setting, so it lives at the config's root: `build_info:
false` turns it off.

## Where the indexes go

`root` and `out` can name the same directory, and then the indexes land beside
the files they describe — one tree that `rsync`s whole and verifies where it
sits. Keep them separate and the artifact tree is never written to, with the web
server putting both at one URL prefix. A client cannot tell the two apart;
[Deployment](docs/deployment.md) draws both and says which to pick. Nothing is
ever copied either way.

`out` may also be a subdirectory of `root`. Cairndex skips that subtree whole, so
the output is neither listed nor walked, and a rebuild stays a fixed point
instead of indexing the last build one level deeper.

## Watching

`cairndex watch` builds once and then rebuilds only the subtree each change
affects, which is what makes it usable against a tree of tens of thousands of
files: a change three directories down re-emits that directory, refreshes the
listings above it that name it, and leaves the rest alone.

![A directory tree showing what one change causes. A file changed in
bootstrap/linux/, so linux/ is rebuilt and recursed into, and its children deb/
and rpm/ are rebuilt with it. The directories above it, bootstrap/ and the root,
have their own listings refreshed but are not recursed into. Everything else —
bootstrap/bsd/, docs/ and docs/guides/ — is
untouched.](docs/diagrams/scoped-rebuild.svg)

The ancestors are the part that is easy to miss: a parent's entry for a
directory carries that directory's child count and modification time, so a file
added three levels down changes what every listing above it should say. Each one
is re-emitted without recursing, since its other children have not moved — which
is why `bsd/` above keeps the page it already had.

The changed directory is not always where the rebuild starts. A `recursive: true`
listing describes a whole subtree, so a change anywhere beneath one invalidates
it, and the highest such listing above the change becomes the scope instead.

```sh
cairndex watch
```

The whole tree is registered before the first event is read, and the platform's
limit is checked before any of it is registered — macOS spends a descriptor per
file, Linux a watch per directory. A tree that does not fit is refused with the
setting to raise, rather than half-watched: a watcher that reports some changes
and says nowhere which ones it dropped leaves an index that is wrong and looks
fine.

`--settle` (250ms by default) is how long the tree has to be quiet before a
rebuild. Directories the build hides are not watched, and Cairndex's own output
never wakes it — including when `root` and `out` are the same directory.

`--serve` runs the viewer in the same process, so a change to the tree and the
page that shows it are one refresh apart:

```sh
cairndex watch --serve
```

The socket opens before the first build — on a large tree that build is minutes
long, and an address already in use reported at the end of it is reported to
somebody who has stopped watching. `--addr` names where to listen, and means
nothing without `--serve`. If either half stops, so does the other.

## Verifying

`cairndex check` reads back what a build recorded: it re-hashes every file
`SHA256SUMS` names, reports what the manifest claims and the disk no longer has,
finds output Cairndex does not own, and catches its own output being changed after
it was written.

```sh
cairndex check
```

That last finding is the one nothing else can produce. `sha256sum -c` confirms
the artifacts a client was told about; only the manifest knows which files Cairndex
wrote, so only Cairndex can tell a current index from one left behind when
`index_basename` or `outputs:` changed. A failed check exits non-zero.

A plain check repairs nothing — an operator unsure about a mirror needs to know
what changed before anything touches it. The one finding you can ask it to act
on is stale output, because `Prune` structurally cannot: it only removes what
the manifest records, and a listing left behind by an older config was never
recorded.

```sh
cairndex check --remove-orphaned
```

It deletes less than the check reports, on purpose. The report answers whether
Cairndex *could* have written a file with that name, which is the right question to
put in front of a person and the wrong one to hand to `rm`: in a mirror `root:`
and `out:` are one directory, so nearly every file is somebody's artifact and the
names Cairndex generates are the most ordinary names in the tree. A mirrored package
index and an extracted documentation tree are both called `index.html`.

So removal asks a stronger question and answers it from the file's own bytes. A
listing carries its own shape, `index.csv` carries Cairndex's column header,
`_index.md` carries its frontmatter, and a page Cairndex rendered carries
`<meta name="generator" content="cairndex">`. Anything those tests do not vouch for
is kept, still reported, and still fails the check — so a name collision is
something you are told about rather than something you lose a file to.

Two formats can never answer it. `index.txt` is one filename per line, which is
what a listing of anything looks like, and `SHA256SUMS` is coreutils format by
design, so every publisher's is the shape Cairndex's is. Stale ones are reported and
left alone; delete those by hand.

It refuses outright when the manifest claims nothing. Everything generated then
looks unowned, so this would delete the whole published tree — that state is a
lost manifest, and [`build --adopt`](#getting-a-wedged-tree-back) is its repair.

## Publishing only what moved

Two builds of an unchanged tree produce identical bytes. Writes of identical
content are skipped, and `generated` in each listing holds the newest
modification time among its entries rather than the build clock, so nothing
depends on when the build ran or which timezone it ran in.

`--changed-to` writes the outputs whose bytes actually moved, in rsync's
`--files-from` format, so a mirror republishes the handful of listings that
changed instead of all of them.

## Seeing it first

`--dry-run` runs every decision a build makes and changes nothing under `out:`
— no listings, no manifest, no hash cache. It reports what would be written,
what of that differs from what is on disk, and, the reason to reach for it,
every file `Prune` would delete:

```sh
cairndex build --dry-run
```

Deleting is the one thing Cairndex does that running it again cannot undo, and a
mistyped `out:` or a manifest left by a different config makes it delete a lot.
`--changed-to` still writes the file it names, so a deployment's transfer list
can be read before anything moves.

## Getting a wedged tree back

If `.cairndex-manifest.json` is lost — an `rsync --delete` over the output
directory does it — every file Cairndex wrote becomes a file it no longer claims,
and `on_conflict: error` refuses all of them. `--adopt` claims those paths
instead of refusing them:

```sh
cairndex build --dry-run --adopt
cairndex build --adopt
```

There was no way out of that state before. Deleting the output is not one when
`root` and `out` are the same directory, and `on_conflict: skip` leaves each
conflicting path alone, so nothing is written and nothing is claimed and the
mirror stays frozen. What `--adopt` takes is exactly the paths this build
produces that already exist — it never walks the output looking for files that
seem generated — and every claim is reported, because waiving the conflict check
should not be something you find out about later.

## Reading it back

`cairndex serve` puts the output directory behind a local HTTP server with the
right media types, so a generated listing can be read in a browser without Hugo
or nginx. It is the partner to `watch`.

```sh
cairndex serve
```

Loopback only, and a port already in use is an error naming the port rather than
a silent move to another one — this hands out whatever is in a directory, and a
half-built mirror is nobody else's to read.

`--no-follow-symlinks` refuses any request path that traverses a symlink at
all, rather than resolving it and checking where it lands. Containment
already refuses one that resolves outside the served directory; this is for
serving a tree where no symlink should be followed regardless of where it
points.

A miss answers with a small styled page instead of the standard library's
plain text, naming the same generic "not found" every miss gets by
default — a missing file, an unreadable one, one outside the served
directory, or a symlink `--no-follow-symlinks` refused are one answer on
purpose: distinguishing them would tell a caller which paths exist on a
machine they cannot see. `--verbose-errors` names the specific reason
instead, the same one already reaching the log.

`--metrics` reserves `/healthz` and `/metrics` ahead of the served tree —
JSON liveness/readiness, and a Prometheus scrape target that also reports the
build loop's own success and timing under `watch --serve --metrics`. Off by
default, so a tree that happens to hold a real `metrics/` directory is still
served faithfully unless asked otherwise. See `docs/deployment.md`'s "Health
checks and metrics".

## Design principles

- **Zero external runtime assets.** No CDN, no web fonts, no icon font. Icons
  are an inline SVG sprite. Works airgapped.
- **The engine normalizes; templates render once.** One record type, filled by
  whichever source applies. Sizes stay exact bytes until the moment they are
  displayed.
- **Coexists with real package repositories.** `apt-ftparchive` and
  `createrepo_c` already produce APT and YUM metadata correctly, signing
  included. Cairndex indexes and presents around their output, and refuses to
  write into `dists/` or `repodata/`.
- **Never dictates a search record shape.** It exposes entries; your site maps
  them into whatever index it already has. Pagefind needs no integration at
  all, since the output is real HTML.

## Environment

`CAIRNDEX_ENVIRONMENT` labels the build in Cairndex's log output. It defaults to
`production`, since Cairndex runs as a build step rather than a server. Nothing
else reads it, and nothing about a listing changes with it.

`CAIRNDEX_LOG_FORMAT` picks the log line shape: `pretty` (the default, a
human-formatted terminal renderer, no ANSI color once the writer is not a
terminal) or `json`, for shipping build logs into Loki, ELK, Datadog or
similar without a regex parser in front of them.

## Documentation

- [Hugo setup](docs/hugo-setup.md) — importing the module, output formats
- [Search integration](docs/search-integration.md) — folding entries into a site's existing index, or emitting a standalone one
- [Deployment](docs/deployment.md) — Cloudflare Pages, nginx, checksums, machine discovery

## License

MIT. See [LICENSE](LICENSE).
