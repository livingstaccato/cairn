# Contributing Cairndex entries to your search index

Cairndex does not own a search record shape. The two sites this pattern came from
disagree — one indexes `{title, url, content, tags, date, excerpt}` for Fuse.js,
the other `{title, summary, kind, source_path, url, text}` — so owning either
would be wrong for the other.

Instead `partials/cairndex/entries.html` returns neutral maps and your site maps
them into whatever it already uses.

## A Fuse.js host

Six lines in `layouts/index.json`:

```go-html-template
{{- range partial "cairndex/entries.html" . -}}
  {{- $searchIndex = $searchIndex | append (dict
      "title" .title "url" .path "content" .summary
      "tags" .tags "date" .modified "excerpt" .summary) -}}
{{- end -}}
```

Worth knowing if your existing index filters by type — one such site indexed
only `where .Site.RegularPages "Type" "posts"`, which excludes every directory
listing. The partial finds pages by the presence of `.Params.cairndex` instead,
because Cairndex writes branch bundles and `site.RegularPages` does not include
them.

## A knowledge-corpus host

```go-html-template
{{- range partial "cairndex/entries.html" . -}}
  {{- $records = $records | append (dict
      "title" .title "summary" .summary "kind" .kind
      "url" .path "text" .summary "source_path" .section) -}}
{{- end -}}
```

## Two cases needing no integration

**Pagefind** indexes built HTML, and Cairndex emits real HTML. It works with no
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

## No search of your own

`outputs: [search]` writes `search-index.json` beside the listing: a bare JSON
array of records, one per entry, carrying the fields a person types — `name`,
`title`, `path`, `summary`, `tags`, `kind`, `is_dir`, `size`, `modified`.
Digests and MIME types are left out; nobody searches for them, and every byte
is downloaded by every visitor who opens the search box.

It also writes a working search box, at `search/` beside it — no Fuse.js, no
CDN, dependency-free the same way the styled listing's own sort and filter
are. Point a browser at `<directory>/search/` and type; `search.js` (the same
file in both modes) ranks name and title above summary and tags, since on a
file mirror people search for filenames — `nginx_1.24.0-1_amd64.deb` — far
more often than for titles, and a title is frequently absent. `search/`
rather than a sibling `search.html`: `mode: hugo` cannot publish a raw
`.html` bundle resource as a plain file — Hugo parses it as a page source of
its own and refuses by policy — so the box is a real page one level under
the index it searches, in both modes, with relative paths that never need to
know where the tree is mounted.

Under `recursive: true` the index covers the whole subtree, which is the useful
case: an index describing one directory of a deep tree finds almost nothing.
Without it the index describes its own directory, as `index.json` does.

An array rather than an object with the array inside it, because that is what a
browser search library takes too — the shipped box only reads name, title,
summary and tags, but the file works with no adapter for a site that already
has its own:

```js
const records = await (await fetch('/bootstrap/search-index.json')).json();
const fuse = new Fuse(records, { keys: ['name', 'title', 'summary', 'tags'] });
fuse.search('nginx');
```
