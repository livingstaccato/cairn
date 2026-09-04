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

func TestWriteOverwritesItsOwnOutput(t *testing.T) {
	out := t.TempDir()
	c := cfg(t, config.ConflictError)

	w1 := NewWriter(c, out)
	if err := w1.Write("bootstrap/index.json", []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := w1.Save(); err != nil {
		t.Fatal(err)
	}

	// A generator that cannot run twice is not finished.
	w2 := NewWriter(c, out)
	if err := w2.Write("bootstrap/index.json", []byte("second")); err != nil {
		t.Fatalf("re-running must overwrite cairn's own output: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(out, "bootstrap/index.json"))
	if string(got) != "second" {
		t.Errorf("body = %q, want the second run's content", got)
	}
}

func TestWriteStillRefusesForeignFileAfterAManifestExists(t *testing.T) {
	out := t.TempDir()
	c := cfg(t, config.ConflictError)

	w1 := NewWriter(c, out)
	if err := w1.Write("bootstrap/index.json", []byte("ours")); err != nil {
		t.Fatal(err)
	}
	if err := w1.Save(); err != nil {
		t.Fatal(err)
	}

	foreign := filepath.Join(out, "bootstrap", "ubuntu.iso")
	if err := os.WriteFile(foreign, []byte("mirrored artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NewWriter(c, out).Write("bootstrap/ubuntu.iso", []byte("clobber")); err == nil {
		t.Error("owning one path must not grant permission to overwrite another")
	}
	got, _ := os.ReadFile(foreign)
	if string(got) != "mirrored artifact" {
		t.Error("a file cairn did not create was overwritten")
	}
}

func TestWrittenAndSaveRoundTrip(t *testing.T) {
	out := t.TempDir()
	w := NewWriter(cfg(t, config.ConflictError), out)
	if err := w.Write("a/index.json", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if len(w.Written()) != 1 || w.Written()[0] != "a/index.json" {
		t.Errorf("Written = %v", w.Written())
	}
	if err := w.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, ManifestFile)); err != nil {
		t.Errorf("manifest not written: %v", err)
	}
}

func TestSaveUnwritableRootErrors(t *testing.T) {
	out := t.TempDir()
	blocked := filepath.Join(out, "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NewWriter(cfg(t, config.ConflictError), blocked).Save(); err == nil {
		t.Fatal("expected an error saving into a path that is a file")
	}
}

func TestCorruptManifestIsNotFatal(t *testing.T) {
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, ManifestFile), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NewWriter(cfg(t, config.ConflictError), out).Write("index.json", []byte("x")); err != nil {
		t.Fatalf("a corrupt manifest must be discarded, not fatal: %v", err)
	}
}

func TestWriteCreatesFileAndParents(t *testing.T) {
	out := t.TempDir()
	if err := NewWriter(cfg(t, config.ConflictError), out).Write("bootstrap/linux/index.json", []byte(`{}`)); err != nil {
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
		err := NewWriter(cfg(t, config.ConflictError), out).Write(p, []byte("x"))
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
		if err := NewWriter(cfg(t, config.ConflictError), out).Write(p, []byte("x")); err == nil {
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

	if err := NewWriter(cfg(t, config.ConflictError), out).Write("index.json", []byte("generated")); err == nil {
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
	if err := NewWriter(cfg(t, config.ConflictError), out).Write("index.json", []byte("generated")); err == nil {
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

	if err := NewWriter(cfg(t, config.ConflictError), out).Write("bootstrap/index.html", []byte("generated")); err == nil {
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

	if err := NewWriter(cfg(t, config.ConflictSkip), out).Write("bootstrap/index.html", []byte("generated")); err != nil {
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
	if err := NewWriter(cfg(t, config.ConflictError), out).Write("bootstrap/index.json", []byte("x")); err == nil {
		t.Fatal("expected an error when a parent path is a file")
	}
}
