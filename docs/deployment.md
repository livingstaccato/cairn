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

## What scales with a large mirror

Measured with `make bench`, which builds one directory holding N entries — the
shape that scales worst, and the realistic one for a package pool. Numbers from
an M-series laptop:

| Entries | cairn direct, cold | warm | cairn hugo | hugo render | index.html |
|---|---|---|---|---|---|
| 1,000 | 0.12s, 21 MB | 0.03s | 0.03s, 36 MB | 0.35s, 87 MB | 704 KB |
| 10,000 | 0.69s, 51 MB | 0.16s | 0.15s, 180 MB | 0.75s, 256 MB | 6.8 MB |
| 50,000 | 3.3s, 195 MB | 0.89s | 0.82s, 901 MB | **fails** | — |

`direct` mode is comfortable throughout: 50,000 entries in 3.3 seconds cold and
0.89 warm, because the hash cache means an unchanged mirror is re-read only by
`stat`.

### hugo mode has a hard ceiling near 10,000 entries per directory

Between 10,000 (builds) and 11,000 (does not), Hugo refuses the page:

```
ERROR assemble: failed to create page from pageMetaSource /pool:
content/pool/_index.md:2:1: too many YAML aliases for non-scalar nodes
```

The listing rides in YAML frontmatter, and a decoder limit stops it — not
memory, not time. A normal apt pool exceeds this, so **`hugo` mode is not
currently usable for a flat pool of that size.** Use `direct` mode, which has no
such ceiling.

The fix is to stop carrying entries in frontmatter and hand them to the template
as a JSON page resource instead. Measured on the same 50,000-entry directory that
fails today: 0.36s and 152 MB, with the frontmatter down to nine lines. That is
not implemented yet.

### Page weight is the other limit

At 10,000 entries the styled listing is a 6.8 MB HTML page, which no amount of
build-time headroom makes pleasant to load. A directory that large wants
pagination or a `bare` listing, whichever the audience is.

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

## Re-running

Builds are repeatable. cairn records what it generated in `.cairn-manifest.json`
and replaces its own output on a later run, while still refusing to touch a file
it did not create. Nothing prunes yet: a file cairn generated once and no longer
would is left behind.
