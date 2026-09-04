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
type bareData struct {
	Listing model.Listing
	Prose   string
}

// BareHTML renders a listing as a self-contained, script-free autoindex page.
//
// Everything it needs is inline: no stylesheet fetch, no icon font, no
// JavaScript. That is what lets it work airgapped, over plain HTTP, and in a
// text browser — the environments a bootstrap mirror actually gets read from.
func BareHTML(l model.Listing, prose string) ([]byte, error) {
	var buf bytes.Buffer
	if err := bareTmpl.Execute(&buf, bareData{Listing: l, Prose: prose}); err != nil {
		return nil, fmt.Errorf("render bare html for %s: %w", l.Path, err)
	}
	return buf.Bytes(), nil
}
