# Deployment reference

Behaviour worth knowing once a mirror is running: what scales, what a listing
leaves out, how a machine discovers the tree, and what provenance records.

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

## Build provenance

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
