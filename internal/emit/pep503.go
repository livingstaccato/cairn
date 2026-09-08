// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package emit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net/url"
	"strings"

	"github.com/livingstaccato/cairndex/internal/config"
	"github.com/livingstaccato/cairndex/internal/model"
)

// sha256HexLen is the length of a SHA-256 digest in hexadecimal.
const sha256HexLen = 64

// pep503Template follows PEP 503: an HTML page whose anchors are the available
// files. pip reads the #sha256= fragment to verify a download, which matters
// most on exactly the plain-HTTP mirrors cairndex targets.
var pep503Template = template.Must(template.New("pep503").Parse(
	`<!DOCTYPE html>
<html>
<head><meta name="pypi:repository-version" content="1.0"><title>Links for {{ .Title }}</title></head>
<body>
<h1>Links for {{ .Title }}</h1>
{{ range .Files }}<a href="{{ .Href }}">{{ .Name }}</a><br>
{{ end }}</body>
</html>
`))

type pep503File struct {
	Name string
	Href template.URL
}

// isHexDigit reports whether c is a hexadecimal digit in either case.
func isHexDigit(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

// isHex64 reports whether s is exactly 64 hexadecimal digits.
func isHex64(s string) bool {
	if len(s) != sha256HexLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isHexDigit(s[i]) {
			return false
		}
	}
	return true
}

// pep503Name normalizes a project name per PEP 503: lowercase, with any run
// of "-", "_" or "." collapsed to a single "-". Two spellings of the same
// project name must normalize identically, or a client that resolved a
// dependency by one spelling does not recognize an index entry filed under
// the other.
//
// Applied only to a directory entry — one level of a simple index naming the
// projects beneath it. A file's own name is never touched: it is the
// download pip fetches, and PEP 503 does not normalize those, only the
// project names that route to them.
func pep503Name(name string) string {
	var b strings.Builder
	inRun := false
	for _, r := range name {
		if r == '-' || r == '_' || r == '.' {
			if !inRun {
				b.WriteByte('-')
				inRun = true
			}
			continue
		}
		inRun = false
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}

// hrefFor builds the anchor target.
//
// template.URL suppresses html/template's contextual escaping, so the value
// handed to it must be CONSTRUCTED here rather than interpolated from a
// filename — names in a mirrored tree are not trusted input. url.URL.String
// percent-encodes the path, and the digest is validated as hex before it
// reaches the fragment.
func hrefFor(e model.Entry) (template.URL, error) {
	u := &url.URL{Path: e.Path}
	if e.SHA256 != "" {
		if !isHex64(e.SHA256) {
			return "", fmt.Errorf("entry %s: sha256 is not %d hex characters", e.Name, sha256HexLen)
		}
		u.Fragment = "sha256=" + e.SHA256
	}
	// #nosec G203 -- template.URL is the point of this function and the reason
	// it exists separately. The value is built by url.URL, never interpolated
	// from a filename, and the digest is validated as hex above.
	// TestPEP503EncodesHref covers a name carrying a quote and an event handler.
	return template.URL(u.String()), nil
}

// PEP503Mixed reports whether l holds both directory and non-directory
// entries.
//
// PEP 503 defines two levels — a root page whose entries are project
// directories, and each project's own page whose entries are its download
// files — and PEP503 renders both with the same code: a directory entry
// normalized as a project link, a file entry left as a download. That only
// renders a page faithful to either level so long as a listing actually
// holds just one kind of entry, which nothing enforces; a caller configuring
// which directories get outputs: [pep503] is what makes that true or not,
// and PEP503Mixed is how a caller checks it rather than assuming it.
func PEP503Mixed(l model.Listing) bool {
	var dir, file bool
	for _, e := range l.Entries {
		if e.IsDir {
			dir = true
		} else {
			file = true
		}
		if dir && file {
			return true
		}
	}
	return false
}

// ValidatePEP503Level rejects a listing that does not match the PEP 503
// level an operator explicitly declared for it with pep503_level: root
// holds only project directories, project holds only distribution files.
//
// Unlike PEP503Mixed's warning, an operator who set pep503_level staked a
// specific, checkable claim about the directory, so a violation fails the
// build rather than rendering a page that is silently untrue to it.
func ValidatePEP503Level(level string, l model.Listing) error {
	for _, e := range l.Entries {
		switch {
		case level == config.PEP503LevelRoot && !e.IsDir:
			return fmt.Errorf("pep503_level: %s but %s is a file, not a project directory",
				config.PEP503LevelRoot, e.Name)
		case level == config.PEP503LevelProject && e.IsDir:
			return fmt.Errorf("pep503_level: %s but %s is a directory, not a distribution file",
				config.PEP503LevelProject, e.Name)
		}
	}
	return nil
}

// PEP503 renders a listing as a Python simple-repository index page.
//
// A simple index is literally a page of anchor links, so this is near-zero
// marginal cost over the listing cairndex already has. The same rendering
// serves both levels PEP 503 defines: the root page, whose entries are
// project directories, and each project's own page, whose entries are its
// download files — PEP503 does not need to be told which, since a directory
// only ever holds one or the other in a real mirror, and a directory entry's
// name is normalized while a file entry's is not either way. PEP503Mixed is
// how a caller checks that assumption before trusting this renders either
// level correctly.
func PEP503(l model.Listing) ([]byte, error) {
	entries, err := pep503Entries(l)
	if err != nil {
		return nil, err
	}
	data := struct {
		Title string
		Files []pep503File
	}{Title: l.Path}
	for _, e := range entries {
		// #nosec G203 -- e.Href was built by hrefFor inside pep503Entries, from
		// url.URL, never interpolated from a filename; round-tripping the
		// already-safe string back to template.URL here does not reopen that.
		data.Files = append(data.Files, pep503File{Name: e.Name, Href: template.URL(e.Href)})
	}

	var buf bytes.Buffer
	if err := pep503Template.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("render pep503 for %s: %w", l.Path, err)
	}
	return buf.Bytes(), nil
}

// PEP503Entry is one rendered anchor — a project directory or a distribution
// file — already name-normalized and href-encoded. It is the shape mode:
// hugo's pep503.json bundle resource carries, so the Hugo template that reads
// it needs no encoding or normalization logic of its own.
type PEP503Entry struct {
	Name string `json:"name"`
	Href string `json:"href"`
}

// pep503Entries builds the anchors a PEP 503 page renders: hrefFor encodes
// and validates each one, pep503Name normalizes a directory's name. Shared
// by PEP503, which renders it as HTML directly, and PEP503JSON, which hands
// the same data to mode: hugo's template — so a hostile filename or a
// malformed digest is caught in exactly one place regardless of which mode
// renders the page.
func pep503Entries(l model.Listing) ([]PEP503Entry, error) {
	var out []PEP503Entry
	for _, e := range l.Entries {
		href, err := hrefFor(e)
		if err != nil {
			return nil, err
		}
		name := e.Name
		if e.IsDir {
			name = pep503Name(e.Name)
		}
		out = append(out, PEP503Entry{Name: name, Href: string(href)})
	}
	return out, nil
}

// PEP503JSON renders a listing's PEP 503 anchors as the pep503.json bundle
// resource mode: hugo's pep503.html partial reads, indented the same way
// JSON does: these files are often committed alongside the tree they
// describe, so a one-entry change should read as a one-line diff.
func PEP503JSON(l model.Listing) ([]byte, error) {
	entries, err := pep503Entries(l)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(entries); err != nil {
		return nil, fmt.Errorf("encode pep503 entries %s: %w", l.Path, err)
	}
	return buf.Bytes(), nil
}
