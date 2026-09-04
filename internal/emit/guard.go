// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

// Package emit writes cairn's output formats. Every write goes through Write,
// which enforces path containment, the protect globs and the conflict policy.
// That single choke point is what keeps cairn from ever clobbering apt or yum
// repository metadata, or writing outside the directory it was given.
package emit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/livingstaccato/cairn/internal/config"
)

// Output permissions. These are deliberately looser than gosec's defaults:
// cairn writes a static site that a web server reads as a different user, so
// 0600 files and 0750 directories would produce a tree nginx cannot serve. The
// content is public by construction — it is a published index.
const (
	outDirMode  os.FileMode = 0o755
	outFileMode os.FileMode = 0o644
)

// Write places body at outRoot/relPath, creating parents.
func Write(cfg *config.Config, outRoot, relPath string, body []byte) error {
	if cfg.IsProtected(relPath) {
		return fmt.Errorf("refusing to write %s: path is protected by a protect: glob", relPath)
	}

	abs, err := containedPath(outRoot, relPath)
	if err != nil {
		return err
	}

	// Lstat, not Stat: a symlink sitting at the output path must count as a
	// conflict rather than being followed and silently overwriting whatever it
	// targets. Stat would also report a dangling link as absent and let the
	// write through.
	if _, err := os.Lstat(abs); err == nil {
		if cfg.OnConflict == config.ConflictSkip {
			return nil
		}
		return fmt.Errorf("refusing to write %s: path already exists "+
			"(set on_conflict: %s, or index_basename to something unused)",
			relPath, config.ConflictSkip)
	}

	// #nosec G301 -- see outDirMode: the served tree must be traversable by the
	// web server's user.
	if err := os.MkdirAll(filepath.Dir(abs), outDirMode); err != nil {
		return fmt.Errorf("create parents for %s: %w", relPath, err)
	}
	// #nosec G306 -- see outFileMode: a published index is world-readable by
	// design.
	if err := os.WriteFile(abs, body, outFileMode); err != nil {
		return fmt.Errorf("write %s: %w", relPath, err)
	}
	return nil
}

// containedPath joins relPath onto outRoot and verifies the result stays inside
// it. filepath.Join cleans "..", so a crafted relPath resolves quietly outside
// the root unless containment is checked explicitly.
//
// Only outRoot is symlink-resolved. The target usually does not exist yet —
// that is the point of writing it — so resolving it would fail for every new
// file. A symlink already standing at the target is caught by the Lstat check
// instead, which refuses rather than following it.
func containedPath(outRoot, relPath string) (string, error) {
	rootAbs, err := filepath.Abs(outRoot)
	if err != nil {
		return "", fmt.Errorf("resolve output root %s: %w", outRoot, err)
	}
	if resolved, err := filepath.EvalSymlinks(rootAbs); err == nil {
		rootAbs = resolved
	}

	abs := filepath.Join(rootAbs, filepath.FromSlash(relPath))
	rel, err := filepath.Rel(rootAbs, abs)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", relPath, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing to write %s: resolves outside the output root", relPath)
	}
	return abs, nil
}
