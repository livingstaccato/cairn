// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

package emit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/livingstaccato/cairn/internal/config"
)

func cfg(t *testing.T, onConflict string) *config.Config {
	t.Helper()
	return &config.Config{
		Version:       1,
		OnConflict:    onConflict,
		IndexBasename: "index",
		Protect:       []string{"dists/**", "repodata/**", "**/Release"},
	}
}

func TestWriteCreatesFileAndParents(t *testing.T) {
	out := t.TempDir()
	if err := Write(cfg(t, config.ConflictError), out, "bootstrap/linux/index.json", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "bootstrap/linux/index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{}` {
		t.Errorf("body = %q", got)
	}
}

func TestWriteRefusesProtectedPath(t *testing.T) {
	out := t.TempDir()
	for _, p := range []string{"repodata/repomd.xml", "dists/stable/Packages", "pool/Release"} {
		err := Write(cfg(t, config.ConflictError), out, p, []byte("x"))
		if err == nil {
			t.Errorf("Write(%q) succeeded; protected paths must fail", p)
			continue
		}
		if !strings.Contains(err.Error(), "protected") {
			t.Errorf("Write(%q) error = %v, want it to name the protection", p, err)
		}
		if _, statErr := os.Stat(filepath.Join(out, p)); statErr == nil {
			t.Errorf("Write(%q) wrote the file anyway", p)
		}
	}
}

func TestWriteRefusesPathEscape(t *testing.T) {
	out := t.TempDir()
	for _, p := range []string{"../escaped.json", "a/../../escaped.json"} {
		if err := Write(cfg(t, config.ConflictError), out, p, []byte("x")); err == nil {
			t.Errorf("Write(%q) succeeded; a path escaping the output root must fail", p)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(out), "escaped.json")); err == nil {
		t.Error("a file was written outside the output root")
	}
}

func TestWriteTreatsSymlinkAsConflict(t *testing.T) {
	out := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim.txt")
	if err := os.WriteFile(victim, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(out, "index.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := Write(cfg(t, config.ConflictError), out, "index.json", []byte("generated")); err == nil {
		t.Error("a symlink at the output path must be a conflict, not a write-through")
	}
	got, _ := os.ReadFile(victim)
	if string(got) != "original" {
		t.Error("wrote through the symlink and clobbered its target")
	}
}

func TestWriteDanglingSymlinkIsAConflict(t *testing.T) {
	out := t.TempDir()
	if err := os.Symlink(filepath.Join(out, "nowhere"), filepath.Join(out, "index.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	// os.Stat would report ENOENT here and let the write through; Lstat sees
	// the link itself.
	if err := Write(cfg(t, config.ConflictError), out, "index.json", []byte("generated")); err == nil {
		t.Error("a dangling symlink at the output path must still be a conflict")
	}
}

func TestWriteConflictError(t *testing.T) {
	out := t.TempDir()
	target := filepath.Join(out, "bootstrap", "index.html")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("mirrored artifact"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Write(cfg(t, config.ConflictError), out, "bootstrap/index.html", []byte("generated")); err == nil {
		t.Fatal("expected a conflict error")
	}
	got, _ := os.ReadFile(target)
	if string(got) != "mirrored artifact" {
		t.Error("existing file was overwritten despite on_conflict: error")
	}
}

func TestWriteConflictSkip(t *testing.T) {
	out := t.TempDir()
	target := filepath.Join(out, "bootstrap", "index.html")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("mirrored artifact"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Write(cfg(t, config.ConflictSkip), out, "bootstrap/index.html", []byte("generated")); err != nil {
		t.Fatalf("on_conflict: skip must not error: %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "mirrored artifact" {
		t.Error("skip must leave the existing file untouched")
	}
}

func TestWriteUnwritableParentErrors(t *testing.T) {
	out := t.TempDir()
	// A file standing where a parent directory needs to be.
	if err := os.WriteFile(filepath.Join(out, "bootstrap"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(cfg(t, config.ConflictError), out, "bootstrap/index.json", []byte("x")); err == nil {
		t.Fatal("expected an error when a parent path is a file")
	}
}
