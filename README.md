# cairn

Static directory-index and artifact-repo generator. A Go binary and a Hugo
module in one repo, so the data it emits and the templates that render it share
a single version pin.

> **Status: unreleased.** Track `main`; pin `@vX.Y.Z` once a release is tagged.
> Pre-1.0, so the config schema and the JSON contract may still change —
> `version: 1` is checked, and a build refuses a schema it does not understand
> rather than guessing.

## Quickstart

```sh
go install github.com/livingstaccato/cairn/cmd/cairn@main
```

Put a `cairn.yaml` beside the tree you want indexed:

```yaml
version: 1
root: ./tree
out:  ./site

defaults:
  present:  bare
  outputs:  [html, json, csv, txt, sums]
  checksum: sha256
```

```sh
cairn build     # writes ./site
cairn serve     # read it back at http://127.0.0.1:8173
```

Both read `./cairn.yaml` unless `--config` says otherwise. Working from a clone
instead? `cairn build --config testdata/example/cairn.yaml` runs against the
example tree in this repo.

`present: bare` is the one line worth understanding on day one. It is a real
autoindex — no JavaScript, no icon font, renders in `lynx` — and it needs
nothing but the binary. The default is `styled`, which themes with your site but
needs the Hugo module; asked for without Hugo it writes no HTML and says so.

Run the build a second time and nothing moves. cairn keeps its own output out of
the listings and records what it wrote in `.cairn-manifest.json`, so a rebuild is
byte-identical, replaces only what cairn created, and refuses to touch anything
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
/bootstrap/linux/search-index.json  every entry, for a browser search box
```

The page is capped at 1,000 rows by default and the machine formats are not, so
`index.html` stays a fixed size on a fifty-thousand-package pool while
`index.json` still describes every one of them.

The output is plain files. It works behind nginx, on Cloudflare Pages, under
`python -m http.server`, and from `file://` on a USB stick.

## Why not just use autoindex

Everything in this space is a *server* — nginx and Apache autoindex,
`miniserve`, `dufs`, `gossa`, `filebrowser`. Static generators mostly stop at
`tree -H`. None of them give you a JSON or CSV view of a directory, per-file
checksums, or a listing that themes with the rest of your site.

That combination is what a file mirror actually needs: humans browse it, and
scripts discover from it.

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
- **`outputs`** — `html`, `json`, `csv`, `txt`, `sums`, `pep503`, `search`.

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

## Where the indexes go

`root` and `out` can name the same directory, and then the indexes land beside
the files they describe — one tree that `rsync`s whole and verifies where it
sits. Keep them separate and the artifact tree is never written to, with the web
server putting both at one URL prefix. A client cannot tell the two apart;
[Deployment](docs/deployment.md) draws both and says which to pick. Nothing is
ever copied either way.

## Watching

`cairn watch` builds once and then rebuilds only the subtree each change
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
cairn watch
```

The whole tree is registered before the first event is read, and the platform's
limit is checked before any of it is registered — macOS spends a descriptor per
file, Linux a watch per directory. A tree that does not fit is refused with the
setting to raise, rather than half-watched: a watcher that reports some changes
and says nowhere which ones it dropped leaves an index that is wrong and looks
fine.

`--settle` (250ms by default) is how long the tree has to be quiet before a
rebuild. Directories the build hides are not watched, and cairn's own output
never wakes it — including when `root` and `out` are the same directory.

`--serve` runs the viewer in the same process, so a change to the tree and the
page that shows it are one refresh apart:

```sh
cairn watch --serve
```

The socket opens before the first build — on a large tree that build is minutes
long, and an address already in use reported at the end of it is reported to
somebody who has stopped watching. `--addr` names where to listen, and means
nothing without `--serve`. If either half stops, so does the other.

## Verifying

`cairn check` reads back what a build recorded: it re-hashes every file
`SHA256SUMS` names, reports what the manifest claims and the disk no longer has,
finds output cairn does not own, and catches its own output being changed after
it was written.

```sh
cairn check
```

That last finding is the one nothing else can produce. `sha256sum -c` confirms
the artifacts a client was told about; only the manifest knows which files cairn
wrote, so only cairn can tell a current index from one left behind when
`index_basename` or `outputs:` changed. Nothing is repaired — an operator unsure
about a mirror needs to know what changed before anything touches it. A failed
check exits non-zero.

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
cairn build --dry-run
```

Deleting is the one thing cairn does that running it again cannot undo, and a
mistyped `out:` or a manifest left by a different config makes it delete a lot.
`--changed-to` still writes the file it names, so a deployment's transfer list
can be read before anything moves.

## Getting a wedged tree back

If `.cairn-manifest.json` is lost — an `rsync --delete` over the output
directory does it — every file cairn wrote becomes a file it no longer claims,
and `on_conflict: error` refuses all of them. `--adopt` claims those paths
instead of refusing them:

```sh
cairn build --dry-run --adopt
cairn build --adopt
```

There was no way out of that state before. Deleting the output is not one when
`root` and `out` are the same directory, and `on_conflict: skip` leaves each
conflicting path alone, so nothing is written and nothing is claimed and the
mirror stays frozen. What `--adopt` takes is exactly the paths this build
produces that already exist — it never walks the output looking for files that
seem generated — and every claim is reported, because waiving the conflict check
should not be something you find out about later.

## Reading it back

`cairn serve` puts the output directory behind a local HTTP server with the
right media types, so a generated listing can be read in a browser without Hugo
or nginx. It is the partner to `watch`.

```sh
cairn serve
```

Loopback only, and a port already in use is an error naming the port rather than
a silent move to another one — this hands out whatever is in a directory, and a
half-built mirror is nobody else's to read.

## Design principles

- **Zero external runtime assets.** No CDN, no web fonts, no icon font. Icons
  are an inline SVG sprite. Works airgapped.
- **The engine normalizes; templates render once.** One record type, filled by
  whichever source applies. Sizes stay exact bytes until the moment they are
  displayed.
- **Coexists with real package repositories.** `apt-ftparchive` and
  `createrepo_c` already produce APT and YUM metadata correctly, signing
  included. cairn indexes and presents around their output, and refuses to
  write into `dists/` or `repodata/`.
- **Never dictates a search record shape.** It exposes entries; your site maps
  them into whatever index it already has. Pagefind needs no integration at
  all, since the output is real HTML.

## Environment

`CAIRN_ENVIRONMENT` labels the build in cairn's log output. It defaults to
`production`, since cairn runs as a build step rather than a server. Nothing
else reads it, and nothing about a listing changes with it.

## Documentation

- [Hugo setup](docs/hugo-setup.md) — importing the module, output formats
- [Search integration](docs/search-integration.md) — folding entries into a site's existing index, or emitting a standalone one
- [Deployment](docs/deployment.md) — Cloudflare Pages, nginx, checksums, machine discovery

## License

MIT. See [LICENSE](LICENSE).
