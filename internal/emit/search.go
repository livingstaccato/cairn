// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package emit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"time"

	"github.com/livingstaccato/cairndex/internal/model"
)

// SearchFile is the fixed name of the standalone search index. Fixed rather
// than derived from the index basename: one directory publishes one index, and
// a recursive listing would otherwise write the file twice under two stems.
const SearchFile = "search-index.json"

// SearchScriptFile is the script the search page loads. Its bytes are
// assets/cairndex/search.js, embedded there rather than copied here, so
// mode: direct and mode: hugo — which reads the same file through Hugo
// Pipes — ship exactly the same code instead of two copies that can drift.
const SearchScriptFile = "search.js"

// SearchPageDir is where the search box lives: a subdirectory of the
// listing it searches, not a sibling index.html. mode: hugo cannot publish
// a raw .html bundle resource verbatim — Hugo parses any .html file inside
// a content bundle as a page source of its own and refuses by policy
// ("text/html is not whitelisted") — so the box needs a real page in both
// modes, one level under SearchFile and SearchScriptFile, which stay put.
const SearchPageDir = "search"

// searchPageTemplate is the page outputs: [search] gives a visitor for
// free: a box, a status line, and a results list, wired up by
// SearchScriptFile. Paths are one level up (SearchPageDir), and relative,
// so base_path or where the tree is mounted never has to reach this
// template. html/template, not text/template — .Path is a directory path,
// and names in a mirror are attacker-influenced the same way a filename is.
//
// Styled inline, the same reasoning as internal/serve/errorpage.go: this
// page ships in mode: direct with no guaranteed cairndex.css alongside it
// (mode: hugo publishes it as a raw bundle resource too, never through
// Hugo's own layout — see SearchPageDir's own comment), so a <link
// rel=stylesheet> would 404 as often as not. The color tokens mirror
// cairndex.css by value. Unlike errorpage.go, --c-signal is used here
// exactly the way cairndex.css itself uses it (focus rings, hover) rather
// than avoided: the "reserved for a verified checksum" carve-out in that
// file's comment is about not sending an affirmative-green signal from a
// rejected request, which does not apply to a working search box.
var searchPageTemplate = template.Must(template.New("search").Parse(
	`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="generator" content="cairndex">
<title>Search {{ .Path }}</title>
<style>
:root {
  --c-ink: #16181d; --c-slate: #5b6270; --c-paper: #fbfbfa; --c-rule: #e2e2de; --c-signal: #2f7d5d;
  --c-mono: ui-monospace, "SF Mono", SFMono-Regular, "Cascadia Mono", Menlo, Consolas, monospace;
  --c-sans: ui-sans-serif, system-ui, -apple-system, "Segoe UI", Helvetica, Arial, sans-serif;
}
@media (prefers-color-scheme: dark) {
  :root { --c-ink: #e6e7e9; --c-slate: #8b93a1; --c-paper: #16181d; --c-rule: #2b2f37; --c-signal: #5fbf92; }
}
* { box-sizing: border-box; }
body { margin: 0; min-height: 100vh; background: var(--c-paper); color: var(--c-ink); font-family: var(--c-sans); }
main { max-width: 34rem; margin: 0 auto; padding: 2.5rem 1.5rem; }
.back {
  display: inline-block; margin-bottom: 1.25rem; color: var(--c-slate);
  text-decoration: none; font-size: 0.8125rem;
}
.back:hover { color: var(--c-ink); text-decoration: underline; }
.back:focus-visible { outline: 2px solid var(--c-signal); outline-offset: 2px; border-radius: 2px; }
h1 { font-family: var(--c-mono); font-size: 1.125rem; font-weight: 600; margin: 0 0 1.25rem; }
input[type="search"] {
  width: 100%; font: inherit; font-family: var(--c-mono); font-size: 0.9375rem;
  padding: 0.55rem 0.75rem; border: 1px solid var(--c-rule); border-radius: 4px;
  background: var(--c-paper); color: var(--c-ink);
}
input[type="search"]:focus-visible { outline: 2px solid var(--c-signal); outline-offset: 1px; }
[data-cairndex-search-status] { margin: 0.75rem 0 0; font-size: 0.8125rem; color: var(--c-slate); min-height: 1.2em; }
[data-cairndex-search-results] { list-style: none; margin: 0.5rem 0 0; padding: 0; }
[data-cairndex-search-results] li { padding: 0.65rem 0; border-bottom: 1px solid var(--c-rule); }
[data-cairndex-search-results] li:last-child { border-bottom: none; }
[data-cairndex-search-results] a { color: var(--c-ink); text-decoration: none; font-weight: 500; }
[data-cairndex-search-results] a:hover { text-decoration: underline; }
[data-cairndex-search-results] a:focus-visible { outline: 2px solid var(--c-signal); outline-offset: 2px; }
[data-cairndex-search-results] small { color: var(--c-slate); font-size: 0.8125rem; }
@media (prefers-reduced-motion: reduce) { * { transition-duration: 0.01ms !important; animation-duration: 0.01ms !important; } }
</style>
</head>
<body>
<main>
<a class="back" href="../">&larr; back to {{ .Path }}</a>
<h1>Search {{ .Path }}</h1>
<div data-cairndex-search="../` + SearchFile + `">
<input type="search" placeholder="Search…" autofocus data-cairndex-search-input>
<p data-cairndex-search-status aria-live="polite"></p>
<ul data-cairndex-search-results></ul>
</div>
<script type="module" src="../` + SearchScriptFile + `"></script>
</main>
</body>
</html>
`))

