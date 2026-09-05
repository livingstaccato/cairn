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

	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/obs"
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
