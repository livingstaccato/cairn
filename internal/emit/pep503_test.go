// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package emit

import (
	"strings"
	"testing"

	"github.com/livingstaccato/cairn/internal/model"
)

func TestPEP503(t *testing.T) {
	l := model.Listing{
		Path: "/simple/requests/",
		Entries: []model.Entry{
			{Name: "requests-2.32.3-py3-none-any.whl", Path: "/simple/requests/requests-2.32.3-py3-none-any.whl",
				Kind: "archive", SHA256: strings.Repeat("c", 64)},
			{Name: "requests-2.32.3.tar.gz", Path: "/simple/requests/requests-2.32.3.tar.gz", Kind: "archive"},
			{Name: "notes", IsDir: true, Path: "/simple/requests/notes/"},
		},
	}
	b, err := PEP503(l)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)

	if !strings.HasPrefix(s, "<!DOCTYPE html>") {
		t.Error("PEP 503 requires an HTML document")
	}
	want := `<a href="/simple/requests/requests-2.32.3-py3-none-any.whl#sha256=` + strings.Repeat("c", 64) +
		`">requests-2.32.3-py3-none-any.whl</a>`
	if !strings.Contains(s, want) {
		t.Errorf("hash fragment missing or malformed:\n%s", s)
	}
	if !strings.Contains(s, `<a href="/simple/requests/requests-2.32.3.tar.gz">requests-2.32.3.tar.gz</a>`) {
		t.Error("an unhashed file should link without a fragment")
	}
	if strings.Contains(s, "notes") {
		t.Error("directories must not appear in a simple index")
	}
}

func TestPEP503EscapesNames(t *testing.T) {
	l := model.Listing{Entries: []model.Entry{
		{Name: `a&b<c>.whl`, Path: `/simple/x/a&b<c>.whl`, Kind: "archive"},
	}}
	b, err := PEP503(l)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "<c>.whl<") {
		t.Error("filename was not HTML-escaped")
	}
}

// template.URL suppresses contextual escaping, so the path must be encoded
// before it gets there. A filename can contain a quote.
func TestPEP503EncodesHref(t *testing.T) {
	l := model.Listing{Entries: []model.Entry{
		{Name: `x.whl`, Path: `/simple/x/x" onmouseover=alert(1) .whl`, Kind: "archive"},
	}}
	b, err := PEP503(l)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, `href="/simple/x/x" onmouseover`) {
		t.Errorf("href attribute was broken out of:\n%s", s)
	}
	if !strings.Contains(s, "%22") {
		t.Errorf("quote was not percent-encoded:\n%s", s)
	}
}

func TestPEP503RejectsNonHexDigest(t *testing.T) {
	l := model.Listing{Entries: []model.Entry{
		{Name: "x.whl", Path: "/simple/x/x.whl", Kind: "archive", SHA256: "not-a-real-digest"},
	}}
	if _, err := PEP503(l); err == nil {
		t.Fatal("a malformed digest must fail rather than reach the fragment")
	}
}

func TestIsHex64(t *testing.T) {
	if !isHex64(strings.Repeat("aF0", 21) + "b") {
		t.Error("mixed-case 64-char hex should be accepted")
	}
	for _, bad := range []string{"", "abc", strings.Repeat("a", 63), strings.Repeat("a", 65), strings.Repeat("g", 64)} {
		if isHex64(bad) {
			t.Errorf("isHex64(%q) = true", bad)
		}
	}
}
