// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package walk

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/livingstaccato/cairn/internal/config"
)

func contentFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mk := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("docs/intro.md", "---\ntitle: Introduction\nsummary: Start here\ntags: [guide]\n---\n\nBody.\n")
	mk("docs/deep/nested.md", "---\ntitle: Nested\n---\n\nBody.\n")
	mk("docs/_index.md", "---\ntitle: Docs\n---\n")
	mk("docs/notes.txt", "not markdown\n")
	mk("docs/draft.md", "---\ntitle: Draft\ndraft: true\n---\n")
	mk("docs/plain.md", "no frontmatter at all\n")
	return root
}

func TestPagesListsMarkdown(t *testing.T) {
	root := contentFixture(t)
	got, warns, err := Pages(root, "docs", config.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Errorf("warnings: %v", warns)
	}
	byName := map[string]int{}
	for i, e := range got {
		byName[e.Name] = i
	}
	if _, ok := byName["notes"]; ok {
		t.Error("a .txt file is not content")
	}
	if _, ok := byName["draft"]; ok {
		t.Error("a draft must not be listed")
	}
	if _, ok := byName["_index"]; ok {
		t.Error("_index.md is the section's own page, not an entry in it")
	}

	intro, ok := byName["intro"]
	if !ok {
		t.Fatalf("intro missing from %v", names(got))
	}
	if got[intro].Title != "Introduction" || got[intro].Summary != "Start here" {
		t.Errorf("frontmatter not read: %+v", got[intro])
	}
	if got[intro].Kind != KindPage {
		t.Errorf("Kind = %q, want %q", got[intro].Kind, KindPage)
	}
	if got[intro].Path != "/docs/intro/" {
		t.Errorf("Path = %q, want the Hugo-style URL /docs/intro/", got[intro].Path)
	}
	if len(got[intro].Tags) != 1 {
		t.Errorf("Tags = %v", got[intro].Tags)
	}
	if deep, ok := byName["deep"]; !ok || !got[deep].IsDir {
		t.Errorf("deep/ should appear as a section directory: %v", names(got))
	}
	// A page with no frontmatter is still a page; its slug becomes its title.
	if plain, ok := byName["plain"]; !ok || got[plain].Title != "plain" {
		t.Errorf("plain.md missing or untitled: %v", names(got))
	}
}

func TestPagesMalformedFrontmatterWarns(t *testing.T) {
	root := contentFixture(t)
	if err := os.WriteFile(filepath.Join(root, "docs", "bad.md"),
		[]byte("---\ntitle: [unclosed\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, warns, err := Pages(root, "docs", config.Defaults())
	if err != nil {
		t.Fatalf("one bad page must warn, not fail the build: %v", err)
	}
	if len(warns) != 1 {
		t.Errorf("warnings = %v, want 1", warns)
	}
	for _, e := range got {
		if e.Name == "bad" {
			t.Error("a page whose frontmatter did not parse must be omitted")
		}
	}
}

func TestPagesMissingDirIsError(t *testing.T) {
	if _, _, err := Pages(t.TempDir(), "nope", config.Defaults()); err == nil {
		t.Fatal("expected an error for a missing content directory")
	}
}
