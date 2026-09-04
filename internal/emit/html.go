// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

package emit

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"

	"github.com/livingstaccato/cairn/internal/model"
)

//go:embed templates/bare.html
var bareTemplate string

// digestPreview is how many hex characters of a SHA-256 the listing shows. Long
// enough to compare against a SHA256SUMS line by eye, short enough not to push
// every other column off a narrow terminal.
const digestPreview = 12

// unitPrefixes are the binary size prefixes, in ascending order.
const unitPrefixes = "KMGTPE"

// HumanSize formats a byte count for display. It is the ONLY place in cairn
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
}

// BareHTML renders a listing as a self-contained, script-free autoindex page.
//
// Everything it needs is inline: no stylesheet fetch, no icon font, no
// JavaScript. That is what lets it work airgapped, over plain HTTP, and in a
// text browser — the environments a bootstrap mirror actually gets read from.
// maxRendered bounds the rows that reach the page; 0 renders every entry. The
// machine formats are never capped — a directory of fifty thousand packages is
// a 34 MB page and a perfectly ordinary index.json.
func BareHTML(l model.Listing, prose string, maxRendered int) ([]byte, error) {
	shown := l.Entries
	if maxRendered > 0 && len(shown) > maxRendered {
		shown = shown[:maxRendered]
	}
	var buf bytes.Buffer
	data := bareData{Listing: l, Prose: prose, Shown: shown, Total: len(l.Entries)}
	if err := bareTmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("render bare html for %s: %w", l.Path, err)
	}
	return buf.Bytes(), nil
}
