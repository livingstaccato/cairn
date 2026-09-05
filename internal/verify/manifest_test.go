// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package verify

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestClaimedPathAbsentFromDiskIsMissing(t *testing.T) {
	f := mirror(t)
	f.outFile("docs/index.json", "{}")
	f.manifest("docs/index.json", "docs/index.csv", "gone/index.json")

	r := f.run()
	eq(t, "Missing", r.Missing, []string{"docs/index.csv", "gone/index.json"})
}

// A symlink standing where cairn wrote a regular file means the file cairn
// wrote is not there any more, whatever the link points at. Reporting it as
// present would make verify agree with a tree whose served bytes now come from
// somewhere else entirely.
func TestSymlinkAtClaimedPathIsMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privilege on windows")
	}
	f := mirror(t)
	f.file("real.json", `{"real":true}`)
	link := filepath.Join(f.out, "index.json")
	if err := os.Symlink(filepath.Join(f.root, "real.json"), link); err != nil {
		t.Fatal(err)
	}
	f.manifest("index.json")

	r := f.run()
	eq(t, "Missing", r.Missing, []string{"index.json"})
}

// A directory standing at a claimed path is the same finding: the file is gone.
func TestDirectoryAtClaimedPathIsMissing(t *testing.T) {
	f := mirror(t)
	if err := os.MkdirAll(filepath.Join(f.out, "index.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.manifest("index.json")

	eq(t, "Missing", f.run().Missing, []string{"index.json"})
}

// A manifest entry that resolves outside the output root is one emit.Write
// could never have produced, so the manifest has been edited or corrupted. It
// is reported as missing and never followed: the alternative is a report that
// stats an attacker-chosen path and then calls the mirror clean.
func TestManifestPathEscapingTheOutRootIsMissing(t *testing.T) {
	f := split(t)
	outside := filepath.Join(f.root, "outside.json")
	if err := os.WriteFile(outside, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.manifest("../" + filepath.Base(f.root) + "/outside.json")

	r := f.run()
	if len(r.Missing) != 1 {
		t.Fatalf("Missing = %v, want the escaping entry", r.Missing)
	}
	if r.OK() {
		t.Error("a manifest pointing outside the tree is not a clean mirror")
	}
}

// Absolute entries are the same class of tampering on every platform.
func TestAbsoluteManifestPathIsMissing(t *testing.T) {
	f := mirror(t)
	f.file("real.json", "{}")
	f.manifest(filepath.ToSlash(filepath.Join(f.root, "real.json")))

	if r := f.run(); len(r.Missing) != 1 {
		t.Errorf("Missing = %v, want the absolute entry", r.Missing)
	}
}

// The manifest is a JSON array of strings. Anything else is not a manifest, and
// guessing at it would claim paths nothing ever wrote.
func TestManifestOfWrongShapeIsAnError(t *testing.T) {
	for _, body := range []string{`{"paths":["a"]}`, `["a", 7]`, `null-ish`} {
		f := mirror(t)
		f.outFile(".cairn-manifest.json", body)
		if err := f.runErr(); err == nil {
			t.Errorf("manifest %q verified as clean", body)
		}
	}
}

// An empty array is a real manifest: it says a run completed and wrote nothing.
func TestEmptyManifestIsValid(t *testing.T) {
	f := mirror(t)
	f.manifest()
	if err := f.runErr(); err != nil {
		t.Fatalf("an empty manifest is a valid one: %v", err)
	}
}

// A directory standing where the manifest belongs is not a missing manifest.
// Reading it fails for a reason that is not absence, and treating every read
// failure as "cairn claims nothing" would report the whole tree as orphaned
// because of a filesystem accident.
func TestUnreadableManifestPathIsAnError(t *testing.T) {
	f := mirror(t)
	if err := os.MkdirAll(filepath.Join(f.out, ".cairn-manifest.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := f.runErr(); err == nil {
		t.Fatal("a manifest that cannot be read must not verify as clean")
	}
}
