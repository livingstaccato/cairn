# Using cairn from a Hugo site

cairn ships `layouts/` and `assets/` as a Hugo component module. Import it, run
`cairn build` in `hugo` mode, and Hugo renders HTML, JSON and CSV for every
directory from one source.

## hugo.toml

```toml
[module]
  [[module.imports]]
    path = "github.com/livingstaccato/cairn"
```

Then pin it, so the templates and the binary that feeds them move together:

```sh
hugo mod get github.com/livingstaccato/cairn@v0.1.0
go install github.com/livingstaccato/cairn/cmd/cairn@v0.1.0
```

That is the whole configuration. There are no output formats to declare and no
media types to register: cairn writes `index.json`, `index.csv`, `index.txt` and
`SHA256SUMS` into each page's bundle, and Hugo publishes a bundle resource
verbatim. Hugo renders only the HTML.

Two things follow from that. The JSON a reader fetches is the exact bytes cairn
produced, so the two cannot disagree — they did, while Hugo re-rendered these
from frontmatter. And the frontmatter stays small, which matters: the listing
used to ride in it, and Hugo refused any directory past roughly ten thousand
entries with "too many YAML aliases for non-scalar nodes". The same
50,000-entry directory now renders in under half a second.

## cairn.yaml

`out:` points at the site's `content/`, because in `hugo` mode cairn writes one
`_index.md` per directory and Hugo publishes the rest.

```yaml
version: 1
mode: hugo
root: ./tree
out:  ./content
```

## What the module gives you

- `layouts/_default/cairn.html` — bound to `layout: cairn`, which cairn stamps
  on every page it writes.
- `layouts/partials/cairn/listing.html` — the listing, dispatching to a presenter.
  Entries come from the `index.json` resource in the page's bundle, not from
  `.Params`, so a custom template reads
  `(.Resources.GetMatch "index.json" | transform.Unmarshal).entries`.
- `layouts/partials/cairn/entries.html` — see [search integration](search-integration.md).
- `assets/cairn/cairn.css`, `cairn.js`, `icons.svg` — load them through Hugo
  Pipes. The reference theme in `themes/reference` shows the minimum.

It ships no `baseof`, header, footer or brand. Colors read your theme's custom
properties with fallbacks, so it inherits whatever design you already have.

## Two things that collide

`html` and `pep503` both target `index.html`. Asking for both in one directory
is a conflict cairn refuses rather than picking a winner — they are two
renderings of the same URL.

`present: styled` produces nothing in `direct` mode. Styled HTML needs your
theme, which cairn does not have, so only `bare` renders without Hugo.
