# cairn

Static directory-index and artifact-repo generator. A Go binary and a Hugo
module in one repo, so the data it emits and the templates that render it share
a single version pin.

> **Status: design complete, implementation not started.** Nothing here works
> yet. This README describes the intended behaviour.

## What it does

Point it at a tree of files. For every directory it covers, it writes a human
page, machine-readable indexes, and optional checksums:

```
/bootstrap/linux/index.html     browsable listing
/bootstrap/linux/index.json     this directory
/bootstrap/linux/index.csv      this directory, as CSV
/bootstrap/linux/tree.json      recursive, opt-in per rule
/bootstrap/linux/tree.csv       recursive, flattened
/bootstrap/linux/SHA256SUMS     coreutils format
```

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

## Design

Each directory is configured by root defaults, path-glob rules, and an optional
per-directory override file. Three axes:

- **`source`** — `fs` (walk a real tree), `pages` (an existing Hugo section), or
  `manifest` (an authored list, for contents not on disk at build time).
- **`present`** — `styled` (themed, sortable, filterable) or `bare` (a genuine
  autoindex: no JavaScript, no icon font, renders in `lynx`).
- **`outputs`** — `html`, `json`, `csv`, `sums`, `pep503`.

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

## License

Apache-2.0. See [LICENSE](LICENSE).
