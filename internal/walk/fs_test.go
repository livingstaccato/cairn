// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"os"
	"path/filepath"
	"testing"

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
	mk("bootstrap/_hidden.txt", 10)
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
	// _hidden.txt skipped by default; nested.conf is not this directory's business.
	want := []string{"linux", "bootstrap.sh", "ubuntu.iso"}
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
	s.Hidden = config.HiddenShow
	got, _, err := Dir(root, "bootstrap", s)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Errorf("got %v, want 4 entries including _hidden.txt", names(got))
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
		if len(got) != 3 {
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
	got, _, err := Tree(root, "bootstrap", config.Defaults(), 1000)
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
	if _, _, err := Tree(root, "bootstrap", config.Defaults(), 2); err == nil {
		t.Fatal("expected an error when the entry cap is exceeded")
	}
}

func TestTreeMissingIsError(t *testing.T) {
	if _, _, err := Tree(t.TempDir(), "nope", config.Defaults(), 10); err == nil {
		t.Fatal("expected an error for a missing directory")
	}
}
