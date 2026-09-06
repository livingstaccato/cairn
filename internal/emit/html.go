// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package emit

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"

	"github.com/livingstaccato/cairndex/internal/model"
)

//go:embed templates/bare.html
var bareTemplate string

// digestPreview is how many hex characters of a SHA-256 the listing shows. Long
// enough to compare against a SHA256SUMS line by eye, short enough not to push
// every other column off a narrow terminal.
const digestPreview = 12

// unitPrefixes are the binary size prefixes, in ascending order.
const unitPrefixes = "KMGTPE"

// HumanSize formats a byte count for display. It is the ONLY place in cairndex
// that turns a size into text — Entry.Size stays exact bytes everywhere else,
// which is what the previous implementation got wrong when it rendered every
// file under a kilobyte as "0KB".
func HumanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), unitPrefixes[exp])
}

// DigestPreview shortens a digest for display, returning "" for an entry that
// has none so the cell renders empty rather than as a bare ellipsis.
func DigestPreview(sum string) string {
	if sum == "" {
		return ""
	}
	if len(sum) <= digestPreview {
		return sum
	}
	return sum[:digestPreview] + "…"
}

// GeneratorMarker is the line that identifies a page cairndex rendered.
//
// HTML is the one generated format whose bytes are otherwise anonymous. JSON
// carries a listing's own shape, CSV carries CSVHeader, and _index.md carries
// its frontmatter — each says who wrote it. An autoindex page does not, and
// "index.html" is the single most likely name for a file in a mirror that cairndex
// did not write: a package index, an extracted documentation tree, a generated
// API reference. --remove-orphaned needs to tell those apart from cairndex's own
// stale output before it deletes anything, and this is what lets it.
//
// A meta tag rather than a comment because it is the standard place to say
// this, and it survives a formatter that strips comments. It does not make the
// page depend on anything external, which the bare presenter must not do.
const GeneratorMarker = `<meta name="generator" content="cairndex">`

var bareTmpl = template.Must(template.New("bare").
	Funcs(template.FuncMap{"humanSize": HumanSize, "digest": DigestPreview}).
	Parse(bareTemplate))

// bareData is the template's view of a directory.
//
// Shown is what the page renders and may be shorter than the listing; Total is
// the whole directory. The two differ only when a cap applied, and the template
// uses the difference to say so.
type bareData struct {
	Listing model.Listing
	Prose   string
	Shown   []model.Entry
	Total   int
	AtRoot  bool
}

// BarePage is one directory's bare rendering. A struct rather than four
// positional arguments, and for the same reason HugoPage is one: the last of
// them would be an unlabelled bool at every call site.
type BarePage struct {
	Listing model.Listing
	Prose   string
	// MaxRendered bounds the rows the template renders; 0 renders every one.
	MaxRendered int
	// AtRoot suppresses the parent row. The top of the indexed tree has no
	// parent cairndex knows anything about: under base_path that link leaves the
	// mirror, and from file:// it walks out of the tree. Everywhere else it is
	// the only way back up.
	AtRoot bool
}

// BareHTML renders a listing as a self-contained, script-free autoindex page.
//
// Everything it needs is inline: no stylesheet fetch, no icon font, no
// JavaScript. That is what lets it work airgapped, over plain HTTP, and in a
// text browser — the environments a bootstrap mirror actually gets read from.
// maxRendered bounds the rows that reach the page; 0 renders every entry. The
// machine formats are never capped — a directory of fifty thousand packages is
// a 34 MB page and a perfectly ordinary index.json.
func BareHTML(p BarePage) ([]byte, error) {
	shown := p.Listing.Entries
	if p.MaxRendered > 0 && len(shown) > p.MaxRendered {
		shown = shown[:p.MaxRendered]
	}
	var buf bytes.Buffer
	data := bareData{
		Listing: p.Listing, Prose: p.Prose, Shown: shown,
		Total: len(p.Listing.Entries), AtRoot: p.AtRoot,
	}
	if err := bareTmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("render bare html for %s: %w", p.Listing.Path, err)
	}
	return buf.Bytes(), nil
}
