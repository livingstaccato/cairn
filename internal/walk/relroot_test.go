// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package walk

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/livingstaccato/cairn/internal/config"
)

// relFixture is a tree reached by a relative path, which is how every config
// cairn documents names its root: — the README quickstart, cairn init,
// testdata/example and exampleSite all use ./tree.
func relFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, d := range []string{"tree/a", "tree/b"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"tree/a/f.txt", "tree/b/g.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
	return "tree"
}

// follow_symlinks did nothing at all when root: was relative. Every symlink was
// refused as "symlink escapes the root", including ones plainly inside it,
// because withinRoot made the root absolute and left the target as it came --
// so filepath.Rel could not relate the two, and the error path is
// indistinguishable from a real escape.
//
// The existing coverage all builds its root from t.TempDir(), which is
// absolute, so the feature looked tested and was broken for every documented
// config.
func TestFollowsAContainedSymlinkUnderARelativeRoot(t *testing.T) {
	root := relFixture(t)
	if err := os.Symlink("../b", filepath.Join(root, "a", "sibling")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink("../b/g.txt", filepath.Join(root, "a", "file.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	s := config.Defaults()
	s.FollowSymlinks = true
	got, warns, err := Dir(root, "a", s)
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Errorf("a symlink inside the root warned: %v", warns[0].Err)
	}
	for _, want := range []string{"sibling", "file.txt"} {
		var found bool
		for _, e := range got {
			if e.Name == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%q is inside the root and was not listed: %v", want, names(got))
		}
	}
}

// The containment property has to survive the fix: a relative root must not
// become a way to list something outside the tree.
func TestStillRefusesAnEscapingSymlinkUnderARelativeRoot(t *testing.T) {
	root := relFixture(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"),
		filepath.Join(root, "a", "leak")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	s := config.Defaults()
	s.FollowSymlinks = true
	got, warns, err := Dir(root, "a", s)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range got {
		if e.Name == "leak" {
			t.Error("a symlink out of the tree was listed")
		}
	}
	if len(warns) != 1 {
		t.Fatalf("want one warning about the escape, got %v", warns)
	}
}

// withinRoot's own contract, since it is the one containment helper that takes
// a target from its caller rather than building one from the root.
func TestWithinRootAcceptsRelativePaths(t *testing.T) {
	root := relFixture(t)
	if !withinRoot(root, filepath.Join(root, "a")) {
		t.Error("a directory inside a relative root reported as outside it")
	}
	if withinRoot(root, t.TempDir()) {
		t.Error("an unrelated directory reported as inside the root")
	}
}
