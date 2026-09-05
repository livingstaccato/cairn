// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for which filesystem events reach the rebuild.

package watch

import (
	"path/filepath"
	"testing"

	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/obs"
)

func TestFilterIgnores(t *testing.T) {
	root := t.TempDir()
	f := NewFilter(conf(nil), root, filepath.Join(root, "_out"), obs.Discard())

	cases := []struct {
		rel  string
		want bool
		why  string
	}{
		{"docs/intro.md", false, "authored content"},
		{"docs/_meta.yaml", false, "a sidecar supplies titles, so it changes the output"},
		{"docs/index.json", true, "cairn's own listing"},
		{"docs/index.html", true, "cairn's own page"},
		{"tree.json", true, "cairn's own recursive listing"},
		{"SHA256SUMS", true, "cairn's own checksums"},
		{".cairn-manifest.json", true, "cairn's own ownership record"},
		{".cairn-cache.json", true, "cairn's own hash cache"},
		{".git/objects/ab/cd", true, "inside a directory the build hides"},
		{".DS_Store", true, "hidden by the default glob"},
		// Deliberately not a name cairn generates: with one, the generated
		// check discards it and the subtree skip is never exercised.
		{"_out/docs/notes.txt", true, "inside the output directory"},
		// The segment test again, at the other end: a sibling that merely
		// shares a prefix with the output directory is content.
		{"_output-notes/todo.md", false, "a sibling of the output directory"},
	}
	for _, tc := range cases {
		abs := filepath.Join(root, filepath.FromSlash(tc.rel))
		if got := f.Ignore(abs); got != tc.want {
			t.Errorf("Ignore(%q) = %v, want %v — %s", tc.rel, got, tc.want, tc.why)
		}
	}
}

// An event for a path outside the watched tree is not something to act on. It
// should not be possible, and acting on it would rebuild a scope computed from
// a path that has no place in the tree.
func TestFilterIgnoresPathsOutsideTheRoot(t *testing.T) {
	root := t.TempDir()
	f := NewFilter(conf(nil), root, root, obs.Discard())

	outside := filepath.Join(filepath.Dir(root), "elsewhere", "file.txt")
	if _, ok := f.Rel(outside); ok {
		t.Errorf("Rel(%q) accepted a path outside the root", outside)
	}
	if !f.Ignore(outside) {
		t.Errorf("Ignore(%q) = false, want true", outside)
	}
}

// The root itself is watched, and an event on it is a change to what the root
// listing describes.
func TestFilterKeepsTheRoot(t *testing.T) {
	root := t.TempDir()
	f := NewFilter(conf(nil), root, t.TempDir(), obs.Discard())
	if f.Ignore(root) {
		t.Error("Ignore(root) = true, want false")
	}
}

// Output written outside the tree leaves nothing to skip inside it; skipping a
// subtree on a relative path that escapes the root would skip real content.
func TestFilterSkipsNoSubtreeWhenOutputIsElsewhere(t *testing.T) {
	root, out := t.TempDir(), t.TempDir()
	f := NewFilter(conf(nil), root, out, obs.Discard())
	if f.outRel != "" {
		t.Errorf("outRel = %q, want empty for output outside the root", f.outRel)
	}
}

// hide: is per-directory, so a directory that widens it has to widen what the
// watcher discards too. Otherwise the build ignores a file and the watcher
// rebuilds for it.
func TestFilterHonoursAPerDirectoryHide(t *testing.T) {
	root := t.TempDir()
	write(t, root, "logs/build.log", "")
	hide := []string{"**/.*", "logs/*.log"}
	c := conf([]config.Rule{{Match: "logs", Override: config.Override{Hide: &hide}}})

	f := NewFilter(c, root, t.TempDir(), obs.Discard())
	if !f.Ignore(filepath.Join(root, "logs", "build.log")) {
		t.Error("a file the build hides woke the watcher")
	}
	if f.Ignore(filepath.Join(root, "logs", "notes.md")) {
		t.Error("a file the build lists did not wake the watcher")
	}
}
