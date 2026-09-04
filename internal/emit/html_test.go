// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

package emit

import (
	"strings"
	"testing"
)

func TestHumanSize(t *testing.T) {
	cases := map[int64]string{
		0: "0 B", 1: "1 B", 512: "512 B", 999: "999 B", 1023: "1023 B",
		1024: "1.0 KiB", 1536: "1.5 KiB",
		1048576: "1.0 MiB", 1073741824: "1.0 GiB",
		1099511627776: "1.0 TiB",
	}
	for in, want := range cases {
		if got := HumanSize(in); got != want {
			t.Errorf("HumanSize(%d) = %q, want %q", in, got, want)
		}
	}
	// The bug this function exists to prevent.
	if HumanSize(512) == "0KB" {
		t.Fatal("regression: sub-kilobyte sizes must not render as 0")
	}
}

func TestBareHTMLHasNoScriptOrExternalAssets(t *testing.T) {
	b, err := BareHTML(sample(), "")
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(b))
	for _, forbidden := range []string{"<script", "fontawesome", "fa-solid", "//cdn", "https://", "http://"} {
		if strings.Contains(s, forbidden) {
			t.Errorf("bare output contains %q; it must be self-contained and JS-free", forbidden)
		}
	}
}

func TestBareHTMLListsEntries(t *testing.T) {
	b, err := BareHTML(sample(), "")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"apt.list", "deep/", `href="/bootstrap/linux/apt.list"`, "64 B",
		`<a href="../">../</a>`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
}

func TestBareHTMLEscapes(t *testing.T) {
	l := sample()
	l.Entries[1].Title = `<script>alert(1)</script>`
	l.Entries[1].Name = `a&b.txt`
	b, err := BareHTML(l, "")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, "<script>alert") {
		t.Error("title was not escaped")
	}
	if !strings.Contains(s, "a&amp;b.txt") {
		t.Error("name was not escaped")
	}
}

func TestBareHTMLIncludesProse(t *testing.T) {
	b, err := BareHTML(sample(), "Base images for PXE installs.")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Base images for PXE installs.") {
		t.Error("directory prose not rendered")
	}
}

func TestBareHTMLShowsTruncatedDigest(t *testing.T) {
	b, err := BareHTML(sample(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), strings.Repeat("a", 12)+"…") {
		t.Error("expected a truncated digest in the listing")
	}
	// A directory has no digest and must not render a bare ellipsis.
	if strings.Contains(string(b), "<td>…</td>") {
		t.Error("an entry without a digest rendered an empty truncation")
	}
}
