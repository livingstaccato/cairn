// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for where the standalone search index lands, which is the whole of the
// decision: the emitter is a projection, the placement is the logic.

package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/livingstaccato/cairn/internal/config"
)

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

// cairn excludes its own output from the listings it writes. A search index
// that offers itself as a result is noise on every query.
//
// Root and out are the same directory here, which is both the arrangement the
// deployment guide describes and the only one that can catch this: with output
// written elsewhere cairn never walks over what it wrote, and the test passes
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
		t.Error("the search index lists cairn's own listing")
	}
}
