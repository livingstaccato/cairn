// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for what a listing says about where it lives: base_path moves the whole
// tree under a prefix, and every path a listing publishes has to move with it.

package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/livingstaccato/cairndex/internal/config"
	"github.com/livingstaccato/cairndex/internal/model"
)

// TestBasePathMakesPathsSiteAbsolute checks the case where cairndex's root is not
// the web root. Entry.Path is rooted at cairndex's root, so a tree indexed from
// static/_odds and served at /_odds yields /mockups/x.html for a file the site
// serves at /_odds/mockups/x.html: every link a 404, with internally consistent
// JSON and nothing to signal it.
func TestBasePathMakesPathsSiteAbsolute(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	c.BasePath = "/_odds"
	run(t, c, root, out)

	var l model.Listing
	b, err := os.ReadFile(filepath.Join(out, "bootstrap", "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &l); err != nil {
		t.Fatal(err)
	}
	if l.Path != "/_odds/bootstrap" {
		t.Errorf("listing path = %q, want it under the base path", l.Path)
	}
	for _, e := range l.Entries {
		if !strings.HasPrefix(e.Path, "/_odds/") {
			t.Errorf("entry %s path = %q, want it under the base path", e.Name, e.Path)
		}
	}
}

// TotalSize is the bytes a visitor would download taking every file a
// listing shows, not counting a directory's own zero-value Size, so a
// mixed directory's total reflects only what is actually downloadable
// from it.
func TestListingTotalSizeSumsFilesNotDirs(t *testing.T) {
	root, out := tree(t), t.TempDir()
	run(t, conf(nil), root, out)

	b, err := os.ReadFile(filepath.Join(out, "bootstrap", "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var l model.Listing
	if err := json.Unmarshal(b, &l); err != nil {
		t.Fatal(err)
	}
	var want int64
	for _, e := range l.Entries {
		if !e.IsDir {
			want += e.Size
		}
	}
	if want == 0 {
		t.Fatal("fixture has no files; this test proves nothing")
	}
	if l.TotalSize != want {
		t.Errorf("TotalSize = %d, want %d (sum of file sizes only)", l.TotalSize, want)
	}
}

// tree.json's own Listing is built from the same r.listing() as a normal
// directory, over its already-flattened set of descendants, so TotalSize
// there is a true recursive rollup with no separate code path to keep in
// sync.
func TestTreeJSONTotalSizeIsRecursive(t *testing.T) {
	root, out := tree(t), t.TempDir()
	recursive := true
	c := conf([]config.Rule{{Match: "bootstrap", Override: config.Override{Recursive: &recursive}}})
	run(t, c, root, out)

	b, err := os.ReadFile(filepath.Join(out, "bootstrap", "tree.json"))
	if err != nil {
		t.Fatal(err)
	}
	var l model.Listing
	if err := json.Unmarshal(b, &l); err != nil {
		t.Fatal(err)
	}
	dirTotal, err := os.ReadFile(filepath.Join(out, "bootstrap", "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var dir model.Listing
	if err := json.Unmarshal(dirTotal, &dir); err != nil {
		t.Fatal(err)
	}
	if l.TotalSize <= dir.TotalSize {
		t.Errorf("tree.json TotalSize = %d, want more than index.json's own %d: "+
			"bootstrap/linux/apt.list is only in the recursive listing",
			l.TotalSize, dir.TotalSize)
	}
}

func TestBasePathDefaultsToUnchanged(t *testing.T) {
	root, out := tree(t), t.TempDir()
	run(t, conf(nil), root, out)

	b, err := os.ReadFile(filepath.Join(out, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var l model.Listing
	if err := json.Unmarshal(b, &l); err != nil {
		t.Fatal(err)
	}
	if l.Path != "/" {
		t.Errorf("root listing path = %q, want / when no base_path is set", l.Path)
	}
}
