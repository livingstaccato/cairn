// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/livingstaccato/cairndex/internal/config"
)

// Where the standalone search index lands, which is the whole of the decision
// emitSearch makes: the emitter in internal/emit is a projection, the placement
// is the logic.

func readSearch(t *testing.T, path string) []map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no search index at %s: %v", path, err)
	}
	var records []map[string]any
	if err := json.Unmarshal(b, &records); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return records
}

func names(records []map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, r := range records {
		out[r["name"].(string)] = true
	}
	return out
}

// Without recursion the index describes the directory it sits in, the way
// index.json does.
func TestSearchIndexScopedToItsDirectory(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	c.Defaults = config.Override{Outputs: &[]string{config.OutputJSON, config.OutputSearch}}
	run(t, c, root, out)

	got := names(readSearch(t, filepath.Join(out, "bootstrap", "search-index.json")))
	if !got["bootstrap.sh"] || !got["linux"] {
		t.Errorf("bootstrap index = %v, want its own children", got)
	}
	if got["apt.list"] {
		t.Error("bootstrap index reached into linux/ without recursion")
	}
}

// With recursion the subtree is what is worth searching: an index covering one
// directory of a deep tree finds almost nothing.
func TestSearchIndexCoversTheSubtree(t *testing.T) {
	root, out := tree(t), t.TempDir()
	yes := true
	c := conf([]config.Rule{{
		Match: "bootstrap/**",
		Override: config.Override{
			Recursive: &yes,
			Outputs:   &[]string{config.OutputJSON, config.OutputSearch},
		},
	}})
	run(t, c, root, out)

	got := names(readSearch(t, filepath.Join(out, "bootstrap", "search-index.json")))
	if !got["apt.list"] {
		t.Errorf("recursive index = %v, want the nested file in it", got)
	}
}

// outputs: [search] used to leave a visitor with a JSON file and nothing to
// search it with — a site author had to wire up Fuse.js or similar
// themselves. search.js lands beside it, and the search box lands one level
// under it (bootstrap/search/index.html): Hugo refuses to publish a raw
// .html bundle resource as a plain file at all, so the box has to be a real
// page in every mode, not a sibling mode: direct alone could get away with.
func TestSearchOutputWritesAWorkingSearchPage(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	c.Defaults = config.Override{Outputs: &[]string{config.OutputJSON, config.OutputSearch}}
	run(t, c, root, out)

	page, err := os.ReadFile(filepath.Join(out, "bootstrap", "search", "index.html"))
	if err != nil {
		t.Fatalf("bootstrap/search/index.html was not written: %v", err)
	}
	if !strings.Contains(string(page), "search-index.json") {
		t.Errorf("the search page does not reference search-index.json:\n%s", page)
	}
	script, err := os.ReadFile(filepath.Join(out, "bootstrap", "search.js"))
	if err != nil {
		t.Fatalf("search.js was not written: %v", err)
	}
	if !strings.Contains(string(script), "rankResults") {
		t.Errorf("search.js does not look like the real script:\n%s", script)
	}
}

// mode: hugo gets the same search/ branch bundle, with its own standalone
// layout rather than the listing's — the layout dispatch pep503 already
// established, reused here for the same reason.
func TestSearchOutputInHugoModeWritesAStandaloneSearchPage(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	c.Defaults = config.Override{Outputs: &[]string{config.OutputJSON, config.OutputSearch}}
	c.Mode = config.ModeHugo
	run(t, c, root, out)

	b, err := os.ReadFile(filepath.Join(out, "bootstrap", "search", "_index.md"))
	if err != nil {
		t.Fatalf("bootstrap/search/_index.md was not written: %v", err)
	}
	if !strings.Contains(string(b), "layout: cairndex-search") {
		t.Errorf("search page frontmatter does not use the standalone layout:\n%s", b)
	}
}

// machineFormats fed the format switcher for json/csv/txt but never for
// search, so a directory got a working search/ page with nothing on its own
// listing pointing at it — reachable only by knowing the URL in advance.
func TestSearchAppearsInTheListingsOwnFormats(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	c.Defaults = config.Override{Outputs: &[]string{config.OutputHTML, config.OutputJSON, config.OutputSearch}}
	c.Mode = config.ModeHugo
	run(t, c, root, out)

	b, err := os.ReadFile(filepath.Join(out, "bootstrap", "_index.md"))
	if err != nil {
		t.Fatalf("bootstrap/_index.md was not written: %v", err)
	}
	if !strings.Contains(string(b), "- search") {
		t.Errorf("the listing's own frontmatter formats do not name search, "+
			"so nothing on the page links to search/:\n%s", b)
	}
}

// The filename is fixed, so both listings would write it under recursion and
// the second would overwrite the first with less. The subtree must win.
func TestSearchIndexWrittenOncePerDirectory(t *testing.T) {
	root, out := tree(t), t.TempDir()
	yes := true
	c := conf([]config.Rule{{
		Match: "bootstrap/**",
		Override: config.Override{
			Recursive: &yes,
			Outputs:   &[]string{config.OutputJSON, config.OutputSearch},
		},
	}})
	run(t, c, root, out)

	// tree.json exists, so both listings were emitted; the search index must
	// still hold the recursive one rather than the directory-only one.
	if _, err := os.Stat(filepath.Join(out, "bootstrap", "tree.json")); err != nil {
		t.Fatalf("expected a recursive listing to have been written: %v", err)
	}
	if got := names(readSearch(t, filepath.Join(out, "bootstrap", "search-index.json"))); !got["apt.list"] {
		t.Errorf("index = %v, want the subtree; the directory-only pass overwrote it", got)
	}
}

// cairndex excludes its own output from the listings it writes. A search index
// that offers itself as a result is noise on every query.
//
// Root and out are the same directory here, which is both the arrangement the
// deployment guide describes and the only one that can catch this: with output
// written elsewhere cairndex never walks over what it wrote, and the test passes
// whether or not the exclusion exists.
func TestSearchIndexDoesNotListItself(t *testing.T) {
	root := tree(t)
	c := conf(nil)
	c.Defaults = config.Override{Outputs: &[]string{config.OutputJSON, config.OutputSearch}}
	run(t, c, root, root)
	// Twice: the first run leaves the file on disk for the second to find.
	run(t, c, root, root)

	got := names(readSearch(t, filepath.Join(root, "search-index.json")))
	if got["search-index.json"] {
		t.Error("the search index lists itself")
	}
	if got["index.json"] {
		t.Error("the search index lists cairndex's own listing")
	}
}
