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

| Cost | Scales with | Bounded by |
|---|---|---|
| Hashing | total bytes, first run only | `.cairn-cache.json`, keyed on `(path, size, mtime)` |
| `_index.md` size | entries *per directory*, since the listing rides in frontmatter | nothing yet; a directory holding tens of thousands of files produces one large YAML document for Hugo to parse |
| `tree.json` | descendants under a `recursive: true` rule | `tree_max_entries`, which errors rather than truncating |
| Hugo build | number of directories, not bytes | — |

`direct` mode has no frontmatter ceiling: cairn writes `index.json` itself and
never hands the listing to Hugo.

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
