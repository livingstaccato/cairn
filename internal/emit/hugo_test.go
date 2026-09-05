// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

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
		AtRoot  bool   `yaml:"at_root"`
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
	b, err := HugoContent(HugoPage{Listing: sample(), Prose: "Some prose.\n", Present: "styled", Source: "_meta.yaml", SourceText: "a: b\n", Formats: []string{"json", "csv"}})
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
	b, err := HugoContent(HugoPage{Listing: l, Present: "bare"})
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
	b, err := HugoContent(HugoPage{Listing: sample(), Prose: "before\n---\nafter\n", Present: "bare"})
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
	b, err := HugoContent(HugoPage{Listing: sample(), Present: "styled"})
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
	small, err := HugoContent(HugoPage{Listing: sample(), Present: "styled"})
	if err != nil {
		t.Fatal(err)
	}

	big := sample()
	for i := 0; i < 5000; i++ {
		big.Entries = append(big.Entries, model.Entry{Name: "pkg.deb", Path: "/pool/pkg.deb", Size: 1})
	}
	big.Count = len(big.Entries)
	large, err := HugoContent(HugoPage{Listing: big, Present: "styled"})
	if err != nil {
		t.Fatal(err)
	}

	// Only the count differs, so the two are within a handful of bytes.
	if d := len(large) - len(small); d > 32 {
		t.Errorf("frontmatter grew by %d bytes for 5000 more entries; it must not carry them", d)
	}
}

func TestHugoContentSourceIsOmittedWhenEmpty(t *testing.T) {
	b, err := HugoContent(HugoPage{Listing: sample(), Present: "styled"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "source:") {
		t.Error("an absent metadata file must not emit an empty source key")
	}
	b, err = HugoContent(HugoPage{Listing: sample(), Present: "styled", Source: "_meta.yaml", SourceText: "a: b\n"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "source: _meta.yaml") {
		t.Errorf("source not emitted:\n%s", b)
	}
}

func TestHugoContentCarriesRenderCap(t *testing.T) {
	b, err := HugoContent(HugoPage{Listing: sample(), Present: "styled", MaxRendered: 1000})
	if err != nil {
		t.Fatal(err)
	}
	// The template applies the cap, so the number has to reach it. Deciding it
	// in the template instead would put the policy in two places and let the
	// two presenters disagree about the same directory.
	if !strings.Contains(string(b), "max_rendered: 1000") {
		t.Errorf("frontmatter omits the render cap:\n%s", b)
	}

	// Zero means unlimited and is the absence of a cap, not a cap of zero.
	uncapped, err := HugoContent(HugoPage{Listing: sample(), Present: "styled"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(uncapped), "max_rendered") {
		t.Errorf("an uncapped page must not declare a cap:\n%s", uncapped)
	}
}

// The Hugo templates render the bare presenter themselves — emitHugo never calls
// BareHTML — so the parent-row rule has to reach them as data. It could not:
// AtRoot was a field on BarePage and nothing else, so the Go template suppressed
// the row at the top of the tree and the Hugo partial had no way to know it
// should.
//
// A template cannot derive this from the path either. Under base_path every
// listing path carries the prefix, so "/" is not what the top of the tree looks
// like.
func TestHugoContentCarriesAtRoot(t *testing.T) {
	b, err := HugoContent(HugoPage{Listing: sample(), AtRoot: true})
	if err != nil {
		t.Fatal(err)
	}
	fm, _ := split(t, b)
	if !fm.Cairn.AtRoot {
		t.Errorf("at_root did not reach the frontmatter:\n%s", b)
	}
}

// Below the top it must be absent or false, so the partial keeps the only way
// back up.
func TestHugoContentOmitsAtRootBelowTheTop(t *testing.T) {
	b, err := HugoContent(HugoPage{Listing: sample()})
	if err != nil {
		t.Fatal(err)
	}
	fm, _ := split(t, b)
	if fm.Cairn.AtRoot {
		t.Errorf("at_root is set for a listing below the top:\n%s", b)
	}
}
