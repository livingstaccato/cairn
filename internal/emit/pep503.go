// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package emit

import (
	"bytes"
	"fmt"
	"html/template"
	"net/url"

	"github.com/livingstaccato/cairn/internal/model"
)

// sha256HexLen is the length of a SHA-256 digest in hexadecimal.
const sha256HexLen = 64

// pep503Template follows PEP 503: an HTML page whose anchors are the available
// files. pip reads the #sha256= fragment to verify a download, which matters
// most on exactly the plain-HTTP mirrors cairn targets.
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

// PEP503 renders a listing as a Python simple-repository index page.
//
// A simple index is literally a page of anchor links, so this is near-zero
// marginal cost over the listing cairn already has.
func PEP503(l model.Listing) ([]byte, error) {
	data := struct {
		Title string
		Files []pep503File
	}{Title: l.Path}

	for _, e := range l.Entries {
		if e.IsDir {
			continue
		}
		href, err := hrefFor(e)
		if err != nil {
			return nil, err
		}
		data.Files = append(data.Files, pep503File{Name: e.Name, Href: href})
	}

	var buf bytes.Buffer
	if err := pep503Template.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("render pep503 for %s: %w", l.Path, err)
	}
	return buf.Bytes(), nil
}
