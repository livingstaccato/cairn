// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for deciding which directories a watcher registers.

package watch

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/livingstaccato/cairndex/internal/config"
	"github.com/livingstaccato/cairndex/internal/obs"
)

// rels turns a plan's absolute directories into paths relative to root, for an
// assertion that reads.
func rels(t *testing.T, root string, p Plan) []string {
	t.Helper()
	out := make([]string, 0, len(p.Dirs))
	for _, d := range p.Dirs {
		rel, err := filepath.Rel(root, d)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, filepath.ToSlash(rel))
	}
	slices.Sort(out)
	return out
}

func TestEnumerateListsEveryIndexedDirectory(t *testing.T) {
	root, out := tree(t), t.TempDir()
	p := Enumerate(conf(nil), root, out, obs.Discard())
	want := []string{".", "bootstrap", "bootstrap/linux", "docs"}
	if got := rels(t, root, p); !slices.Equal(got, want) {
		t.Errorf("Dirs = %v, want %v", got, want)
	}
	// bootstrap.sh, apt.list, intro.md. Counted because kqueue opens a
	// descriptor for each, and the budget is decided on this number.
	if p.Files != 3 {
		t.Errorf("Files = %d, want 3", p.Files)
	}
}

// A hidden directory is most of the descriptors in a working tree and none of
// the output. Watching it would rebuild the site on every object git writes.
func TestEnumerateSkipsHiddenDirectories(t *testing.T) {
	root, out := tree(t), t.TempDir()
	write(t, root, ".git/objects/ab/cdef", "x")
	write(t, root, ".DS_Store", "x")

	p := Enumerate(conf(nil), root, out, obs.Discard())
	for _, d := range rels(t, root, p) {
		if d == ".git" || d == ".git/objects" || d == ".git/objects/ab" {
			t.Errorf("watching %s, which the build hides", d)
		}
	}
	if p.Files != 3 {
		t.Errorf("Files = %d, want 3; a hidden file was counted", p.Files)
	}
}

func TestEnumerateSkipsTheOutputDirectory(t *testing.T) {
	root := tree(t)
	out := filepath.Join(root, "_index")
	write(t, root, "_index/docs/index.json", "{}")
	// A sibling sharing the prefix stays watched: the skip is by path segment,
	// not by string prefix.
	write(t, root, "_index-notes/todo.md", "x")

	p := Enumerate(conf(nil), root, out, obs.Discard())
	got := rels(t, root, p)
	if slices.Contains(got, "_index") || slices.Contains(got, "_index/docs") {
		t.Errorf("watching the output directory: %v", got)
	}
	if !slices.Contains(got, "_index-notes") {
		t.Errorf("stopped watching a sibling of the output directory: %v", got)
	}
}

// EnumerateUnder resolves rules against the watched root, not against the
// subtree. A directory that appears while the watcher is running has to get the
// settings its path earns, the same as one that was there at startup.
func TestEnumerateUnderResolvesAgainstTheRoot(t *testing.T) {
	root, out := tree(t), t.TempDir()
	hide := []string{"**/.*", "**/*.list"}
	c := conf([]config.Rule{{Match: "bootstrap/linux", Override: config.Override{Hide: &hide}}})

	p := EnumerateUnder(c, root, out, "bootstrap/linux", obs.Discard())
	if p.Files != 0 {
		t.Errorf("Files = %d, want 0; the subtree's own rule was not applied", p.Files)
	}
}

// follow_symlinks: true makes the build descend into a symlinked directory
// and index it, so a watcher that never descends the same link can build
// forever behind changes it never sees.
func TestEnumerateDescendsAFollowedSymlink(t *testing.T) {
	root, out := tree(t), t.TempDir()
	target := t.TempDir()
	write(t, target, "release.md", "# release notes\n")
	if err := os.Symlink(target, filepath.Join(root, "bootstrap", "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	follow := true
	c := conf([]config.Rule{{Match: "bootstrap/**", Override: config.Override{FollowSymlinks: &follow}}})
	p := EnumerateUnder(c, root, out, "bootstrap", obs.Discard())

	if !slices.Contains(rels(t, root, p), "bootstrap/linked") {
		t.Errorf("a followed symlinked directory was not watched: %v", rels(t, root, p))
	}
}

// Without follow_symlinks a symlinked directory stays a leaf, matching what
// the build itself lists it as: an entry, never a subtree to descend into.
func TestEnumerateDoesNotDescendAnUnfollowedSymlink(t *testing.T) {
	root, out := tree(t), t.TempDir()
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(root, "bootstrap", "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	p := EnumerateUnder(conf(nil), root, out, "bootstrap", obs.Discard())
	if slices.Contains(rels(t, root, p), "bootstrap/linked") {
		t.Errorf("an unfollowed symlink was watched as a directory: %v", rels(t, root, p))
	}
}

// A symlink cycle must not recurse forever: an ancestor directory reached
// again through a followed link is where descending stops.
func TestEnumerateStopsAtASymlinkLoop(t *testing.T) {
	root, out := tree(t), t.TempDir()
	if err := os.Symlink(root, filepath.Join(root, "bootstrap", "loop")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	follow := true
	c := conf([]config.Rule{{Match: "bootstrap/**", Override: config.Override{FollowSymlinks: &follow}}})

	done := make(chan Plan, 1)
	go func() { done <- EnumerateUnder(c, root, out, "bootstrap", obs.Discard()) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("EnumerateUnder never returned; a symlink loop was not detected")
	}
}

// A directory that cannot be read is one directory that will not report its
// changes, not a reason to refuse to watch the rest of the tree.
func TestEnumerateSurvivesAnUnreadableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions do not deny listing on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads a directory whatever its mode says")
	}
	root, out := tree(t), t.TempDir()
	locked := filepath.Join(root, "docs")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	p := Enumerate(conf(nil), root, out, obs.Discard())
	if !slices.Contains(rels(t, root, p), "bootstrap") {
		t.Error("stopped enumerating at the unreadable directory")
	}
}
