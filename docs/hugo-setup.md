# Using cairn from a Hugo site

cairn ships `layouts/` and `assets/` as a Hugo component module. Import it, run
`cairn build` in `hugo` mode, and Hugo renders HTML, JSON and CSV for every
directory from one source.

## hugo.toml

```toml
[module]
  [[module.imports]]
    path = "github.com/livingstaccato/cairn"

# index.json and index.csv land beside index.html, at the same URL.
[outputFormats]
  [outputFormats.cairnjson]
    mediaType = "application/json"
    baseName = "index"
    isPlainText = true
    notAlternative = true
  [outputFormats.cairncsv]
    mediaType = "text/csv"
    baseName = "index"
    isPlainText = true
    notAlternative = true

[outputs]
  home = ["html", "cairnjson", "cairncsv"]
  section = ["html", "cairnjson", "cairncsv"]

[mediaTypes]
  [mediaTypes."text/csv"]
    suffixes = ["csv"]
```

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
- `layouts/partials/cairn/listing.html` — the table, both presenters.
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
