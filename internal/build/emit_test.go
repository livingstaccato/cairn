// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/livingstaccato/cairn/internal/config"
)

// recursing returns a config that writes the recursive listing beside the
// per-directory one.
func recursing() *config.Config {
	c := conf(nil)
	rec := true
	outs := []string{config.OutputJSON}
	c.Defaults = config.Override{Recursive: &rec, Outputs: &outs}
	return c
}

// treeEntries reads a recursive listing and returns its entries.
func treeEntries(t *testing.T, path string) []struct{ Name, Path string } {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var l struct {
		Entries []struct{ Name, Path string } `json:"entries"`
	}
	if err := json.Unmarshal(b, &l); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return l.Entries
}

// The recursive listing has to leave out exactly what the per-directory listing
// leaves out.
//
// The two are produced by different paths — collect runs the per-directory walk
// through dropGenerated, while emitTree reaches walk.Tree directly — so the
// exclusion had to be stated twice or stated once and shared. Stated only on the
// collect side, tree.json published the output directory and cairn's own
// generated files as though they were content, and the recursive search index
// built from that listing inherited it.
func TestTreeListingExcludesTheOutputDirectory(t *testing.T) {
	root, out := nested(t)

	// Twice: the output directory does not exist to be walked until the first
	// build has written it.
	run(t, recursing(), root, out)
	run(t, recursing(), root, out)

	for _, e := range treeEntries(t, filepath.Join(out, "tree.json")) {
		if e.Name == "site" {
			t.Errorf("the recursive listing names the output directory: %+v", e)
		}
		if strings.Contains(e.Path, "/site/") {
			t.Errorf("the recursive listing descended into the output directory: %+v", e)
		}
	}
}

// cairn's own generated filenames are dropped from the recursive listing for the
// same reason they are dropped from the per-directory one: in a mirror, root and
// out are one directory, so there is no subtree to skip and the names are the
// only thing keeping the build from listing what it just wrote.
func TestTreeListingExcludesGeneratedNames(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pool"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pool", "a.txt"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	run(t, recursing(), root, root)
	run(t, recursing(), root, root)

	for _, e := range treeEntries(t, filepath.Join(root, "tree.json")) {
		if e.Name == "index.json" || e.Name == "tree.json" {
			t.Errorf("the recursive listing names cairn's own output: %+v", e)
		}
	}
}

// The recursive listing is an output like any other, so a rebuild that changed
// nothing must not rewrite it. Listing the output directory made tree.json
// differ on every run, which is the churn Result.Changed exists to rule out.
func TestTreeListingReachesAFixedPoint(t *testing.T) {
	root, out := nested(t)

	run(t, recursing(), root, out)
	run(t, recursing(), root, out)
	third := run(t, recursing(), root, out)

	if len(third.Changed) != 0 {
		t.Errorf("a settled rebuild rewrote %v, want nothing", third.Changed)
	}
}