// SearchRecord is one searchable thing, in the shape a browser search library
// consumes directly.
//
// Every field is one a person would type. Name is first because on a file
// mirror people search for filenames — "nginx_1.24.0-1_amd64.deb" — far more
// often than for titles, and a title is frequently absent. Digests, MIME types
// and layout fields like weight and depth are left out: nobody searches for
// them, and each one makes the file bigger for every visitor who downloads it.
type SearchRecord struct {
	Name     string    `json:"name"`
	Title    string    `json:"title,omitempty"`
	Path     string    `json:"path"`
	Summary  string    `json:"summary,omitempty"`
	Tags     []string  `json:"tags,omitempty"`
	Kind     string    `json:"kind"`
	IsDir    bool      `json:"is_dir"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

// Search renders a listing as a standalone search index.
//
// A bare JSON array, not an object with the array inside it, because that is
// what a browser search library takes: Fuse.js, MiniSearch and FlexSearch are
// all constructed from an array of records plus the names of the fields to
// index, so this file is usable with no adapter and no unwrapping. Lunr builds
// its own index from the same array. cairndex still dictates no record shape for
// a site that already has a search index — that site maps entries itself.
func Search(l model.Listing) ([]byte, error) {
	records := make([]SearchRecord, 0, len(l.Entries))
	for _, e := range l.Entries {
		records = append(records, SearchRecord{
			Name:     e.Name,
			Title:    e.Title,
			Path:     e.Path,
			Summary:  e.Summary,
			Tags:     e.Tags,
			Kind:     e.Kind,
			IsDir:    e.IsDir,
			Size:     e.Size,
			Modified: e.ModTime,
		})
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	// Filenames, like everywhere else cairndex writes JSON: escaping would leave
	// "a&b<c>.txt" naming nothing on disk.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(records); err != nil {
		return nil, fmt.Errorf("encode search index %s: %w", l.Path, err)
	}
	return buf.Bytes(), nil
}

// SearchPage renders the standalone search box for a directory that asked
// for outputs: [search], published one level under it at SearchPageDir.
// Without it, the JSON existed but nothing let a visitor actually search it
// short of wiring up Fuse.js or similar themselves.
func SearchPage(l model.Listing) ([]byte, error) {
	var buf bytes.Buffer
	if err := searchPageTemplate.Execute(&buf, struct{ Path string }{Path: l.Path}); err != nil {
		return nil, fmt.Errorf("render search page %s: %w", l.Path, err)
	}
	return buf.Bytes(), nil
}
