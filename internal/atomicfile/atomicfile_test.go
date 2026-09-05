// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Same length, different bytes. A size check alone is a cheap first pass, not
// the answer.
func TestSameComparesContentNotLength(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(abs, []byte("aaaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	if Same(abs, []byte("bbbb")) {
		t.Error("two different four-byte files compared equal")
	}
	if !Same(abs, []byte("aaaa")) {
		t.Error("identical content compared unequal")
	}
}

// A symlink where cairn writes is something someone else put there. Comparing
// through it would read the target's bytes and then leave the link standing —
// the one outcome this package exists to prevent.
//
// Constructed so the size check cannot answer it by accident: a symlink's own
// size is the length of the path it holds, so the target's content is made
// equal to that path. Both the length and the bytes then match, and only the
// regular-file guard can tell the difference.
func TestSameRefusesToCompareThroughASymlink(t *testing.T) {
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
	if Same(link, body) {
		t.Error("compared through a symlink instead of treating it as unowned")
	}
}

// A path with nothing at it has nothing to compare.
func TestSameIsFalseForAMissingFile(t *testing.T) {
	if Same(filepath.Join(t.TempDir(), "nope"), []byte("")) {
		t.Error("a missing path compared equal")
	}
}

// The point of the package: the new bytes are at the path afterwards.
func TestWriteReplacesContent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	wrote, err := Write(p, []byte("new"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if !wrote {
		t.Error("reported no write for changed content")
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("file holds %q, want %q", got, "new")
	}
}

// A file already holding what it is asked to hold is left entirely alone —
// mtime included, which is what stops a mirror rebuilding itself forever.
func TestWriteSkipsIdenticalContent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}

	wrote, err := Write(p, []byte("same"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if wrote {
		t.Error("rewrote a file that already held the right bytes")
	}

	after, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("mtime moved on a write that should not have happened")
	}
}

// The mode the caller asked for is the mode the file ends up with, not
// CreateTemp's 0600. It matters because the staged file starts private and the
// target has to be readable by whatever serves it.
func TestWriteAppliesTheRequestedMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows has no permission bits; every writable file reports 0666")
	}
	if os.Getuid() == 0 {
		t.Skip("root's umask and permissions make this meaningless")
	}
	p := filepath.Join(t.TempDir(), "f")
	if _, err := Write(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o644 {
		t.Errorf("mode = %v, want %v", got, os.FileMode(0o644))
	}
}

// A new file is created, not just an existing one replaced.
func TestWriteCreatesAMissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "new")
	if _, err := Write(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("file holds %q, want %q", got, "hello")
	}
}

// Nothing is left behind when the write cannot be staged. A directory that
// cannot be written to is the ordinary shape of that failure.
//
// The cleanup property itself is asserted portably by the rename test above;
// what this adds is the earlier failure point, where the staged file does not
// exist yet and the error has to be reported rather than swallowed.
func TestWriteLeavesNothingBehindOnFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod on windows toggles the read-only attribute, which does not " +
			"stop a file being created in a directory")
	}
	if os.Getuid() == 0 {
		t.Skip("root writes to a 0500 directory regardless")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if _, err := Write(filepath.Join(dir, "f"), []byte("x"), 0o644); err == nil {
		t.Fatal("no error writing into an unwritable directory")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		t.Errorf("left %d file(s) behind: %v", len(ents), ents)
	}
}

// The replacement is a rename, so a symlink at the path is replaced by a real
// file rather than written through to whatever it pointed at.
func TestWriteReplacesASymlinkRatherThanFollowingIt(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("target content"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := Write(link, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if !fi.Mode().IsRegular() {
		t.Errorf("path is still a %v, want a regular file", fi.Mode().Type())
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "target content" {
		t.Errorf("wrote through the link: target holds %q", got)
	}
}

// The staged file is cleaned up even when the rename is what failed. Anything
// else litters the output directory with dot-files nobody claims — and in
// cairn's case, files a later run would report as output it does not own.
func TestWriteRemovesTheStagedFileWhenTheRenameFails(t *testing.T) {
	dir := t.TempDir()
	orig := rename
	rename = func(_, _ string) error { return errors.New("no") }
	t.Cleanup(func() { rename = orig })

	if _, err := Write(filepath.Join(dir, "f"), []byte("x"), 0o644); err == nil {
		t.Fatal("a failed rename must be an error")
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		t.Errorf("left %d file(s) behind: %v", len(ents), ents)
	}
}

// A write that cannot reach the disk is an error, not a silent partial file.
func TestWriteAllReportsAFailedWrite(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "closed")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writeAll(f, []byte("x"), 0o644); err == nil {
		t.Error("writing to a closed file must be an error")
	}
}
