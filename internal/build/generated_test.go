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

// These cover the rule generated.go owns: cairndex's own output is not content, so
// neither the output directory nor a generated filename belongs in a listing or
// in the walk that produces one.

// nested builds a tree whose out: is a subdirectory of root:.
func nested(t *testing.T) (root, out string) {
	t.Helper()
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pool"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pool", "a.txt"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, filepath.Join(root, "site")
}

// The output directory used to be walked like any other, so every build indexed
// the last build's output one level deeper: site/, then site/site/, then
// site/site/site/. It never reached a fixed point, and on a mirror each level
// duplicates the whole index set.
//
// internal/watch already skips a nested output directory whole. The builder did
// not, so the watcher and the builder disagreed about the same tree — and the
// one that was wrong is the one that writes files.
func TestOutInsideRootIsNotIndexed(t *testing.T) {
	root, out := nested(t)

	first := run(t, hashing(), root, out)
	for i := range 3 {
		got := run(t, hashing(), root, out)
		if got.Dirs != first.Dirs {
			t.Fatalf("rebuild %d covered %d directories, first build covered %d: "+
				"the build is indexing its own output", i+2, got.Dirs, first.Dirs)
		}
		if len(got.Written) != len(first.Written) {
			t.Fatalf("rebuild %d wrote %d outputs, first build wrote %d",
				i+2, len(got.Written), len(first.Written))
		}
	}

	if _, err := os.Lstat(filepath.Join(out, "site")); err == nil {
		t.Error("the output directory was indexed into itself")
	}
}

// Excluded from the listing as well as from the walk. Leaving it listed would
// name a directory whose page is never written — a link that 404s, which is
// worse than not mentioning it.
func TestOutInsideRootIsNotListed(t *testing.T) {
	root, out := nested(t)
	run(t, hashing(), root, out)

	b, err := os.ReadFile(filepath.Join(out, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var l struct {
		Entries []struct{ Name string } `json:"entries"`
	}
	if err := json.Unmarshal(b, &l); err != nil {
		t.Fatal(err)
	}
	for _, e := range l.Entries {
		if e.Name == "site" {
			t.Errorf("the root listing names the output directory: %s", b)
		}
	}
	// The real content is still there.
	if !strings.Contains(string(b), "pool") {
		t.Errorf("the listing lost the tree's own directories: %s", b)
	}
}

// The mirror deployment is root and out being the same directory, which is
// supported and must stay so: there is no subtree to skip there, and the
// generated names are what keep it from feeding itself.
func TestOutEqualToRootStillBuilds(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first := run(t, hashing(), root, root)
	again := run(t, hashing(), root, root)
	if again.Dirs != first.Dirs || len(again.Written) != len(first.Written) {
		t.Errorf("a mirror rebuild changed shape: %d/%d then %d/%d",
			first.Dirs, len(first.Written), again.Dirs, len(again.Written))
	}
	if len(again.Changed) != 0 {
		t.Errorf("a mirror rebuild rewrote %v, want nothing", again.Changed)
	}
}

// outputs: [search] writes both a file (search.js) and a directory
// (emit.SearchPageDir) into the tree it indexes, and neither name was in
// GeneratedNames. In a mirror, the second build finds search/ sitting in
// root like any other directory, indexes it, and writes it a search/
// of its own — the same runaway nesting TestOutInsideRootIsNotIndexed
// covers for the top-level output directory, one rebuild deeper each time.
func TestSearchOutputIsNotIndexedInAMirror(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := conf(nil)
	outs := []string{config.OutputJSON, config.OutputSearch}
	c.Defaults = config.Override{Outputs: &outs}

	first := run(t, c, root, root)
	for i := range 3 {
		got := run(t, c, root, root)
		if got.Dirs != first.Dirs {
			t.Fatalf("rebuild %d covered %d directories, first build covered %d: "+
				"search's own output is being indexed", i+2, got.Dirs, first.Dirs)
		}
	}

	b, err := os.ReadFile(filepath.Join(root, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"search.js"`) {
		t.Errorf("the listing names search.js as content: %s", b)
	}
	if strings.Contains(string(b), `"name":"search"`) {
		t.Errorf("the listing names the search/ subdirectory as content: %s", b)
	}
}

// hugo mode writes its own page into the tree it indexes, so the same exclusion
// has to cover it.

// Hugo mode writes _index.md into the tree in the same arrangement, and must
// not list it either.
func TestRunInPlaceHugoExcludesItsPage(t *testing.T) {
	root := tree(t)
	c := conf(nil)
	c.Mode = config.ModeHugo
	outs := []string{config.OutputText}
	c.Defaults = config.Override{Outputs: &outs}
	run(t, c, root, root)
	run(t, c, root, root)

	b, err := os.ReadFile(filepath.Join(root, "bootstrap", "index.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "_index.md") {
		t.Errorf("listing includes the page cairndex generated: %q", b)
	}
}
