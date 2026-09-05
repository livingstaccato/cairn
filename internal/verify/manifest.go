// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package verify

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/livingstaccato/cairn/internal/emit"
)

// loadManifest reads what cairn claims to own under outDir.
//
// A missing manifest is not an error and not an empty tree — it is cairn
// claiming nothing here. Nothing can then be reported missing, and every file
// wearing a generated name becomes an orphan, which is the honest reading: those
// files are being served and cairn cannot account for one of them. It is also
// the answer that agrees with what happens next, since the following build will
// refuse each of them as a conflict for the same reason.
//
// A manifest that exists and does not parse is a different case and is fatal.
// emit.NewWriter tolerates one because the cost there is a conflict error an
// operator resolves; here the cost is a report that calls every file in the tree
// an orphan, which is a lie about the tree rather than a finding about it.
func loadManifest(outDir string) (map[string]bool, error) {
	p := filepath.Join(outDir, emit.ManifestFile)
	// #nosec G304 -- composed from the output directory the operator named and a
	// fixed filename, the same path emit.Writer reads.
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	var claims []string
	if err := json.Unmarshal(b, &claims); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	set := make(map[string]bool, len(claims))
	for _, c := range claims {
		set[c] = true
	}
	return set, nil
}

// checkMissing reports every claimed path the output tree no longer holds.
//
// Lstat and IsRegular, never Stat: cairn writes regular files, so a symlink or a
// directory standing at a claimed path means the file cairn wrote is gone,
// whatever now answers for it. Following the link would report the target's
// existence as the artifact's, which is the substitution the whole ownership
// record exists to make visible.
func (v *verifier) checkMissing() {
	for claim := range v.claimed {
		abs, err := containedPath(v.out, claim)
		if err != nil {
			// A claim resolving outside the output root is one emit.Write could
			// never have produced, so the manifest has been edited or corrupted.
			// It is reported as missing and never resolved: the alternative is a
			// verifier that stats an attacker-chosen path and then calls the
			// mirror clean.
			v.missing[claim] = true
			continue
		}
		if fi, err := os.Lstat(abs); err != nil || !fi.Mode().IsRegular() {
			v.missing[claim] = true
		}
	}
}

// containedPath joins a slash-separated relative path onto a root and refuses a
// result outside it.
//
// filepath.Join cleans "..", so a crafted path resolves quietly outside the root
// unless containment is checked afterwards. This mirrors emit's guard on the
// write side; the read side needs it just as much, because every path it is
// given comes from a file on disk that cairn did not necessarily write.
//
// Only the root is symlink-resolved. Resolving the target would follow a link
// standing at the path, which is the one thing callers here must decide about
// themselves.
func containedPath(root, rel string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", root, err)
	}
	if resolved, err := filepath.EvalSymlinks(rootAbs); err == nil {
		rootAbs = resolved
	}
	abs := filepath.Join(rootAbs, filepath.FromSlash(rel))
	back, err := filepath.Rel(rootAbs, abs)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", rel, err)
	}
	if back == ".." || strings.HasPrefix(back, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s resolves outside %s", rel, root)
	}
	return abs, nil
}
