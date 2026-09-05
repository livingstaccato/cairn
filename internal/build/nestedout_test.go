// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

// OutRel is the one place that decides whether the output lies inside the tree.
// The builder and the watcher both ask it, so they cannot drift.
func TestOutRel(t *testing.T) {
	cases := []struct{ root, out, want string }{
		{"/srv/tree", "/srv/tree/site", "site"},
		{"/srv/tree", "/srv/tree/a/b", "a/b"},
		{"/srv/tree", "/srv/tree", ""},     // the mirror: nothing to skip
		{"/srv/tree", "/srv/site", ""},     // separate trees
		{"/srv/tree", "/srv", ""},          // out holds root
		{"/srv/tree", "/srv/tree-old", ""}, // a sibling sharing a prefix
	}
	for _, c := range cases {
		if got := OutRel(c.root, c.out); got != c.want {
			t.Errorf("OutRel(%q, %q) = %q, want %q", c.root, c.out, got, c.want)
		}
	}
}
