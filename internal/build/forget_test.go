// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/hash"
	"github.com/livingstaccato/cairn/internal/obs"
)

// hashing returns a config that digests every file, which is what puts records
// in the cache in the first place.
func hashing() *config.Config {
	c := conf(nil)
	sha := config.ChecksumSHA256
	outs := []string{config.OutputJSON, config.OutputSums}
	c.Defaults = config.Override{Checksum: &sha, Outputs: &outs}
	return c
}

// cached reports the paths the saved cache holds.
func cached(t *testing.T, out string) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, hash.CacheFile))
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("parse cache: %v", err)
	}
	got := make(map[string]bool, len(m))
	for k := range m {
		got[k] = true
	}
	return got
}

// TestARebuildForgetsAFileThatIsGone is the whole point: nothing ever removed a
// record, so a tree that churns accumulated one entry per file that had ever
// been in it, and a long-lived mirror carried a cache of hundreds of megabytes
// describing files that no longer exist.
func TestARebuildForgetsAFileThatIsGone(t *testing.T) {
	root, out := tree(t), t.TempDir()
	gone := filepath.Join(root, "bootstrap", "linux", "apt.list")

	run(t, hashing(), root, out)
	if !cached(t, out)[gone] {
		t.Fatalf("the first build recorded no digest for %s", gone)
	}

	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	res := run(t, hashing(), root, out)

	if res.Forgot != 1 {
		t.Errorf("Forgot = %d, want 1", res.Forgot)
	}
	if cached(t, out)[gone] {
		t.Error("the cache still holds a digest for a file that was removed")
	}
	// The files that are still there keep theirs, or the sweep has merely
	// traded unbounded growth for re-hashing the tree on every build.
	if kept := filepath.Join(root, "docs", "intro.md"); !cached(t, out)[kept] {
		t.Errorf("the sweep dropped %s, which is still in the tree", kept)
	}
}

// TestAScopedRebuildForgetsOnlyInsideItsScope is the property that makes the
// sweep safe under cairn watch. A watch event rebuilds one subtree and consults
// only that subtree, so an unscoped sweep would discard the digests for the
// whole rest of the mirror on the first change and re-hash it on the next full
// build.
func TestAScopedRebuildForgetsOnlyInsideItsScope(t *testing.T) {
	root, out := tree(t), t.TempDir()
	elsewhere := filepath.Join(root, "bootstrap", "linux", "apt.list")
	inScope := filepath.Join(root, "docs", "intro.md")

	run(t, hashing(), root, out)
	if err := os.Remove(inScope); err != nil {
		t.Fatal(err)
	}

	res, err := RunScoped(hashing(), root, out, obs.Discard(), "docs")
	if err != nil {
		t.Fatalf("RunScoped: %v", err)
	}

	if res.Forgot != 1 {
		t.Errorf("Forgot = %d, want 1", res.Forgot)
	}
	if cached(t, out)[inScope] {
		t.Error("kept a digest for a file removed from the rebuilt scope")
	}
	if !cached(t, out)[elsewhere] {
		t.Error("a scoped rebuild discarded a digest from outside its scope")
	}
}

// TestADryRunReportsWhatItWouldForgetAndForgetsNothing: --dry-run guards the
// output tree, and the cache is a file under it.
//
// The tree is changed in both directions on purpose. A record that should go
// catches a dry run that swept and saved; a record that should arrive catches
// one that saved at all, which a sweep alone could not — the cache marshals a
// map, so a rewrite of unchanged entries is byte-identical and invisible.
func TestADryRunReportsWhatItWouldForgetAndForgetsNothing(t *testing.T) {
	root, out := tree(t), t.TempDir()
	gone := filepath.Join(root, "bootstrap", "linux", "apt.list")
	added := filepath.Join(root, "docs", "new.md")

	run(t, hashing(), root, out)
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(added, []byte("# New\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := RunDry(hashing(), root, out, obs.Discard())
	if err != nil {
		t.Fatalf("RunDry: %v", err)
	}
	if res.Forgot != 1 {
		t.Errorf("Forgot = %d, want 1", res.Forgot)
	}
	if !cached(t, out)[gone] {
		t.Error("a dry run dropped a cache record and saved the result")
	}
	if cached(t, out)[added] {
		t.Error("a dry run wrote the cache")
	}
}

// TestAFailedBuildForgetsNothing: a build that died partway never reached the
// rest of its scope, so "the run did not consult this" says nothing about
// whether the file is still there. Sweeping then would throw away digests that
// are perfectly good and re-hash a mirror that may be terabytes.
func TestAFailedBuildForgetsNothing(t *testing.T) {
	root, out := tree(t), t.TempDir()
	run(t, hashing(), root, out)

	// A file cairn does not own, standing where a later directory's listing
	// goes: on_conflict: error stops the build there.
	if err := os.MkdirAll(filepath.Join(root, "docs", "guides"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "guides", "a.md"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(out, "docs", "guides")
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "index.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "bootstrap", "linux", "apt.list")); err != nil {
		t.Fatal(err)
	}

	res, err := Run(hashing(), root, out, obs.Discard())
	if err == nil {
		t.Fatal("expected the conflicting file to fail the build")
	}
	if res.Forgot != 0 {
		t.Errorf("Forgot = %d after a failed build, want 0", res.Forgot)
	}
}

// TestADryRunLeavesTheCacheAbleToAnswerAgain pins forget's dry branch.
//
// Nothing saves the cache on a dry run, so sweeping instead of counting leaves
// no trace in the output tree — which is exactly why it needs its own test. A
// Cache swept in memory has lost the records, and any caller that goes on to
// use or save it writes the loss out.
func TestADryRunLeavesTheCacheAbleToAnswerAgain(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "f.bin")
	if err := os.WriteFile(f, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(f)
	if err != nil {
		t.Fatal(err)
	}
	cp := filepath.Join(t.TempDir(), hash.CacheFile)
	seeded := hash.NewCache(cp)
	if _, err := seeded.Sum(f, fi.Size(), fi.ModTime().Unix()); err != nil {
		t.Fatal(err)
	}
	if err := seeded.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}

	r := &runner{cache: hash.NewCache(cp), result: &Result{}, root: dir, dry: true}
	r.forget(dir)
	if r.result.Forgot != 1 {
		t.Fatalf("Forgot = %d, want 1", r.result.Forgot)
	}
	r.forget(dir)
	if r.result.Forgot != 1 {
		t.Errorf("second Forgot = %d, want 1; the dry run swept the cache in memory", r.result.Forgot)
	}
}
