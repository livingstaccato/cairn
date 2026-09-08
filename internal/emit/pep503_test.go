// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package emit

import (
	"strings"
	"testing"

	"github.com/livingstaccato/cairndex/internal/config"
	"github.com/livingstaccato/cairndex/internal/model"
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
	if !strings.Contains(s, `<a href="/simple/requests/notes/">notes</a>`) {
		t.Errorf("a directory entry should appear as a linked project, got:\n%s", s)
	}
}

// A directory of projects is the other level PEP 503 defines: each entry's
// text has to be its normalized name, or a client that looked a project up
// by one spelling of its name does not recognize the entry filed under
// another. Without this, PEP503() over a real tree of project directories
// rendered a page with no anchors at all — every entry was a directory, and
// every one of them was skipped.
func TestPEP503NormalizesDirectoryNames(t *testing.T) {
	l := model.Listing{
		Path: "/simple/",
		Entries: []model.Entry{
			{Name: "My_Package.Name", IsDir: true, Path: "/simple/My_Package.Name/"},
			{Name: "requests", IsDir: true, Path: "/simple/requests/"},
		},
	}
	b, err := PEP503(l)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)

	want := `<a href="/simple/My_Package.Name/">my-package-name</a>`
	if !strings.Contains(s, want) {
		t.Errorf("directory name was not PEP 503 normalized, want %q in:\n%s", want, s)
	}
	if !strings.Contains(s, `<a href="/simple/requests/">requests</a>`) {
		t.Errorf("an already-normalized name should render unchanged:\n%s", s)
	}
}

func TestPEP503Name(t *testing.T) {
	cases := map[string]string{
		"requests":        "requests",
		"My_Package.Name": "my-package-name",
		"a--b__c..d":      "a-b-c-d",
		"UPPER":           "upper",
		"":                "",
	}
	for in, want := range cases {
		if got := pep503Name(in); got != want {
			t.Errorf("pep503Name(%q) = %q, want %q", in, got, want)
		}
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

// PEP503 renders every directory as a normalized project link and every
// file as a download, on the assumption that a real listing only ever holds
// one or the other. PEP503Mixed is how a caller checks that assumption
// before trusting the page it gets back.
func TestPEP503Mixed(t *testing.T) {
	dir := model.Entry{Name: "notes", IsDir: true, Path: "/simple/requests/notes/"}
	file := model.Entry{Name: "requests-2.32.3.tar.gz", Path: "/simple/requests/requests-2.32.3.tar.gz"}

	cases := map[string]struct {
		entries []model.Entry
		want    bool
	}{
		"pure files":  {[]model.Entry{file, file}, false},
		"pure dirs":   {[]model.Entry{dir, dir}, false},
		"empty":       {nil, false},
		"mixed":       {[]model.Entry{file, dir}, true},
		"mixed other": {[]model.Entry{dir, file}, true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := PEP503Mixed(model.Listing{Entries: c.entries}); got != c.want {
				t.Errorf("PEP503Mixed(%s) = %v, want %v", name, got, c.want)
			}
		})
	}
}

// ValidatePEP503Level is the hard check for a directory the operator
// explicitly declared a level for, unlike PEP503Mixed's best-effort warning
// for one that was not.
func TestValidatePEP503Level(t *testing.T) {
	dir := model.Entry{Name: "requests", IsDir: true}
	file := model.Entry{Name: "requests-2.32.3.tar.gz"}

	cases := map[string]struct {
		level   string
		entries []model.Entry
		wantErr bool
		names   []string
	}{
		"root of only directories": {config.PEP503LevelRoot, []model.Entry{dir, dir}, false, nil},
		"root with a stray file":   {config.PEP503LevelRoot, []model.Entry{dir, file}, true, []string{config.PEP503LevelRoot, file.Name}},
		"project of only files":    {config.PEP503LevelProject, []model.Entry{file, file}, false, nil},
		"project with a stray dir": {config.PEP503LevelProject, []model.Entry{file, dir}, true, []string{config.PEP503LevelProject, dir.Name}},
		"empty root is fine":       {config.PEP503LevelRoot, nil, false, nil},
		"empty project is fine":    {config.PEP503LevelProject, nil, false, nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			err := ValidatePEP503Level(c.level, model.Listing{Entries: c.entries})
			if c.wantErr && err == nil {
				t.Fatalf("expected an error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			for _, want := range c.names {
				if err != nil && !strings.Contains(err.Error(), want) {
					t.Errorf("error %v does not mention %q", err, want)
				}
			}
		})
	}
}

// PEP503JSON is what mode: hugo's pep503.html partial reads instead of
// building hrefs itself, so a hostile filename or a malformed digest is
// caught by the same code direct mode already tests, in exactly one place.
func TestPEP503JSONEncodesHref(t *testing.T) {
	l := model.Listing{Entries: []model.Entry{
		{Name: "x.whl", Path: `/simple/x/weird#file?name.tar.gz`, Kind: "archive"},
	}}
	b, err := PEP503JSON(l)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, `"href": "/simple/x/weird#file?name.tar.gz"`) {
		t.Errorf("href was not percent-encoded:\n%s", s)
	}
	if !strings.Contains(s, "%23") || !strings.Contains(s, "%3F") {
		t.Errorf("expected %%23 and %%3F in the encoded href:\n%s", s)
	}
}

func TestPEP503JSONNormalizesDirectoryNames(t *testing.T) {
	l := model.Listing{Entries: []model.Entry{
		{Name: "My_Package.Name", IsDir: true, Path: "/simple/My_Package.Name/"},
	}}
	b, err := PEP503JSON(l)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"name": "my-package-name"`) {
		t.Errorf("directory name was not PEP 503 normalized:\n%s", b)
	}
	if !strings.Contains(string(b), `"href": "/simple/My_Package.Name/"`) {
		t.Errorf("href should keep the real directory casing:\n%s", b)
	}
}

func TestPEP503JSONRejectsNonHexDigest(t *testing.T) {
	l := model.Listing{Entries: []model.Entry{
		{Name: "x.whl", Path: "/simple/x/x.whl", Kind: "archive", SHA256: "not-a-real-digest"},
	}}
	if _, err := PEP503JSON(l); err == nil {
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
