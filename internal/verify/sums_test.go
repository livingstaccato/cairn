// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package verify

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/livingstaccato/cairn/internal/emit"
	"github.com/livingstaccato/cairn/internal/hash"
)

func TestChangedBytesAreModified(t *testing.T) {
	f := mirror(t)
	f.file("pool/nginx.deb", "deb payload\n")
	f.file("pool/curl.deb", "curl payload\n")
	f.sums("pool", "curl.deb", "nginx.deb")
	f.file("pool/nginx.deb", "tampered payload\n")
	f.manifest("pool/SHA256SUMS")

	r := f.run()
	eq(t, "Modified", r.Modified, []string{"pool/nginx.deb"})
	eq(t, "Missing", r.Missing, nil)
	if r.Checked != 2 {
		t.Errorf("Checked = %d, want 2: both digests were recomputed", r.Checked)
	}
}

// The names in SHA256SUMS are relative to the directory it is served from, and
// what is served there is the artifact tree — the indexed root. In a split
// build the artifacts are not under out at all, so resolving against out finds
// nothing and reports a mirror of missing files that are in fact right there.
func TestSumsNamesResolveAgainstTheIndexedRoot(t *testing.T) {
	f := split(t)
	f.file("pool/nginx.deb", "deb payload\n")
	f.sums("pool", "nginx.deb")
	f.manifest("pool/SHA256SUMS")

	r := f.run()
	if !r.OK() {
		t.Fatalf("split build reported dirty: %+v", r)
	}
	if r.Checked != 1 {
		t.Errorf("Checked = %d, want 1", r.Checked)
	}
}

// A digest line is relative to its own directory, not to the top of the tree.
// The decoy at the root has different bytes, so an implementation that joins
// the name onto the wrong directory reports a modification that is not there.
func TestSumsNamesResolveAgainstTheirOwnDirectory(t *testing.T) {
	f := mirror(t)
	f.file("nginx.deb", "decoy payload\n")
	f.file("pool/nginx.deb", "deb payload\n")
	f.sums("pool", "nginx.deb")
	f.manifest("pool/SHA256SUMS")

	if r := f.run(); !r.OK() {
		t.Fatalf("digest resolved against the wrong directory: %+v", r)
	}
}

// Absent is a different finding from altered, and the operator's next move
// differs with it: one file was deleted, the other was rewritten.
func TestFileListedInSumsButAbsentIsMissing(t *testing.T) {
	f := mirror(t)
	f.file("pool/nginx.deb", "deb payload\n")
	f.sums("pool", "nginx.deb")
	if err := os.Remove(filepath.Join(f.root, "pool", "nginx.deb")); err != nil {
		t.Fatal(err)
	}
	f.manifest("pool/SHA256SUMS")

	r := f.run()
	eq(t, "Missing", r.Missing, []string{"pool/nginx.deb"})
	eq(t, "Modified", r.Modified, nil)
	if r.Checked != 0 {
		t.Errorf("Checked = %d: nothing was hashed", r.Checked)
	}
}

// Hashing through a link would report the target's bytes as the artifact's,
// which is the one substitution a published digest exists to catch.
func TestSymlinkWhereSumsExpectsAFileIsMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privilege on windows")
	}
	f := mirror(t)
	f.file("pool/nginx.deb", "deb payload\n")
	f.file("other.deb", "deb payload\n")
	f.sums("pool", "nginx.deb")
	p := filepath.Join(f.root, "pool", "nginx.deb")
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(f.root, "other.deb"), p); err != nil {
		t.Fatal(err)
	}
	f.manifest("pool/SHA256SUMS")

	r := f.run()
	eq(t, "Missing", r.Missing, []string{"pool/nginx.deb"})
	if r.Checked != 0 {
		t.Error("a link must not be hashed through, even when the bytes match")
	}
}

// A SHA256SUMS wearing a symlink is not cairn's file and is not read. Following
// it would hand an arbitrary path to the parser.
func TestSymlinkNamedSumsIsNotParsed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privilege on windows")
	}
	f := mirror(t)
	// The linked file is a SHA256SUMS that would verify, so a verifier that
	// followed the link reports a digest it checked rather than nothing.
	f.file("x", "x")
	f.file("elsewhere", digest("x")+"  x\n")
	if err := os.Symlink(filepath.Join(f.root, "elsewhere"),
		filepath.Join(f.out, emit.SumsFile)); err != nil {
		t.Fatal(err)
	}
	f.manifest()

	r := f.run()
	if r.Checked != 0 {
		t.Errorf("Checked = %d: a linked SHA256SUMS must not be followed", r.Checked)
	}
	eq(t, "Orphaned", r.Orphaned, []string{emit.SumsFile})
}

// A line that names no checkable file cannot be verified, and pretending
// otherwise either invents a finding or hides one. Each is warned about and
// skipped; the operator sees the line, the report stays about the tree.
func TestMalformedSumsLinesAreWarnedAndSkipped(t *testing.T) {
	body := digest("x")
	cases := []struct {
		name, line string
	}{
		{"no separator", body + " onespace.txt"},
		{"digest only", body},
		{"short digest", body[:63] + "  short.txt"},
		{"long digest", body + "0  long.txt"},
		{"not hex", strings.Repeat("z", 64) + "  nothex.txt"},
		{"empty name", body + "  "},
		{"name is a directory", body + "  ."},
		{"leading junk", "  " + body + "  lead.txt"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := mirror(t)
			f.file("onespace.txt", "x")
			f.rawSums(".", c.line+"\n")
			f.manifest(emit.SumsFile)

			r := f.run()
			if !r.OK() || r.Checked != 0 {
				t.Errorf("line %q produced %+v, want an untouched report", c.line, r)
			}
			if !strings.Contains(f.logs.String(), "SHA256SUMS") {
				t.Error("a line that cannot be checked must be reported to the operator")
			}
		})
	}
}

