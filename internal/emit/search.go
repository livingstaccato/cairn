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
// template. html/template, not text/template — .Title is a directory path,
// and names in a mirror are attacker-influenced the same way a filename is.
var searchPageTemplate = template.Must(template.New("search").Parse(
	`<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>{{ .Title }}</title></head>
<body>
<h1>{{ .Title }}</h1>
<div data-cairndex-search="../` + SearchFile + `">
<input type="search" placeholder="Search…" autofocus data-cairndex-search-input>
<p data-cairndex-search-status aria-live="polite"></p>
<ul data-cairndex-search-results></ul>
</div>
<script type="module" src="../` + SearchScriptFile + `"></script>
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
	if err := searchPageTemplate.Execute(&buf, struct{ Title string }{Title: "Search " + l.Path}); err != nil {
		return nil, fmt.Errorf("render search page %s: %w", l.Path, err)
	}
	return buf.Bytes(), nil
}
