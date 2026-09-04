# Contributing cairn entries to your search index

cairn does not own a search record shape. The two sites this pattern came from
disagree — one indexes `{title, url, content, tags, date, excerpt}` for Fuse.js,
the other `{title, summary, kind, source_path, url, text}` — so owning either
would be wrong for the other.

Instead `partials/cairn/entries.html` returns neutral maps and your site maps
them into whatever it already uses.

## A Fuse.js host

Six lines in `layouts/index.json`:

```go-html-template
{{- range partial "cairn/entries.html" . -}}
  {{- $searchIndex = $searchIndex | append (dict
      "title" .title "url" .path "content" .summary
      "tags" .tags "date" .modified "excerpt" .summary) -}}
{{- end -}}
```

Worth knowing if your existing index filters by type — one such site indexed
only `where .Site.RegularPages "Type" "posts"`, which excludes every directory
listing. The partial finds pages by the presence of `.Params.cairn` instead,
because cairn writes branch bundles and `site.RegularPages` does not include
them.

## A knowledge-corpus host

```go-html-template
{{- range partial "cairn/entries.html" . -}}
  {{- $records = $records | append (dict
      "title" .title "summary" .summary "kind" .kind
      "url" .path "text" .summary "source_path" .section) -}}
{{- end -}}
```

## Two cases needing no integration

**Pagefind** indexes built HTML, and cairn emits real HTML. It works with no
wiring at all.

**No existing search** — build a standalone index from the same partial, or
skip it: `index.json` per directory is already machine-readable, and
`tree.json` covers a whole subtree in one fetch.

## Fields

| Key | Meaning |
|---|---|
| `title` | authored title, falling back to the filename |
| `path` | absolute URL path of the entry |
| `summary` | authored summary, or `""` |
| `tags` | authored tags, or empty |
| `modified` | RFC 3339 timestamp |
| `kind` | `dir`, `script`, `image`, `archive`, `doc`, `page`, `config`, `data`, `other` |
| `section` | path of the directory the entry appears in |
