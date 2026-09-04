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

## No search of your own

`outputs: [search]` writes `search-index.json` beside the listing: a bare JSON
array of records, one per entry, carrying the fields a person types — `name`,
`title`, `path`, `summary`, `tags`, `kind`, `is_dir`, `size`, `modified`.
Digests and MIME types are left out; nobody searches for them, and every byte
is downloaded by every visitor who opens the search box.

An array rather than an object with the array inside it, because that is what a
browser search library takes. Fuse.js, MiniSearch and FlexSearch are each
constructed from an array of records plus the fields to index, so the file is
usable with no adapter:

```js
const records = await (await fetch('/bootstrap/search-index.json')).json();
const fuse = new Fuse(records, { keys: ['name', 'title', 'summary', 'tags'] });
fuse.search('nginx');
```

`name` is worth indexing first. On a file mirror people search for filenames —
`nginx_1.24.0-1_amd64.deb` — far more often than for titles, and a title is
frequently absent.

Under `recursive: true` the index covers the whole subtree, which is the useful
case: an index describing one directory of a deep tree finds almost nothing.
Without it the index describes its own directory, as `index.json` does.
