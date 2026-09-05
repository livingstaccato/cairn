// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for rebuilding one subtree: what has to be rebuilt with it, and what
// must survive untouched.

package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/model"
	"github.com/livingstaccato/cairn/internal/obs"
)

func readListing(t *testing.T, path string) model.Listing {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no listing at %s: %v", path, err)
	}
	var l model.Listing
	if err := json.Unmarshal(b, &l); err != nil {
		t.Fatal(err)
	}
	return l
}

// The whole point: a scoped rebuild must not delete the listings it did not
// write. The unscoped Prune deletes everything the previous run owned and this
// one did not rewrite, which on a watch event is most of the tree.
func TestScopedRebuildKeepsTheRestOfTheTree(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	if _, err := Run(c, root, out, obs.Discard()); err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(out, "docs", "index.json")
	if _, err := os.Stat(elsewhere); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if _, err := RunScoped(c, root, out, obs.Discard(), "bootstrap"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(elsewhere); err != nil {
		t.Errorf("a scoped rebuild pruned an unrelated listing: %v", err)
	}
}

// A parent's entry for a directory carries that directory's child count and
// modification time, so a file added below has to reach the listings above it.
func TestScopedRebuildRefreshesAncestors(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	if _, err := Run(c, root, out, obs.Discard()); err != nil {
		t.Fatal(err)
	}

	before := readListing(t, filepath.Join(out, "index.json"))
	var wantCount int
	for _, e := range before.Entries {
		if e.Name == "bootstrap" {
			wantCount = e.Count
		}
	}

	if err := os.WriteFile(filepath.Join(root, "bootstrap", "extra.sh"), []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RunScoped(c, root, out, obs.Discard(), "bootstrap"); err != nil {
		t.Fatal(err)
	}

	after := readListing(t, filepath.Join(out, "index.json"))
	for _, e := range after.Entries {
		if e.Name == "bootstrap" && e.Count == wantCount {
			t.Errorf("root listing still reports %d children of bootstrap; the ancestor was not refreshed", e.Count)
		}
	}
}

// Refreshing an ancestor must not rebuild its other children: that is the
// saving a scoped rebuild exists for.
func TestScopedRebuildDoesNotDescendIntoSiblings(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	if _, err := Run(c, root, out, obs.Discard()); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(out, "docs", "index.json")
	stamp := time.Now().Add(-time.Hour)
	if err := os.Chtimes(sibling, stamp, stamp); err != nil {
		t.Fatal(err)
	}

	if _, err := RunScoped(c, root, out, obs.Discard(), "bootstrap"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(sibling)
	if err != nil {
		t.Fatal(err)
	}
	if fi.ModTime().After(stamp.Add(time.Minute)) {
		t.Error("a scoped rebuild rewrote a sibling subtree it had no reason to touch")
	}
}

// A change below a recursive listing invalidates that listing, so the rebuild
// has to start at the directory that owns it, not at the file's own directory.
func TestScopeClimbsToTheRecursiveOwner(t *testing.T) {
	root := tree(t)
	yes := true
	c := conf([]config.Rule{{
		Match:    "bootstrap/**",
		Override: config.Override{Recursive: &yes},
	}})
	if got := Scope(c, root, "bootstrap/linux", obs.Discard()); got != "bootstrap" {
		t.Errorf("scope = %q, want bootstrap: the recursive listing above it is stale", got)
	}
}

// With nothing recursive above it, the changed directory is the whole scope.
func TestScopeStopsAtTheChangedDirectory(t *testing.T) {
	root := tree(t)
	if got := Scope(conf(nil), root, "bootstrap/linux", obs.Discard()); got != "bootstrap/linux" {
		t.Errorf("scope = %q, want the changed directory", got)
	}
}

func TestRelDirOfRejectsPathsOutsideTheRoot(t *testing.T) {
	root := t.TempDir()
	if _, ok := RelDirOf(root, filepath.Join(root, "a", "b.txt")); !ok {
		t.Error("a path inside the root was rejected")
	}
	if _, ok := RelDirOf(root, filepath.Join(filepath.Dir(root), "elsewhere", "b.txt")); ok {
		t.Error("a path outside the root was accepted")
	}
}

// The manifest is how the next run tells its own output from somebody else's.
// A scoped rebuild that recorded only what it wrote would disown the rest of
// the tree, and the next run would refuse all of it as a conflict.
func TestScopedRebuildKeepsOwnershipOfTheRestOfTheTree(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	if _, err := Run(c, root, out, obs.Discard()); err != nil {
		t.Fatal(err)
	}
	if _, err := RunScoped(c, root, out, obs.Discard(), "bootstrap"); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(filepath.Join(out, ".cairn-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var owned []string
	if err := json.Unmarshal(b, &owned); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, p := range owned {
		if strings.Contains(p, "docs/index.json") {
			found = true
		}
	}
	if !found {
		t.Errorf("manifest no longer claims the unscoped output: %v", owned)
	}

	// The proof it matters: a following full run must not refuse those files.
	if _, err := Run(c, root, out, obs.Discard()); err != nil {
		t.Errorf("a run after a scoped rebuild refused its own earlier output: %v", err)
	}
}

// A scope of "docs" must not claim "docs-old". A prefix test on the raw string
// would prune a sibling directory's entire output on the first watch event.
func TestScopedRebuildDoesNotClaimSiblingsBySharedPrefix(t *testing.T) {
	root, out := tree(t), t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs-old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs-old", "note.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := conf(nil)
	if _, err := Run(c, root, out, obs.Discard()); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(out, "docs-old", "index.json")
	if _, err := os.Stat(sibling); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Remove the file so a rebuild of "docs" would prune anything it claims.
	if err := os.Remove(filepath.Join(root, "docs-old", "note.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := RunScoped(c, root, out, obs.Discard(), "docs"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Errorf("rebuilding docs pruned docs-old: %v", err)
	}
}

// A scoped rebuild of the root is a full rebuild, and the ancestor walk has
// nothing above it to refresh.
func TestScopedRebuildOfTheRootCoversEverything(t *testing.T) {
	root, out := tree(t), t.TempDir()
	if _, err := RunScoped(conf(nil), root, out, obs.Discard(), "."); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		filepath.Join(out, "index.json"),
		filepath.Join(out, "bootstrap", "index.json"),
		filepath.Join(out, "bootstrap", "linux", "index.json"),
		filepath.Join(out, "docs", "index.json"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("root scope did not write %s: %v", p, err)
		}
	}
	if got := ancestors("."); got != nil {
		t.Errorf("ancestors(\".\") = %v, want none", got)
	}
}

// An empty scope means the whole tree, so a caller with nothing to say does
// not silently rebuild only the root directory's own listing.
func TestScopedRebuildTreatsEmptyScopeAsTheRoot(t *testing.T) {
	root, out := tree(t), t.TempDir()
	if _, err := RunScoped(conf(nil), root, out, obs.Discard(), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "bootstrap", "linux", "index.json")); err != nil {
		t.Errorf("empty scope did not rebuild the tree: %v", err)
	}
}

// A build that fails partway must still claim what it wrote, or the next run
// refuses those files as somebody else's.
func TestScopedRebuildRecordsPartialOutputOnFailure(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	c.Defaults = config.Override{Outputs: &[]string{"pdf"}}
	if _, err := RunScoped(c, root, out, obs.Discard(), "bootstrap"); err == nil {
		t.Fatal("an unknown output format must fail the rebuild")
	}
	if _, err := os.Stat(filepath.Join(out, ".cairn-manifest.json")); err != nil {
		t.Errorf("a failed rebuild recorded nothing: %v", err)
	}
}

// Every directory between the change and the root, nearest first.
func TestAncestorsWalkToTheRoot(t *testing.T) {
	got := ancestors("a/b/c")
	want := []string{"a/b", "a", "."}
	if len(got) != len(want) {
		t.Fatalf("ancestors = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ancestors[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// A watcher hands over the file that changed, not its directory.
func TestRelDirOfNamesTheContainingDirectory(t *testing.T) {
	root := t.TempDir()
	got, ok := RelDirOf(root, filepath.Join(root, "bootstrap", "linux", "apt.list"))
	if !ok || got != "bootstrap/linux" {
		t.Errorf("RelDirOf = %q, %v; want bootstrap/linux", got, ok)
	}
	// A file directly in the root belongs to ".", not to "".
	if got, ok := RelDirOf(root, filepath.Join(root, "top.txt")); !ok || got != "." {
		t.Errorf("RelDirOf at the root = %q, %v; want .", got, ok)
	}
}

// A deleted file leaves its digest and its directory's listing behind. Pruning
// is confined to the scope, but inside the scope it still has to happen.
func TestScopedRebuildPrunesInsideItsScope(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	if _, err := Run(c, root, out, obs.Discard()); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(out, "bootstrap", "linux", "index.json")
	if _, err := os.Stat(stale); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := os.RemoveAll(filepath.Join(root, "bootstrap", "linux")); err != nil {
		t.Fatal(err)
	}
	res, err := RunScoped(c, root, out, obs.Discard(), "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("the listing of a removed directory survived: %v", err)
	}
	if res.Pruned == 0 {
		t.Error("the result reported nothing pruned")
	}
}

// A scope narrower than the recursive listing above it still has to leave that
// listing correct: refreshing an ancestor regenerates its recursive view, not
// just its own directory's index.
func TestScopedRebuildRegeneratesARecursiveAncestor(t *testing.T) {
	root, out := tree(t), t.TempDir()
	yes := true
	c := conf([]config.Rule{{
		Match:    "bootstrap",
		Override: config.Override{Recursive: &yes},
	}})
	if _, err := Run(c, root, out, obs.Discard()); err != nil {
		t.Fatal(err)
	}
	before := readListing(t, filepath.Join(out, "bootstrap", "tree.json"))

	if err := os.WriteFile(filepath.Join(root, "bootstrap", "linux", "new.list"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Deliberately narrower than Scope() would choose, which is the case this
	// guards: the recursive listing above must still be brought up to date.
	if _, err := RunScoped(c, root, out, obs.Discard(), "bootstrap/linux"); err != nil {
		t.Fatal(err)
	}

	after := readListing(t, filepath.Join(out, "bootstrap", "tree.json"))
	if len(after.Entries) <= len(before.Entries) {
		t.Errorf("recursive listing has %d entries, was %d: the ancestor's tree was not regenerated",
			len(after.Entries), len(before.Entries))
	}
}
