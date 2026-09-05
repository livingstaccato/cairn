// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package emit

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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

func TestWriteSkipsProtectedPath(t *testing.T) {
	out := t.TempDir()
	w := NewWriter(cfg(t, config.ConflictError), out)
	for _, p := range []string{"repodata/repomd.xml", "dists/stable/Packages", "pool/Release"} {
		// A protect: glob is the operator declaring intent, not reporting a
		// fault. Skipping keeps a build over a package repo running; failing
		// made protect: unusable for the only job it has.
		if err := w.Write(p, []byte("x")); err != nil {
			t.Errorf("Write(%q) = %v, want a silent skip", p, err)
		}
		if _, statErr := os.Stat(filepath.Join(out, p)); statErr == nil {
			t.Errorf("Write(%q) wrote the file anyway", p)
		}
	}
	// Skipped paths must not be claimed: ownership would let a later run
	// overwrite the very files protect: exists to shield, and Prune would
	// delete them.
	if got := w.Written(); len(got) != 0 {
		t.Errorf("Written() = %v, want nothing claimed for skipped writes", got)
	}
	if got := w.Protected(); len(got) != 3 {
		t.Errorf("Protected() = %v, want all three recorded", got)
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

func TestPruneRemovesOnlyLastRunsOutput(t *testing.T) {
	out := t.TempDir()
	c := cfg(t, config.ConflictError)

	w1 := NewWriter(c, out)
	for _, p := range []string{"a/index.json", "a/index.csv", "b/index.json"} {
		if err := w1.Write(p, []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := w1.Save(); err != nil {
		t.Fatal(err)
	}

	// A file cairn never wrote, sharing the tree.
	foreign := filepath.Join(out, "b", "ubuntu.iso")
	if err := os.WriteFile(foreign, []byte("artifact"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The second run covers a/ but not b/ — b/ has gone away upstream.
	w2 := NewWriter(c, out)
	if err := w2.Write("a/index.json", []byte("y")); err != nil {
		t.Fatal(err)
	}
	removed, err := w2.Prune()
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"a/index.csv", "b/index.json"}
	if len(removed) != len(want) {
		t.Fatalf("removed %v, want %v", removed, want)
	}
	for i, p := range want {
		if removed[i] != p {
			t.Errorf("removed[%d] = %q, want %q (sorted for a stable log)", i, removed[i], p)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "a/index.json")); err != nil {
		t.Errorf("a live output was pruned: %v", err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Errorf("pruning touched a file cairn did not write: %v", err)
	}
}

// A directory that held only cairn's output is itself stale; one still holding
// an artifact is not.
func TestPruneRemovesEmptiedDirectories(t *testing.T) {
	out := t.TempDir()
	c := cfg(t, config.ConflictError)

	w1 := NewWriter(c, out)
	for _, p := range []string{"gone/index.json", "gone/deeper/index.json", "kept/index.json"} {
		if err := w1.Write(p, []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := w1.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "kept", "artifact.bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	w2 := NewWriter(c, out)
	if _, err := w2.Prune(); err != nil {
		t.Fatal(err)
	}

	// Nested directories collapse in one pass, deepest first.
	if _, err := os.Stat(filepath.Join(out, "gone")); !os.IsNotExist(err) {
		t.Errorf("emptied directory survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "kept")); err != nil {
		t.Errorf("a directory still holding an artifact was removed: %v", err)
	}
}

func TestPruneWithNoManifestDoesNothing(t *testing.T) {
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "index.json"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, err := NewWriter(cfg(t, config.ConflictError), out).Prune()
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 0 {
		t.Errorf("removed %v without a manifest; stale files are a nuisance, deletion is not", removed)
	}
	if _, err := os.Stat(filepath.Join(out, "index.json")); err != nil {
		t.Errorf("an unowned file was deleted: %v", err)
	}
}

// A path already gone is not an error: an operator may have cleaned up by hand.
func TestPruneToleratesAlreadyDeleted(t *testing.T) {
	out := t.TempDir()
	c := cfg(t, config.ConflictError)

	w1 := NewWriter(c, out)
	if err := w1.Write("a/index.json", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := w1.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(out, "a")); err != nil {
		t.Fatal(err)
	}

	removed, err := NewWriter(c, out).Prune()
	if err != nil {
		t.Fatalf("a path already gone must not fail the prune: %v", err)
	}
	if len(removed) != 1 {
		t.Errorf("removed = %v, want the path still reported", removed)
	}
}

func TestDigestPreview(t *testing.T) {
	long := strings.Repeat("a", 64)
	if got := DigestPreview(long); got != strings.Repeat("a", 12)+"…" {
		t.Errorf("got %q", got)
	}
	if got := DigestPreview(""); got != "" {
		t.Errorf("an entry with no digest must render nothing, got %q", got)
	}
	// Shorter than the preview: return it whole rather than an ellipsis on air.
	if got := DigestPreview("abc"); got != "abc" {
		t.Errorf("got %q, want the value unchanged", got)
	}
}

func TestSavePartialKeepsPriorOwnership(t *testing.T) {
	out := t.TempDir()
	c := cfg(t, config.ConflictError)

	// A run that completed, owning two paths.
	first := NewWriter(c, out)
	for _, p := range []string{"a/index.json", "b/index.json"} {
		if err := first.Write(p, []byte("{}")); err != nil {
			t.Fatal(err)
		}
	}
	if err := first.Save(); err != nil {
		t.Fatal(err)
	}

	// A run that died after rewriting only the first of them.
	second := NewWriter(c, out)
	if err := second.Write("a/index.json", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := second.SavePartial(); err != nil {
		t.Fatal(err)
	}

	// b/index.json must still be claimed. Dropping it would make the next run
	// refuse a file cairn itself created.
	third := NewWriter(c, out)
	for _, p := range []string{"a/index.json", "b/index.json"} {
		if err := third.Write(p, []byte("{}")); err != nil {
			t.Errorf("Write(%q) after a partial save = %v, want the path still owned", p, err)
		}
	}
}

// underScope decides what a scoped rebuild is allowed to delete and disown, so
// it is worth stating the boundary cases outright rather than only reaching it
// through a build.
func TestUnderScope(t *testing.T) {
	for _, tc := range []struct {
		path, scope string
		want        bool
	}{
		{"/docs/index.json", "", true},           // no scope is the whole tree
		{"/docs/index.json", ".", true},          // and so is the root
		{"/docs/index.json", "docs", true},       // inside
		{"/docs/a/b/index.json", "docs", true},   // deeper inside
		{"/docs", "docs", true},                  // the scope directory itself
		{"/docs-old/index.json", "docs", false},  // a sibling sharing a prefix
		{"/other/index.json", "docs", false},     // unrelated
		{"/documentation/x.json", "docs", false}, // prefix of a longer name
	} {
		if got := underScope(tc.path, tc.scope); got != tc.want {
			t.Errorf("underScope(%q, %q) = %v, want %v", tc.path, tc.scope, got, tc.want)
		}
	}
}

// A scoped prune removes stale output inside the scope and leaves the rest of
// the previous run's output where it is.
func TestPruneScopedLeavesOtherSubtreesAlone(t *testing.T) {
	out := t.TempDir()
	c := cfg(t, config.ConflictError)

	w1 := NewWriter(c, out)
	for _, p := range []string{"docs/index.json", "docs/stale.json", "other/index.json"} {
		if err := w1.Write(p, []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := w1.Save(); err != nil {
		t.Fatal(err)
	}

	// A second run that rewrites only part of docs and nothing else.
	w2 := NewWriter(c, out)
	if err := w2.Write("docs/index.json", []byte("y")); err != nil {
		t.Fatal(err)
	}
	removed, err := w2.PruneScoped("docs")
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || !strings.Contains(removed[0], "stale.json") {
		t.Errorf("pruned %v, want only docs/stale.json", removed)
	}
	if _, err := os.Stat(filepath.Join(out, "other", "index.json")); err != nil {
		t.Errorf("a scoped prune deleted output in another subtree: %v", err)
	}
}

// A scoped save has to keep claiming what it did not touch, or the next run
// sees the rest of the tree as somebody else's files.
func TestSaveScopedKeepsOwnershipOutsideTheScope(t *testing.T) {
	out := t.TempDir()
	c := cfg(t, config.ConflictError)

	w1 := NewWriter(c, out)
	for _, p := range []string{"docs/index.json", "other/index.json"} {
		if err := w1.Write(p, []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := w1.Save(); err != nil {
		t.Fatal(err)
	}

	w2 := NewWriter(c, out)
	if err := w2.Write("docs/index.json", []byte("y")); err != nil {
		t.Fatal(err)
	}
	if err := w2.SaveScoped("docs"); err != nil {
		t.Fatal(err)
	}

	// The proof: a third writer must still consider the untouched file its own.
	w3 := NewWriter(c, out)
	if err := w3.Write("other/index.json", []byte("z")); err != nil {
		t.Errorf("output outside the scope was disowned: %v", err)
	}
}

// Rewriting an identical file moves its modification time. In a mirror that is
// a change the parent's listing records and a watcher reacts to, so a build
// that writes the same bytes twice never settles.
func TestWriteLeavesAnIdenticalFileAlone(t *testing.T) {
	out := t.TempDir()
	body := []byte(`{"entries":[]}`)

	w := NewWriter(cfg(t, config.ConflictError), out)
	if err := w.Write("docs/index.json", body); err != nil {
		t.Fatal(err)
	}
	if err := w.Save(); err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(out, "docs", "index.json")
	before := modTime(t, abs)

	// A second run, as the watcher would do it.
	time.Sleep(10 * time.Millisecond)
	w2 := NewWriter(cfg(t, config.ConflictError), out)
	if err := w2.Write("docs/index.json", body); err != nil {
		t.Fatal(err)
	}
	if got := modTime(t, abs); !got.Equal(before) {
		t.Errorf("the file was rewritten: mtime moved from %v to %v", before, got)
	}
	if w2.Unchanged() != 1 {
		t.Errorf("Unchanged = %d, want 1", w2.Unchanged())
	}
}

// The regression that would be silent: a skipped write is still a file this run
// owns. Left out of the manifest, the next run's Prune deletes it — a build
// that settles by removing its own output.
func TestAnUnchangedFileStaysOwned(t *testing.T) {
	out := t.TempDir()
	body := []byte("same")

	w := NewWriter(cfg(t, config.ConflictError), out)
	if err := w.Write("index.json", body); err != nil {
		t.Fatal(err)
	}
	if err := w.Save(); err != nil {
		t.Fatal(err)
	}

	w2 := NewWriter(cfg(t, config.ConflictError), out)
	if err := w2.Write("index.json", body); err != nil {
		t.Fatal(err)
	}
	if got := w2.Written(); !slices.Contains(got, "index.json") {
		t.Fatalf("Written = %v, want the unchanged path recorded", got)
	}
	pruned, err := w2.Prune()
	if err != nil {
		t.Fatal(err)
	}
	if len(pruned) != 0 {
		t.Errorf("pruned %v; an unchanged file was treated as stale", pruned)
	}
	if _, err := os.Stat(filepath.Join(out, "index.json")); err != nil {
		t.Errorf("the unchanged file was deleted: %v", err)
	}
}

func TestWriteReplacesAChangedFile(t *testing.T) {
	out := t.TempDir()
	w := NewWriter(cfg(t, config.ConflictError), out)
	if err := w.Write("index.json", []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := w.Save(); err != nil {
		t.Fatal(err)
	}

	w2 := NewWriter(cfg(t, config.ConflictError), out)
	if err := w2.Write("index.json", []byte("second")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second" {
		t.Errorf("file holds %q, want %q", got, "second")
	}
	if w2.Unchanged() != 0 {
		t.Errorf("Unchanged = %d, want 0", w2.Unchanged())
	}
}

// Same length, different bytes. A size check alone is a cheap first pass, not
// the answer.
func TestUnchangedComparesContentNotLength(t *testing.T) {
	out := t.TempDir()
	abs := filepath.Join(out, "f")
	if err := os.WriteFile(abs, []byte("aaaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	if unchanged(abs, []byte("bbbb")) {
		t.Error("two different four-byte files compared equal")
	}
	if !unchanged(abs, []byte("aaaa")) {
		t.Error("identical content compared unequal")
	}
}

// A symlink at an output path is something someone else put there. Comparing
// through it would read the target's bytes and then leave the link standing —
// the one outcome this package exists to prevent.
//
// Constructed so the size check cannot answer it by accident: a symlink's own
// size is the length of the path it holds, so the target's content is made
// equal to that path. Both the length and the bytes then match, and only the
// regular-file guard can tell the difference.
func TestUnchangedRefusesToCompareThroughASymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	body := []byte(target)
	if err := os.WriteFile(target, body, 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != int64(len(body)) {
		t.Skipf("this filesystem does not size a symlink by its target path (%d vs %d)",
			fi.Size(), len(body))
	}
	if unchanged(link, body) {
		t.Error("compared through a symlink instead of treating it as unowned")
	}
}

// A path with nothing at it has nothing to compare.
func TestUnchangedIsFalseForAMissingFile(t *testing.T) {
	if unchanged(filepath.Join(t.TempDir(), "nope"), []byte("")) {
		t.Error("a missing path compared equal")
	}
}

func modTime(t *testing.T, p string) time.Time {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.ModTime()
}

// Written is what cairn owns; Changed is what moved. A deploy needs the second,
// and until the writer tracked it there was no way to ask.
func TestChangedListsOnlyWhatMoved(t *testing.T) {
	out := t.TempDir()
	c := cfg(t, config.ConflictError)

	w := NewWriter(c, out)
	for _, p := range []string{"a.json", "b.json"} {
		if err := w.Write(p, []byte("one")); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Save(); err != nil {
		t.Fatal(err)
	}
	if got := w.Changed(); len(got) != 2 {
		t.Fatalf("first run Changed = %v, want both paths", got)
	}

	// Second run: one file's content moves, the other's does not.
	w2 := NewWriter(c, out)
	if err := w2.Write("a.json", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := w2.Write("b.json", []byte("two")); err != nil {
		t.Fatal(err)
	}
	if got := w2.Changed(); !slices.Equal(got, []string{"b.json"}) {
		t.Errorf("Changed = %v, want [b.json]", got)
	}
	if got := w2.Written(); !slices.Equal(got, []string{"a.json", "b.json"}) {
		t.Errorf("Written = %v, want both — cairn owns them either way", got)
	}
	if w2.Unchanged() != 1 {
		t.Errorf("Unchanged = %d, want 1", w2.Unchanged())
	}
}

// A protected path is written by nobody, so it never counts as changed. Claiming
// it would put another tool's file into a deployment's transfer list.
func TestChangedExcludesProtectedPaths(t *testing.T) {
	out := t.TempDir()
	w := NewWriter(cfg(t, config.ConflictError), out)
	if err := w.Write("dists/Release", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if got := w.Changed(); len(got) != 0 {
		t.Errorf("Changed = %v, want nothing", got)
	}
}
