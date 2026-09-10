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

## Where the rest of this lives

- **[Static hosting](deployment/static-hosting.md)** — nginx and caddy on a
  mirror you own, Cloudflare Pages, `?format=`, serving metadata as text,
  styled listings and the Hugo copy, indexing a subtree with `base_path`, and
  why a CDN that rewrites HTML breaks HTML checksums.
- **[Running as a container](deployment/container.md)** — `watch --serve` in
  one image, choosing a mount shape, health checks and metrics, what to do when
  `/healthz` reports degraded, a reverse proxy in front, and how to stop it.
- **[Operating a published mirror](deployment/operating.md)** — re-running and
  what happens when files go away, getting a wedged tree back with `--adopt`,
  dry runs, verifying what you published, publishing only what moved, and exit
  codes.
- **[Deployment reference](deployment/reference.md)** — what scales with a
  large mirror, machine discovery, what a listing leaves out, pointing an entry
  elsewhere, ordering by hand, sitting beside a package repository, and build
  provenance.
