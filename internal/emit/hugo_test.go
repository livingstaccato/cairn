// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package emit

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/livingstaccato/cairndex/internal/model"
)

// frontmatter is the shape a Hugo template will read back out of .Params.
type parsedFM struct {
	Title    string `yaml:"title"`
	Layout   string `yaml:"layout"`
	Cairndex struct {
		Present  string `yaml:"present"`
		Path     string `yaml:"path"`
		Count    int    `yaml:"count"`
		AtRoot   bool   `yaml:"at_root"`
		BasePath string `yaml:"base_path"`
		PEP503   bool   `yaml:"pep503"`
	} `yaml:"cairndex"`
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
	if fm.Cairndex.Present != "styled" {
		t.Errorf("present = %q", fm.Cairndex.Present)
	}
	if fm.Cairndex.Count != 2 {
		t.Errorf("count = %d, want 2", fm.Cairndex.Count)
	}
	if fm.Title != "linux" {
		t.Errorf("title = %q, want the directory name", fm.Title)
	}
}

func TestHugoContentRootTitle(t *testing.T) {
	l := sample()
	l.Path = "/"
	b, err := HugoContent(HugoPage{Listing: l, Present: "bare", AtRoot: true})
	if err != nil {
		t.Fatal(err)
	}
	fm, _ := split(t, b)
	if fm.Title == "" || fm.Title == "/" || fm.Title == "." {
		t.Errorf("root title = %q, want a readable name", fm.Title)
	}
}

// A configured base_path prepends its prefix to every Listing.Path, root
// included, before HugoContent ever sees it — so the root's title has to come
// from AtRoot, not from matching the path string against "/".
func TestHugoContentRootTitleWithBasePath(t *testing.T) {
	l := sample()
	l.Path = "/mirror"
	b, err := HugoContent(HugoPage{Listing: l, Present: "bare", AtRoot: true, BasePath: "/mirror"})
	if err != nil {
		t.Fatal(err)
	}
	fm, _ := split(t, b)
	if fm.Title == "mirror" {
		t.Errorf("root title = %q, base_path leaked into the title", fm.Title)
	}
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
	if !fm.Cairndex.AtRoot {
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
	if fm.Cairndex.AtRoot {
		t.Errorf("at_root is set for a listing below the top:\n%s", b)
	}
}

// The breadcrumb's root anchor is site-absolute, so under base_path it has to be
// the top of the mirror rather than the top of the site.
//
// It was hardcoded to "/". The crumbs themselves were already right, because
// listing paths carry the prefix, but the anchor in front of them pointed above
// the tree cairndex published — and from file:// at the filesystem root. A template
// cannot work this out from the path alone, so it travels with it.
func TestHugoContentCarriesBasePath(t *testing.T) {
	b, err := HugoContent(HugoPage{Listing: sample(), BasePath: "/mirror"})
	if err != nil {
		t.Fatal(err)
	}
	fm, _ := split(t, b)
	if fm.Cairndex.BasePath != "/mirror" {
		t.Errorf("base_path did not reach the frontmatter:\n%s", b)
	}
}

// Absent when there is none, so a site published at the root does not carry a
// key that says nothing.
func TestHugoContentOmitsAnEmptyBasePath(t *testing.T) {
	b, err := HugoContent(HugoPage{Listing: sample()})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "base_path") {
		t.Errorf("an empty base_path was written out:\n%s", b)
	}
}

// PEP503 tells the template to render the PEP 503 simple-index page instead
// of the normal listing — a template cannot derive this from present:,
// since pep503 is requested through outputs:, an independent axis.
func TestHugoContentCarriesPEP503(t *testing.T) {
	b, err := HugoContent(HugoPage{Listing: sample(), PEP503: true})
	if err != nil {
		t.Fatal(err)
	}
	fm, _ := split(t, b)
	if !fm.Cairndex.PEP503 {
		t.Errorf("pep503 did not reach the frontmatter:\n%s", b)
	}
}

// A pep503 page gets its own layout, one with no {{ define "main" }} block,
// so Hugo renders it standalone instead of wrapping it in the theme's
// baseof.html — a page whose own doctype ends up nested inside the theme's
// otherwise. Reusing HugoLayout for both pages is what let that happen: the
// listing template's {{ if .Params.cairndex.pep503 }} branch decided which
// partial to call, but baseof wraps the whole layout either way.
func TestHugoContentUsesTheStandalonePEP503Layout(t *testing.T) {
	b, err := HugoContent(HugoPage{Listing: sample(), PEP503: true})
	if err != nil {
		t.Fatal(err)
	}
	fm, _ := split(t, b)
	if fm.Layout != HugoPEP503Layout {
		t.Errorf("layout = %q, want %q so Hugo skips baseof for this page", fm.Layout, HugoPEP503Layout)
	}
}

func TestHugoContentUsesTheOrdinaryLayoutWhenNotPEP503(t *testing.T) {
	b, err := HugoContent(HugoPage{Listing: sample()})
	if err != nil {
		t.Fatal(err)
	}
	fm, _ := split(t, b)
	if fm.Layout != HugoLayout {
		t.Errorf("layout = %q, want %q", fm.Layout, HugoLayout)
	}
}

func TestHugoContentOmitsPEP503WhenFalse(t *testing.T) {
	b, err := HugoContent(HugoPage{Listing: sample()})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "pep503") {
		t.Errorf("pep503: false was written out where omitting it says the same thing:\n%s", b)
	}
}

// HugoSearchContent is the frontmatter for the search page's own branch
// bundle, one level under the listing it searches — a real Hugo page,
// unlike search.html's plain-file sibling in mode: direct, because Hugo
// refuses to publish a raw .html bundle resource as a static file at all.
func TestHugoSearchContentUsesTheStandaloneLayout(t *testing.T) {
	b, err := HugoSearchContent("/bootstrap/")
	if err != nil {
		t.Fatal(err)
	}
	fm, _ := split(t, b)
	if fm.Layout != HugoSearchLayout {
		t.Errorf("layout = %q, want %q so Hugo skips baseof for this page", fm.Layout, HugoSearchLayout)
	}
	if fm.Title != "Search /bootstrap/" {
		t.Errorf("title = %q, want %q", fm.Title, "Search /bootstrap/")
	}
}
