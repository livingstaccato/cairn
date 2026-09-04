// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

package emit

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/livingstaccato/cairn/internal/model"
)

// frontmatter is the shape a Hugo template will read back out of .Params.
type parsedFM struct {
	Title  string `yaml:"title"`
	Layout string `yaml:"layout"`
	Cairn  struct {
		Present string `yaml:"present"`
		Path    string `yaml:"path"`
		Count   int    `yaml:"count"`
	} `yaml:"cairn"`
}

func split(t *testing.T, b []byte) (parsedFM, string) {
	t.Helper()
	s := string(b)
	if !strings.HasPrefix(s, "---\n") {
		t.Fatal("must start with a YAML frontmatter fence")
	}
	parts := strings.SplitN(s, "\n---\n", 2)
	if len(parts) != 2 {
		t.Fatal("frontmatter fence not closed")
	}
	var fm parsedFM
	if err := yaml.Unmarshal([]byte(strings.TrimPrefix(parts[0], "---\n")), &fm); err != nil {
		t.Fatalf("frontmatter does not parse: %v", err)
	}
	return fm, parts[1]
}

func TestHugoContentFrontmatter(t *testing.T) {
	b, err := HugoContent(sample(), "Some prose.\n", "styled", "_meta.yaml", "a: b\n", []string{"json", "csv"})
	if err != nil {
		t.Fatal(err)
	}
	fm, body := split(t, b)

	if !strings.Contains(body, "Some prose.") {
		t.Error("prose must be the page body")
	}
	if fm.Layout != HugoLayout {
		t.Errorf("layout = %q, want %q", fm.Layout, HugoLayout)
	}
	if fm.Cairn.Present != "styled" {
		t.Errorf("present = %q", fm.Cairn.Present)
	}
	if fm.Cairn.Count != 2 {
		t.Errorf("count = %d, want 2", fm.Cairn.Count)
	}
	if fm.Title != "linux" {
		t.Errorf("title = %q, want the directory name", fm.Title)
	}
}

func TestHugoContentRootTitle(t *testing.T) {
	l := sample()
	l.Path = "/"
	b, err := HugoContent(l, "", "bare", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	fm, _ := split(t, b)
	if fm.Title == "" || fm.Title == "/" || fm.Title == "." {
		t.Errorf("root title = %q, want a readable name", fm.Title)
	}
}

// Prose containing a --- line must not truncate the frontmatter.
func TestHugoContentBodyWithFence(t *testing.T) {
	b, err := HugoContent(sample(), "before\n---\nafter\n", "bare", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, body := split(t, b)
	if !strings.Contains(body, "before") || !strings.Contains(body, "after") {
		t.Errorf("body mangled: %q", body)
	}
}

// The entries must NOT be in frontmatter. Carrying them there capped a
// directory at roughly ten thousand entries: Hugo refused the page with "too
// many YAML aliases for non-scalar nodes", a decoder limit rather than a memory
// or time one. They travel as an index.json resource instead.
func TestHugoContentOmitsEntries(t *testing.T) {
	b, err := HugoContent(sample(), "", "styled", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, "entries:") {
		t.Errorf("the listing leaked back into frontmatter:\n%s", s)
	}
	for _, name := range []string{"apt.list", "deep"} {
		if strings.Contains(s, name) {
			t.Errorf("entry %q reached frontmatter", name)
		}
	}
}

// Frontmatter size must not grow with the number of entries. This is the
// property the ceiling depended on.
func TestHugoContentSizeIsIndependentOfEntryCount(t *testing.T) {
	small, err := HugoContent(sample(), "", "styled", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	big := sample()
	for i := 0; i < 5000; i++ {
		big.Entries = append(big.Entries, model.Entry{Name: "pkg.deb", Path: "/pool/pkg.deb", Size: 1})
	}
	big.Count = len(big.Entries)
	large, err := HugoContent(big, "", "styled", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Only the count differs, so the two are within a handful of bytes.
	if d := len(large) - len(small); d > 32 {
		t.Errorf("frontmatter grew by %d bytes for 5000 more entries; it must not carry them", d)
	}
}

func TestHugoContentSourceIsOmittedWhenEmpty(t *testing.T) {
	b, err := HugoContent(sample(), "", "styled", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "source:") {
		t.Error("an absent metadata file must not emit an empty source key")
	}
	b, err = HugoContent(sample(), "", "styled", "_meta.yaml", "a: b\n", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "source: _meta.yaml") {
		t.Errorf("source not emitted:\n%s", b)
	}
}