// Blank lines are not malformed. coreutils tolerates them and so must this,
// silently, or an operator who adds one gets a warning about nothing.
func TestBlankSumsLinesAreSilent(t *testing.T) {
	f := mirror(t)
	f.file("a.txt", "a")
	f.rawSums(".", "\n"+digest("a")+"  a.txt\n\n")
	f.manifest(emit.SumsFile)

	r := f.run()
	if !r.OK() || r.Checked != 1 {
		t.Fatalf("got %+v, want one file checked and nothing found", r)
	}
	if strings.Contains(f.logs.String(), "level=WARN") {
		t.Errorf("blank lines warned: %s", f.logs.String())
	}
}

// sha256sum accepts an uppercase digest, so skipping the line would leave a
// file counted as unchecked when a client verifies it without complaint.
func TestUppercaseDigestIsAccepted(t *testing.T) {
	f := mirror(t)
	f.file("a.iso", "iso bytes\n")
	f.rawSums(".", strings.ToUpper(digest("iso bytes\n"))+"  a.iso\n")
	f.manifest(emit.SumsFile)

	if r := f.run(); !r.OK() || r.Checked != 1 {
		t.Fatalf("got %+v, want the uppercase line checked", r)
	}
}

// The binary-mode marker is what `sha256sum -b` writes, and a mirror's
// SHA256SUMS is not always cairn's own.
func TestBinaryModeMarkerIsAccepted(t *testing.T) {
	f := mirror(t)
	f.file("a.iso", "iso bytes\n")
	f.rawSums(".", digest("iso bytes\n")+" *a.iso\n")
	f.manifest(emit.SumsFile)

	if r := f.run(); !r.OK() || r.Checked != 1 {
		t.Fatalf("got %+v, want the binary-mode line checked", r)
	}
}

// A name climbing out of the tree names nothing cairn published. It is never
// resolved, and it is reported rather than dropped: a digest covering a file
// that is not there is not a verified mirror.
func TestSumsNameEscapingTheTreeIsMissing(t *testing.T) {
	f := split(t)
	f.file("pool/real.deb", "deb\n")
	// A real file with a matching digest is waiting where the name points, so a
	// verifier that resolved it would report a checked file and a clean tree.
	outside := filepath.Join(f.root, "..", "escape.deb")
	if err := os.WriteFile(outside, []byte("deb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.rawSums("pool", digest("deb\n")+"  ../../escape.deb\n")
	f.manifest("pool/SHA256SUMS")

	r := f.run()
	if len(r.Missing) != 1 || r.Checked != 0 {
		t.Fatalf("got %+v, want one unresolvable name and nothing hashed", r)
	}
	if r.OK() {
		t.Error("an unresolvable digest line is not a clean mirror")
	}
}

// A stale SHA256SUMS nobody claims is still served, and a client running
// sha256sum -c against it still fails. Both findings are real and both are
// reported.
func TestOrphanedSumsFileIsStillChecked(t *testing.T) {
	f := mirror(t)
	f.file("old.deb", "current bytes\n")
	f.rawSums(".", digest("previous bytes\n")+"  old.deb\n")
	f.manifest()

	r := f.run()
	eq(t, "Orphaned", r.Orphaned, []string{emit.SumsFile})
	eq(t, "Modified", r.Modified, []string{"old.deb"})
}

// The hash cache answers from (path, size, mtime), which is exactly the wrong
// oracle for a tamper check: a same-size edit with the mtime put back would
// verify clean off a value nobody recomputed. Verification reads the bytes.
func TestOnDiskHashCacheIsNotTrusted(t *testing.T) {
	f := mirror(t)
	f.file("a.iso", "original\n")
	f.sums(".", "a.iso")

	p := filepath.Join(f.root, "a.iso")
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}
	// Keyed by the path verification resolves to, symlinks and all, so the entry
	// is one a verifier reading this cache would actually hit.
	f.outFile(hash.CacheFile, cacheJSON(t, resolve(t, p), fi.Size(), fi.ModTime(), digest("original\n")))
	f.manifest(emit.SumsFile)

	if r := f.run(); len(r.Modified) != 1 {
		t.Fatalf("got %+v, want the same-size edit caught", r)
	}
}

func resolve(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func cacheJSON(t *testing.T, absPath string, size int64, mod time.Time, sum string) string {
	t.Helper()
	b, err := json.Marshal(map[string]map[string]any{
		absPath: {"size": size, "mod_unix": mod.Unix(), "sum": sum},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Two SHA256SUMS files, one report: the count is files hashed, not lines read.
func TestCheckedCountsEveryRehash(t *testing.T) {
	f := mirror(t)
	f.file("a/x.bin", "x")
	f.file("b/y.bin", "y")
	f.sums("a", "x.bin")
	f.sums("b", "y.bin")
	f.manifest("a/SHA256SUMS", "b/SHA256SUMS")

	if r := f.run(); r.Checked != 2 {
		t.Errorf("Checked = %d, want 2", r.Checked)
	}
}
