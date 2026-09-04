# Deploying a cairn site

## Write the indexes into the tree

You never copy the repository. Point `root` and `out` at the same directory and
cairn writes `index.html`, `index.json`, `index.csv`, `index.txt` and
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
    autoindex off;      # cairn's index.html *is* the autoindex
    index index.html;
}
```

One tree. It `rsync`s whole with its indexes attached, and `sha256sum -c` works
where it sits rather than needing a second path:

```sh
cd /srv/mirror/pool && sha256sum -c SHA256SUMS
```

Builds reach a fixed point: cairn excludes its own output from the listings, so
a second run produces byte-identical `SHA256SUMS` rather than checksumming the
first run's `index.json`. Re-running is cheap — hashes are cached on
`(path, size, mtime)`, so nothing unchanged is read again.

**If the tree is synced from upstream with `rsync --delete`,** exclude the
generated names or the sync removes them:

```sh
rsync -a --delete \
  --exclude 'index.html' --exclude 'index.json' --exclude 'index.csv' \
  --exclude 'index.txt' --exclude 'SHA256SUMS' --exclude '.cairn-*' \
  upstream::mirror/ /srv/mirror/
cairn build -config /srv/mirror/cairn.yaml
```

Running cairn after each sync is what you want anyway: the content changed.

## Styled listings, which need Hugo

`present: styled` needs a theme, so it needs Hugo, and Hugo insists on writing
to its own `public/`. That is the one arrangement with a copy in it — but the
copy goes small into big, never the reverse:

```sh
cairn build -config /srv/site/cairn.yaml   # writes content/, ~68 KB
hugo --source /srv/site                    # renders public/
cp -R /srv/site/public/. /srv/mirror/      # deposits pages into the tree
```

Hugo never sees the artifacts. In `hugo` mode cairn writes one small `_index.md`
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
and cairn says so once per run:

```
no HTML written: the styled presenter needs mode: hugo; use present: bare to render HTML directly
```

The machine formats are unaffected; `index.json`, `index.csv`, `index.txt` and
`SHA256SUMS` are rendered in Go and do not depend on the presenter.

## What scales with a large mirror

Measured with `make bench`, which builds one directory holding N entries — the
shape that scales worst, and the realistic one for a package pool. Numbers from
an M-series laptop:

| Entries | cairn direct, cold | warm | cairn hugo | hugo render | index.html |
|---|---|---|---|---|---|
| 1,000 | 0.12s, 21 MB | 0.03s | 0.03s, 36 MB | 0.35s, 87 MB | 704 KB |
| 10,000 | 0.69s, 51 MB | 0.16s | 0.15s, 180 MB | 0.75s, 256 MB | 6.8 MB |
| 50,000 | 3.3s, 195 MB | 0.89s | 0.43s, 124 MB | 1.4s, 424 MB | 34 MB |

`direct` mode is comfortable throughout: 50,000 entries in 3.3 seconds cold and
0.89 warm, because the hash cache means an unchanged mirror is re-read only by
`stat`.

### There used to be a ceiling near 10,000 entries per directory

The listing rode in YAML frontmatter, and between 10,000 and 11,000 entries Hugo
refused the page outright:

```
ERROR assemble: failed to create page from pageMetaSource /pool:
content/pool/_index.md:2:1: too many YAML aliases for non-scalar nodes
```

A decoder limit, not memory or time — and a normal apt pool exceeds it. The
entries now travel as an `index.json` resource in the page's bundle, which the
template unmarshals. On the same 50,000-entry directory that would not build:
`_index.md` went from 11 MB to 4 KB, `cairn hugo` from 901 MB to 124 MB, and
Hugo renders it in 1.4 seconds.

### Page weight is bounded, not proportional

A directory's HTML renders at most `max_rendered` rows — 1,000 by default — and
the machine formats carry every entry. So the page a browser downloads is the
same size whether the directory holds a thousand packages or fifty thousand:

| entries | `index.html` bare | `index.html` styled | `index.json` |
| ------- | ----------------- | ------------------- | ------------ |
| 10,000  | 184 KB            | 680 KB              | 3.5 MB       |
| 50,000  | 184 KB            | 680 KB              | 17 MB        |

Before the cap those pages were 1.8 MB and 6.6 MB at ten thousand entries, and
roughly 9 MB and 34 MB at fifty thousand. Nobody reads fifty thousand rows; a
person filters or reaches for the file. The cap makes the page a fixed cost and
leaves the directory completely represented in `index.json`, `index.csv` and
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
artifact tree, not from the directory cairn wrote it into:

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

    # cairn's index.html *is* the autoindex, and it is static.
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

Each listing links to the `_meta.yaml` or `.cairn.yaml` that describes it, so a
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
map $arg_format $cairn_ext {
    default "index.html";
    csv     "index.csv";
    json    "index.json";
}
location / {
    autoindex off;
    try_files $uri $uri/$cairn_ext $uri/index.html =404;
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

Builds are repeatable. cairn records what it generated in `.cairn-manifest.json`,
replaces its own output on a later run, and still refuses to touch a file it did
not create.

Removals are handled too. Anything the previous run wrote and this one did not is
deleted, and a directory left holding nothing goes with it — so a removed file
loses its digest, and a removed directory loses the whole listing published for
it rather than serving a page of links to things that are gone.

Only paths the manifest recorded are ever considered, so cairn can delete nothing
it did not create. Two consequences worth knowing:

- **Losing the manifest stops the build.** An `rsync --delete` or a `git clean`
  over the output directory is enough. cairn will not overwrite files it can no
  longer prove it wrote; run once with `on_conflict: skip`, or clear the output
  directory, to recover.
- **Two configs must not share one output root.** Each would prune the other's
  files, and there is no way to tell that apart from a directory that was
  legitimately removed.

- **A build that dies partway is recoverable.** cairn records what it managed to
  write before the error, alongside everything the previous run claimed. Without
  that the partial output would belong to nobody and `on_conflict: error` would
  refuse every later run until someone deleted the files by hand.

## When the indexed tree is not the web root

`Entry.path` is rooted at cairn's `root:`. That is the site root only when the
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

## Underscore directories

`hidden:` takes three values, because two different conventions look alike:

| value      | hides                        |
| ---------- | ---------------------------- |
| `skip`     | `.dotfiles` and `_underscore` (default) |
| `dotfiles` | `.dotfiles` only             |
| `show`     | nothing                      |

A leading dot is a filesystem convention meaning hidden. A leading underscore is
a Hugo convention meaning "not a page", and it says nothing about a directory of
static artifacts that a site publishes and links to. On a tree like
`static/_odds/_tradewars/`, `skip` drops the entire published subtree and `show`
puts `.DS_Store` on the page; `dotfiles` is the one that describes what is
actually there.

## Beside a package repository

This is the deployment cairn was built for: an APT or YUM mirror that already
has an owner. `dists/` is signed, `repodata/repomd.xml` is authoritative, and
both are verified by tools that did not ask for cairn's opinion. `protect:`
names what belongs to them:

```yaml
protect:
  - "dists/**"
  - "repodata/**"
```

A protected path is skipped, not an error. The glob is you declaring which paths
another tool owns, so refusing to write there is the whole point; failing the
build over it would make `protect:` useless for its only job. cairn reports the
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
