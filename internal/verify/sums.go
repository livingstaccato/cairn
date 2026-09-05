// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package verify

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// The shape of a coreutils line: a 64-character hex digest, a two-byte
// separator, then the filename. "  " is text mode and " *" is binary mode;
// sha256sum writes both and accepts both, so a verifier that reads only cairn's
// own output would skip lines a client verifies successfully — reporting a file
// as unchecked when it is in fact fine.
const (
	digestLen       = 64
	sumsSeparator   = "  "
	binarySeparator = " *"
)

// checkSums re-hashes everything one SHA256SUMS lists.
//
// relSums is the file's path under the output directory, but the names inside it
// are not. A digest line is what a client resolves against the directory it
// fetched the file from, and what is served there is the artifact tree — cairn's
// indexed root. In a mirror the two directories are one; in a split build the
// artifacts are not under the output directory at all, and resolving there finds
// nothing and reports a tree of missing files that are sitting on disk.
//
// A protected SHA256SUMS is skipped whole. protect: is the operator saying
// another tool owns that subtree, so both the file and the names in it belong to
// a scheme cairn does not know — apt's dists/ has its own signed digests over
// its own paths.
func (v *verifier) checkSums(relSums string) error {
	if v.cfg.IsProtected(relSums) {
		return nil
	}
	abs := filepath.Join(v.out, filepath.FromSlash(relSums))
	// #nosec G304 -- abs is a path the walk of the output directory produced, and
	// the walk does not follow links; a SHA256SUMS that is itself a symlink is
	// reported as an orphan rather than read.
	b, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("read %s: %w", relSums, err)
	}
	dir := path.Dir(relSums)
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue // coreutils tolerates blank lines; warning about one is noise
		}
		sum, name, ok := parseSumsLine(line)
		if !ok {
			// Warned rather than counted. A line naming no checkable file cannot
			// be verified, and inventing either a finding or a clean result from
			// it would be a claim about a file nobody read. The operator gets the
			// line; the report stays about the tree.
			v.log.Warn("ignoring a line that is not a SHA256SUMS entry",
				"path", relSums, "line", line)
			continue
		}
		v.checkDigest(path.Join(dir, name), sum)
	}
	return nil
}

// checkDigest compares one published digest against the bytes on disk.
//
// Absent is reported as missing rather than modified: the two findings lead an
// operator to different places, one to whatever removed the file and the other
// to whatever rewrote it.
func (v *verifier) checkDigest(target, want string) {
	abs, err := containedPath(v.root, target)
	if err != nil {
		// A name climbing out of the tree names nothing cairn published. It is
		// reported and never resolved: dropping it would let a doctored
		// SHA256SUMS verify clean, and following it would hash a path the file
		// under inspection chose.
		v.missing[target] = true
		return
	}
	fi, err := os.Lstat(abs)
	if err != nil || !fi.Mode().IsRegular() {
		// Lstat, so a symlink standing where the artifact was counts as gone.
		// Hashing through it would publish the target's bytes as the artifact's,
		// which is the substitution a published digest exists to catch.
		v.missing[target] = true
		return
	}
	got, err := v.cache.Sum(abs, fi.Size(), fi.ModTime().Unix())
	if err != nil {
		// Unreadable is reported as missing on purpose. A file whose bytes nobody
		// read cannot be called intact, and the one outcome verify must never
		// produce is a clean report covering something it did not check.
		v.log.Warn("could not hash a file SHA256SUMS lists", "path", target, "err", err)
		v.missing[target] = true
		return
	}
	v.checked++
	if !strings.EqualFold(got, want) {
		v.modified[target] = true
	}
}

// parseSumsLine splits one coreutils line into digest and name.
//
// Strict about the digest and the separator, deliberately loose about the name:
// a filename in a mirror may hold spaces, and everything after the separator is
// the name by definition. "." and ".." are rejected because they name a
// directory rather than a file, and a directory would otherwise be reported as
// a missing artifact.
//
// Case is not significant. sha256sum accepts an uppercase digest, so rejecting
// one would skip a line a client verifies successfully and leave a file counted
// as unchecked when nothing is wrong with it.
func parseSumsLine(line string) (sum, name string, ok bool) {
	if len(line) < digestLen+len(sumsSeparator)+1 {
		return "", "", false
	}
	sum = line[:digestLen]
	sep := line[digestLen : digestLen+len(sumsSeparator)]
	name = line[digestLen+len(sumsSeparator):]
	if !isHex(sum) || (sep != sumsSeparator && sep != binarySeparator) {
		return "", "", false
	}
	if name == "." || name == ".." {
		return "", "", false
	}
	return sum, name, true
}

// isHex reports whether s is nothing but hex digits.
func isHex(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}
