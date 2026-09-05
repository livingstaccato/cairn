// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package hash

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seed writes a cache file holding exactly these records and returns its path,
// so a test can start from a cache a previous run left behind without hashing
// anything.
func seed(t *testing.T, dir string, entries map[string]record) string {
	t.Helper()
	b, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, CacheFile)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// rec is a plausible record for a file of that size and mtime. The digest is
// never verified by anything a sweep does; only size and mtime decide a hit.
func rec(size, mod int64) record {
	return record{Size: size, ModUnix: mod, Sum: strings.Repeat("a", 64)}
}

func keys(c *Cache) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.entries))
	for k := range c.entries {
		out = append(out, k)
	}
	return out
}

func has(c *Cache, path string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.entries[path]
	return ok
}

func TestSweepDropsWhatTheRunDidNotConsult(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	kept := filepath.Join(root, "still-here.bin")
	gone := filepath.Join(root, "deleted.bin")
	c := NewCache(seed(t, dir, map[string]record{kept: rec(1, 10), gone: rec(2, 20)}))

	// The run consults one of the two. The other names a file that is no longer
	// in the tree, which is the whole reason the cache grows forever.
	if _, err := c.Sum(kept, 1, 10); err != nil {
		t.Fatal(err)
	}

	if n := c.Sweep(root); n != 1 {
		t.Errorf("Sweep dropped %d, want 1", n)
	}
	if !has(c, kept) {
		t.Error("swept an entry this run consulted")
	}
	if has(c, gone) {
		t.Error("kept an entry for a file the run never looked at")
	}
}

func TestSweepKeepsWhatIsOutsideThePrefix(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	outside := filepath.Join(root, "other", "x.bin")
	inside := filepath.Join(root, "scope", "y.bin")
	c := NewCache(seed(t, dir, map[string]record{outside: rec(1, 10), inside: rec(2, 20)}))

	// A scoped rebuild consults only its own subtree. Sweeping the whole cache
	// after one would throw away every digest in the rest of the mirror and
	// re-hash it on the next full build.
	if n := c.Sweep(filepath.Join(root, "scope")); n != 1 {
		t.Errorf("Sweep dropped %d, want 1", n)
	}
	if !has(c, outside) {
		t.Errorf("swept an entry outside the scope; left %v", keys(c))
	}
}

func TestSweepDoesNotClaimASiblingSharingANamePrefix(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	sibling := filepath.Join(root, "docs-old", "x.bin")
	c := NewCache(seed(t, dir, map[string]record{sibling: rec(1, 10)}))

	// A raw string prefix would match "docs-old" against a scope of "docs" and
	// discard a sibling directory's digests entirely.
	if n := c.Sweep(filepath.Join(root, "docs")); n != 0 {
		t.Errorf("Sweep dropped %d, want 0", n)
	}
	if !has(c, sibling) {
		t.Error("swept a sibling whose name merely starts with the scope")
	}
}

func TestABatchHitCountsAsConsulted(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	p := filepath.Join(root, "f.bin")
	c := NewCache(seed(t, dir, map[string]record{p: rec(3, 30)}))

	// SumAll is the path a real build takes; Sum is not. A sweep that only
	// noticed Sum's hits would empty the cache on every build.
	res := c.SumAll([]Job{{Path: p, Size: 3, ModTime: 30}})
	if res[0].Err != nil {
		t.Fatal(res[0].Err)
	}
	if n := c.Sweep(root); n != 0 {
		t.Errorf("Sweep dropped %d, want 0", n)
	}
}

func TestSweepKeepsWhatThisRunStored(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	if err := os.WriteFile(p, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}

	c := NewCache(filepath.Join(dir, CacheFile))
	if _, err := c.Sum(p, fi.Size(), fi.ModTime().Unix()); err != nil {
		t.Fatal(err)
	}
	if n := c.Sweep(dir); n != 0 {
		t.Errorf("Sweep dropped %d freshly computed digests, want 0", n)
	}
}

func TestSweepAloneMakesTheCacheWorthSaving(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	gone := filepath.Join(root, "deleted.bin")
	p := seed(t, dir, map[string]record{gone: rec(1, 10)})

	// Save short-circuits on a clean cache. A sweep that did not mark it dirty
	// would drop the entry in memory and leave the file on disk unchanged, so
	// the next run would load it straight back.
	c := NewCache(p)
	if n := c.Sweep(root); n != 1 {
		t.Fatalf("Sweep dropped %d, want 1", n)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if has(NewCache(p), gone) {
		t.Error("the swept entry came back from disk")
	}
}

func TestStaleCountsWithoutDropping(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	c := NewCache(seed(t, dir, map[string]record{
		filepath.Join(root, "a.bin"): rec(1, 10),
		filepath.Join(root, "b.bin"): rec(2, 20),
	}))

	// What --dry-run reports. Asking twice has to give the same answer, which
	// it cannot if asking removed anything.
	if n := c.Stale(root); n != 2 {
		t.Errorf("Stale = %d, want 2", n)
	}
	if n := c.Stale(root); n != 2 {
		t.Errorf("second Stale = %d, want 2; counting must not drop", n)
	}
	if got := len(keys(c)); got != 2 {
		t.Errorf("cache holds %d entries after Stale, want 2", got)
	}
}

func TestSweepOfARelativeRootTakesEverything(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(seed(t, dir, map[string]record{"tree/a.bin": rec(1, 10)}))

	// A config whose root: is relative produces relative keys, and the prefix
	// for a full build is then ".". Nothing is under "./" by string, so a plain
	// prefix test would sweep nothing and the cache would still grow forever.
	if n := c.Sweep("."); n != 1 {
		t.Errorf("Sweep(\".\") dropped %d, want 1", n)
	}
}
