// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Package atomicfile replaces a file's contents without leaving a moment where
// the path holds neither the old bytes nor the new ones.
//
// It exists for cairn's two sidecar files — the manifest of what cairn owns and
// the hash cache. Both are whole-file rewrites of state a later run depends on,
// and os.WriteFile truncates before it writes: a process killed in that window
// leaves a half-written JSON object. For the manifest that is not a lost
// optimization but a wedged tree, because a manifest that will not parse means
// cairn claims nothing, refuses every path it wrote last time as somebody
// else's, and prunes none of it.
//
// The generated listings deliberately do not go through here. They are written
// by the thousand and a rename per file is a cost paid on every build to guard
// a page that the next build rewrites anyway — and the manifest records their
// digests, so `cairn check` names a truncated one rather than leaving it to be
// found in a browser.
package atomicfile

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

// rename is os.Rename, indirected so a test can reach the one failure this
// package has no other way to produce: the last step failing after every
// earlier one succeeded, with a fully written temp file left standing.
var rename = os.Rename

// Write puts body at path and reports whether it had to.
//
// A path already holding exactly body is left alone, mtime included. That is
// the difference between a rebuild that touches nothing and one that rewrites
// a 23 MB manifest to say what it already said — and in a mirror, where the
// indexed tree and the output are the same directory, a moved mtime is a change
// the parent's listing records and a watcher reacts to.
func Write(path string, body []byte, mode os.FileMode) (bool, error) {
	if Same(path, body) {
		return false, nil
	}

	dir := filepath.Dir(path)
	// Same directory as the target, because rename is only atomic within one
	// filesystem: a temp file under TMPDIR can land on a different mount and
	// the rename then fails, or silently degrades to a copy.
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp")
	if err != nil {
		return false, fmt.Errorf("stage %s: %w", path, err)
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }() // no-op once the rename has consumed it

	if err := writeAll(f, body, mode); err != nil {
		return false, fmt.Errorf("stage %s: %w", path, err)
	}
	if err := rename(tmp, path); err != nil {
		return false, fmt.Errorf("replace %s: %w", path, err)
	}
	return true, nil
}

// writeAll fills the staged file and closes it.
//
// Sync before close is the point of the exercise. A rename is ordered against
// the data only if the data has reached the disk; without it a crash shortly
// after a build can leave the new name pointing at zero bytes, which is the
// corrupt manifest this package exists to prevent, arrived at by a longer road.
func writeAll(f *os.File, body []byte, mode os.FileMode) error {
	defer func() { _ = f.Close() }() // Close after a successful Close is harmless
	if _, err := f.Write(body); err != nil {
		return err
	}
	// CreateTemp makes the file 0600. The caller's mode is what the file has to
	// end up with, and chmod before the rename means the file is never visible
	// at the target path with the wrong permissions.
	if err := f.Chmod(mode); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return f.Close()
}

// Same reports whether path already holds exactly body.
//
// Only a regular file qualifies. A symlink standing where cairn writes is
// something someone else put there: reading through it would compare the
// target's bytes, conclude nothing needs doing, and leave the link in place.
func Same(path string, body []byte) bool {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() != int64(len(body)) {
		return false
	}
	// #nosec G304 -- the caller composes path; this package only ever reads
	// back the file it is about to replace.
	have, err := os.ReadFile(path)
	return err == nil && bytes.Equal(have, body)
}
