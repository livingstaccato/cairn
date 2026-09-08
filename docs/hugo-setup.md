# Using Cairndex from a Hugo site

Cairndex ships `layouts/` and `assets/` as a Hugo component module. Import it, run
`cairndex build` in `hugo` mode, and Hugo renders HTML, JSON and CSV for every
directory from one source.

## hugo.toml

```toml
[module]
  [[module.imports]]
    path = "github.com/livingstaccato/cairndex"
```

Track `main`. Once a release is tagged, pin that instead — `@vX.Y.Z` in both
commands, so the templates and the binary that feeds them move together.

```sh
hugo mod get github.com/livingstaccato/cairndex@main
go install github.com/livingstaccato/cairndex/cmd/cairndex@main
```

`hugo mod get` writes the revision it resolved into `go.mod` as a pseudo-version,
so the templates stay pinned; `go install` takes whatever `main` is at that
moment. The templates read what the binary writes, so update both together.

That is the whole configuration. There are no output formats to declare and no
media types to register: Cairndex writes `index.json`, `index.csv`, `index.txt` and
`SHA256SUMS` into each page's bundle, and Hugo publishes a bundle resource
verbatim. Hugo renders only the HTML.

Two things follow. The JSON a reader fetches is the exact bytes Cairndex produced,
so the page and the data cannot disagree. And the frontmatter stays small, which
is what keeps large directories buildable: with the listing inline, Hugo refuses
anything past roughly ten thousand entries with "too many YAML aliases for
non-scalar nodes". A 50,000-entry directory renders in under half a second.

## cairndex.yaml

`out:` points at the site's `content/`, because in `hugo` mode Cairndex writes one
`_index.md` per directory and Hugo publishes the rest.

```yaml
version: 1
mode: hugo
root: ./tree
out:  ./content
```

## What the module gives you

- `layouts/_default/cairndex.html` — bound to `layout: cairndex`, which Cairndex stamps
  on every page it writes.
- `layouts/partials/cairndex/listing.html` — the listing, dispatching to a presenter.
  Entries come from the `index.json` resource in the page's bundle, not from
  `.Params`, so a custom template reads
  `(.Resources.GetMatch "index.json" | transform.Unmarshal).entries`.
- `layouts/partials/cairndex/entries.html` — see [search integration](search-integration.md).
- `layouts/partials/cairndex/pep503.html` — the PEP 503 simple index, for a
  directory with `outputs: [pep503]`. Bypasses `listing.html` and `present:`
  entirely: PEP 503 defines a minimal page of anchors, and neither the
  breadcrumb nor the format switcher belongs on one.
- `assets/cairndex/cairndex.css`, `cairndex.js`, `icons.svg` — load them through Hugo
  Pipes. The reference theme in `themes/reference` shows the minimum.

It ships no `baseof`, header, footer or brand. Colors read your theme's custom
properties with fallbacks, so it inherits whatever design you already have.

## Two things that collide

`html` and `pep503` both target `index.html`. Asking for both in one directory
is a conflict Cairndex refuses at config load, rather than picking a winner —
they are two renderings of the same URL.

`present: styled` produces nothing in `direct` mode. Styled HTML needs your
theme, which Cairndex does not have, so only `bare` renders without Hugo.
