// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package emit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/livingstaccato/cairn/internal/config"
)

// stray puts a file at relPath under out that cairn has no claim on.
func stray(t *testing.T, out, relPath, body string) string {
	t.Helper()
	p := filepath.Join(out, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func body(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestAdoptClaimsAnExistingUnownedPath is the recovery this exists for: a tree
// whose manifest was lost holds hundreds of thousands of files cairn wrote and
// no longer claims, and on_conflict: error refuses every one of them.
func TestAdoptClaimsAnExistingUnownedPath(t *testing.T) {
	out := t.TempDir()
	p := stray(t, out, "bootstrap/index.json", "written by an earlier cairn")

	w := NewWriterWith(cfg(t, config.ConflictError), out, Options{Adopt: true})
	if err := w.Write("bootstrap/index.json", []byte("fresh")); err != nil {
		t.Fatalf("--adopt must claim the path, not refuse it: %v", err)
	}
	if got := body(t, p); got != "fresh" {
		t.Errorf("body = %q, want the new build's content", got)
	}
	if got := w.Adopted(); len(got) != 1 || got[0] != "bootstrap/index.json" {
		t.Errorf("Adopted = %v, want [bootstrap/index.json]", got)
	}
}

// TestWithoutAdoptAnUnownedPathIsStillRefused is the control: adopting is a
// deliberate act, never the default.
func TestWithoutAdoptAnUnownedPathIsStillRefused(t *testing.T) {
	out := t.TempDir()
	p := stray(t, out, "bootstrap/index.json", "somebody else's")

	if err := NewWriter(cfg(t, config.ConflictError), out).Write("bootstrap/index.json", []byte("x")); err == nil {
		t.Error("a plain build must still refuse a path cairn does not own")
	}
	if got := body(t, p); got != "somebody else's" {
		t.Errorf("body = %q, want it untouched", got)
	}
}

// TestAdoptDoesNotReachAProtectedPath: protect: names paths another tool owns —
// apt's signed dists/, dnf's repodata/. Adopting is about reclaiming cairn's own
// output, and must not become a way to take those.
func TestAdoptDoesNotReachAProtectedPath(t *testing.T) {
	out := t.TempDir()
	p := stray(t, out, "dists/stable/Release", "signed by apt-ftparchive")

	w := NewWriterWith(cfg(t, config.ConflictError), out, Options{Adopt: true})
	if err := w.Write("dists/stable/Release", []byte("clobber")); err != nil {
		t.Fatal(err)
	}
	if got := body(t, p); got != "signed by apt-ftparchive" {
		t.Errorf("body = %q, want the protected file untouched", got)
	}
	if got := w.Adopted(); len(got) != 0 {
		t.Errorf("Adopted = %v, want nothing: a protected path is never claimed", got)
	}
}

// TestAdoptStillRefusesToLeaveTheOutputRoot: containment is not a policy, and
// no flag relaxes it.
func TestAdoptStillRefusesToLeaveTheOutputRoot(t *testing.T) {
	out := t.TempDir()
	w := NewWriterWith(cfg(t, config.ConflictError), out, Options{Adopt: true})
	if err := w.Write("../escaped.json", []byte("x")); err == nil {
		t.Error("--adopt must not grant a write outside the output root")
	}
}

// TestAdoptOverridesOnConflictSkip: under skip the path would be left alone and
// stay unowned, so the next build would skip it again and Prune would never
// reach it. Asking to adopt is asking to take the path.
func TestAdoptOverridesOnConflictSkip(t *testing.T) {
	out := t.TempDir()
	p := stray(t, out, "bootstrap/index.json", "left alone under skip")

	w := NewWriterWith(cfg(t, config.ConflictSkip), out, Options{Adopt: true})
	if err := w.Write("bootstrap/index.json", []byte("fresh")); err != nil {
		t.Fatal(err)
	}
	if got := body(t, p); got != "fresh" {
		t.Errorf("body = %q, want the new build's content", got)
	}
}

// TestAnAdoptedPathIsRecordedInTheManifest is what makes the recovery stick: a
// claim that is not saved leaves the next run refusing the same file again.
func TestAnAdoptedPathIsRecordedInTheManifest(t *testing.T) {
	out := t.TempDir()
	stray(t, out, "bootstrap/index.json", "orphaned")

	w := NewWriterWith(cfg(t, config.ConflictError), out, Options{Adopt: true})
	if err := w.Write("bootstrap/index.json", []byte("fresh")); err != nil {
		t.Fatal(err)
	}
	if err := w.Save(); err != nil {
		t.Fatal(err)
	}

	// The next run is an ordinary one, with no flag at all.
	if err := NewWriter(cfg(t, config.ConflictError), out).Write("bootstrap/index.json", []byte("later")); err != nil {
		t.Errorf("an adopted path must be owned by every later run: %v", err)
	}
}

// TestADryAdoptClaimsNothingOnDisk: the two flags compose, and the preview is
// the point — an operator reads what would be taken before anything is.
func TestADryAdoptClaimsNothingOnDisk(t *testing.T) {
	out := t.TempDir()
	p := stray(t, out, "bootstrap/index.json", "orphaned")

	w := NewWriterWith(cfg(t, config.ConflictError), out, Options{Adopt: true, Dry: true})
	if err := w.Write("bootstrap/index.json", []byte("fresh")); err != nil {
		t.Fatal(err)
	}
	if got := w.Adopted(); len(got) != 1 {
		t.Errorf("Adopted = %v, want the path it would have claimed", got)
	}
	if got := body(t, p); got != "orphaned" {
		t.Errorf("body = %q, want it untouched by a dry run", got)
	}
}

// TestTheConflictErrorNamesBothRemedies. The two are for opposite situations
// and the message is all an operator has to tell them apart: a file that is
// genuinely somebody else's should be left alone, and output cairn wrote and
// can no longer prove it wrote should be reclaimed. Naming only skip, as this
// did, sends the second case to the one setting that freezes the mirror.
func TestTheConflictErrorNamesBothRemedies(t *testing.T) {
	out := t.TempDir()
	stray(t, out, "bootstrap/index.json", "already here")

	err := NewWriter(cfg(t, config.ConflictError), out).Write("bootstrap/index.json", []byte("x"))
	if err == nil {
		t.Fatal("expected a conflict")
	}
	for _, want := range []string{"on_conflict: skip", "index_basename", "--adopt"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the conflict error does not mention %q: %v", want, err)
		}
	}
}
