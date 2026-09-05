// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package walk

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/model"
)

func names(es []model.Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Name
	}
	return out
}

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mk := func(rel string, size int) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("bootstrap/bootstrap.sh", 512)
	mk("bootstrap/ubuntu.iso", 4096)
	mk("bootstrap/.hidden.txt", 10)
	mk("bootstrap/_notes.txt", 10)
	mk("bootstrap/linux/apt.list", 64)
	mk("bootstrap/linux/deep/nested.conf", 32)
	return root
}

func TestDirIsNotRecursive(t *testing.T) {
	root := fixture(t)
	got, warns, err := Dir(root, "bootstrap", config.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	// .hidden.txt is skipped by default; _notes.txt is not, because an
	// underscore is nobody's filesystem convention. nested.conf belongs to linux/.
	want := []string{"linux", "_notes.txt", "bootstrap.sh", "ubuntu.iso"}
	if len(got) != len(want) {
		t.Fatalf("got %d entries %v, want %d %v", len(got), names(got), len(want), want)
	}
	for i, n := range want {
		if got[i].Name != n {
			t.Errorf("entry %d = %q, want %q (dirs_first then name asc)", i, got[i].Name, n)
		}
	}
	for _, e := range got {
		if e.Depth != 1 {
			t.Errorf("%s: Depth = %d, want 1", e.Name, e.Depth)
		}
	}
}

func TestDirPathsAndTrailingSlash(t *testing.T) {
	root := fixture(t)
	got, _, err := Dir(root, "bootstrap", config.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range got {
		switch e.Name {
		case "linux":
			if e.Path != "/bootstrap/linux/" {
				t.Errorf("dir Path = %q, want a trailing slash", e.Path)
			}
			if e.Size != 0 {
				t.Errorf("dir Size = %d, want 0", e.Size)
			}
		case "bootstrap.sh":
			if e.Path != "/bootstrap/bootstrap.sh" {
				t.Errorf("file Path = %q", e.Path)
			}
		}
	}
}

func TestDirExactSizeNotRounded(t *testing.T) {
	root := fixture(t)
	got, _, err := Dir(root, "bootstrap", config.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range got {
		if e.Name == "bootstrap.sh" && e.Size != 512 {
			t.Errorf("Size = %d, want exactly 512", e.Size)
		}
	}
}

func TestDirHiddenShow(t *testing.T) {
	root := fixture(t)
	s := config.Defaults()
	s.Hide = nil
	got, _, err := Dir(root, "bootstrap", s)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Errorf("got %v, want every entry including the dotfile", names(got))
	}
}

func TestDirSorting(t *testing.T) {
	root := fixture(t)
	t.Run("size desc, dirs mixed in", func(t *testing.T) {
		s := config.Defaults()
		s.Sort, s.Order, s.DirsFirst = config.SortSize, config.OrderDesc, false
		got, _, err := Dir(root, "bootstrap", s)
		if err != nil {
			t.Fatal(err)
		}
		if got[0].Name != "ubuntu.iso" {
			t.Errorf("first = %q, want ubuntu.iso (largest first)", got[0].Name)
		}
	})
	t.Run("name desc keeps dirs first when asked", func(t *testing.T) {
		s := config.Defaults()
		s.Order = config.OrderDesc
		got, _, err := Dir(root, "bootstrap", s)
		if err != nil {
			t.Fatal(err)
		}
		if !got[0].IsDir {
			t.Errorf("first = %q, want the directory even in desc order", got[0].Name)
		}
	})
	t.Run("kind", func(t *testing.T) {
		s := config.Defaults()
		s.Sort, s.DirsFirst = config.SortKind, false
		got, _, err := Dir(root, "bootstrap", s)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 4 {
			t.Fatalf("got %v", names(got))
		}
	})
	t.Run("modtime", func(t *testing.T) {
		s := config.Defaults()
		s.Sort = config.SortModTime
		if _, _, err := Dir(root, "bootstrap", s); err != nil {
			t.Fatal(err)
		}
	})
}

func TestDirCountsChildren(t *testing.T) {
	root := fixture(t)
	got, _, err := Dir(root, "bootstrap", config.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range got {
		if e.Name == "linux" {
			if !e.IsDir {
				t.Fatal("linux should be a directory")
			}
			if e.Count != 2 {
				t.Errorf("Count = %d, want 2 (apt.list and deep/)", e.Count)
			}
		}
	}
}

func TestDirMissingIsError(t *testing.T) {
	if _, _, err := Dir(t.TempDir(), "nope", config.Defaults()); err == nil {
		t.Fatal("expected error for a missing directory")
	}
}

func TestDirSkipsSymlinkByDefault(t *testing.T) {
	root := fixture(t)
	if err := os.Symlink(filepath.Join(root, "bootstrap", "ubuntu.iso"),
		filepath.Join(root, "bootstrap", "alias.iso")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	got, _, err := Dir(root, "bootstrap", config.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range got {
		if e.Name == "alias.iso" {
			t.Error("symlinks must be skipped unless follow_symlinks is set")
		}
	}
}

func TestDirSkipsEscapingSymlink(t *testing.T) {
	root := fixture(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "bootstrap", "leak")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	s := config.Defaults()
	s.FollowSymlinks = true
	got, warns, err := Dir(root, "bootstrap", s)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range got {
		if e.Name == "leak" {
			t.Error("a symlink pointing outside the root was listed")
		}
	}
	if len(warns) == 0 {
		t.Error("expected a warning naming the skipped symlink")
	}
	if warns[0].String() == "" {
		t.Error("Warning.String must be usable in a log line")
	}
}

func TestDirFollowsContainedSymlink(t *testing.T) {
	root := fixture(t)
	if err := os.Symlink(filepath.Join(root, "bootstrap", "linux"),
		filepath.Join(root, "bootstrap", "alias")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	s := config.Defaults()
	s.FollowSymlinks = true
	got, _, err := Dir(root, "bootstrap", s)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range got {
		if e.Name == "alias" {
			found = true
		}
	}
	if !found {
		t.Errorf("a symlink inside the root should be listed: %v", names(got))
	}
}

func TestTreeRecursesWithDepth(t *testing.T) {
	root := fixture(t)
	got, _, err := Tree(root, "bootstrap", config.Defaults(), 1000, nil)
	if err != nil {
		t.Fatal(err)
	}
	depth := map[string]int{}
	for _, e := range got {
		depth[e.Name] = e.Depth
	}
	for name, want := range map[string]int{
		"bootstrap.sh": 1, "linux": 1, "apt.list": 2, "deep": 2, "nested.conf": 3,
	} {
		if depth[name] != want {
			t.Errorf("%s: Depth = %d, want %d", name, depth[name], want)
		}
	}
}

func TestTreeCapIsAnError(t *testing.T) {
	root := fixture(t)
	if _, _, err := Tree(root, "bootstrap", config.Defaults(), 2, nil); err == nil {
		t.Fatal("expected an error when the entry cap is exceeded")
	}
}

func TestTreeMissingIsError(t *testing.T) {
	if _, _, err := Tree(t.TempDir(), "nope", config.Defaults(), 10, nil); err == nil {
		t.Fatal("expected an error for a missing directory")
	}
}

// Every sort key falls back to name, which is what makes the order total and so
// stable across runs — two files of the same size must not swap places between
// builds and change every emitted file.
func TestLessByFallsBackToName(t *testing.T) {
	now := time.Now()
	a := model.Entry{Name: "alpha", Size: 10, Kind: "doc", ModTime: now}
	b := model.Entry{Name: "beta", Size: 10, Kind: "doc", ModTime: now}

	for _, key := range []string{config.SortSize, config.SortModTime, config.SortKind, config.SortName, "unknown"} {
		if !lessBy(key, a, b) {
			t.Errorf("lessBy(%q) did not fall back to name ordering", key)
		}
		if lessBy(key, b, a) {
			t.Errorf("lessBy(%q) is not antisymmetric on the name fallback", key)
		}
	}
}

func TestLessByOrdersByEachKey(t *testing.T) {
	early := time.Now().Add(-time.Hour)
	late := time.Now()
	cases := []struct {
		key  string
		a, b model.Entry
	}{
		{config.SortSize, model.Entry{Name: "z", Size: 1}, model.Entry{Name: "a", Size: 2}},
		{config.SortModTime, model.Entry{Name: "z", ModTime: early}, model.Entry{Name: "a", ModTime: late}},
		{config.SortKind, model.Entry{Name: "z", Kind: "archive"}, model.Entry{Name: "a", Kind: "doc"}},
	}
	for _, c := range cases {
		if !lessBy(c.key, c.a, c.b) {
			t.Errorf("lessBy(%q) ignored its key in favour of the name", c.key)
		}
	}
}

func TestSortHonorsWeight(t *testing.T) {
	es := []model.Entry{
		{Name: "c.txt"},
		{Name: "a.txt", Weight: 2},
		{Name: "b.txt", Weight: 1},
		{Name: "d.txt"},
	}
	s := config.Defaults()
	s.DirsFirst = false
	sortEntries(es, s)

	// Weighted entries lead, in weight order; everything else keeps the
	// configured sort. Zero means unweighted, not "weight zero", or authoring
	// one entry would silently reorder the whole directory.
	want := []string{"b.txt", "a.txt", "c.txt", "d.txt"}
	for i, n := range want {
		if es[i].Name != n {
			t.Fatalf("got %v, want %v", names(es), want)
		}
	}
}
