// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

// Package emit writes cairn's output formats. Every write goes through Writer,
// which enforces path containment, the protect globs and the conflict policy.
// That single choke point is what keeps cairn from ever clobbering apt or yum
// repository metadata, or writing outside the directory it was given.
package emit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/livingstaccato/cairn/internal/config"
)

// ManifestFile records the paths cairn generated, so a later run can tell its
// own output apart from content that was already there.
const ManifestFile = ".cairn-manifest.json"

// Output permissions. These are deliberately looser than gosec's defaults:
// cairn writes a static site that a web server reads as a different user, so
// 0600 files and 0750 directories would produce a tree nginx cannot serve. The
// content is public by construction — it is a published index.
const (
	outDirMode  os.FileMode = 0o755
	outFileMode os.FileMode = 0o644
)

// Writer places output files under one root.
//
// It carries a manifest because a generator that cannot be run twice is not
// finished. Without one, the second build sees the first build's index.json and
// reports a conflict; with one, cairn overwrites what it previously wrote and
// still refuses to touch anything it did not.
type Writer struct {
	cfg  *config.Config
	root string
	own  map[string]bool // paths this run may overwrite, from the previous run
	made []string        // paths written by this run
}

// NewWriter loads the previous run's manifest, if any. A missing or unreadable
// manifest yields an empty one: the cost is a conflict error the operator can
// resolve, never a silent overwrite.
func NewWriter(cfg *config.Config, outRoot string) *Writer {
	w := &Writer{cfg: cfg, root: outRoot, own: map[string]bool{}}
	// #nosec G304 -- composed from the configured output directory and a fixed
	// filename.
	b, err := os.ReadFile(filepath.Join(outRoot, ManifestFile))
	if err != nil {
		return w
	}
	var prev []string
	if err := json.Unmarshal(b, &prev); err != nil {
		return w
	}
	for _, p := range prev {
		w.own[p] = true
	}
	return w
}

// Write places body at root/relPath, creating parents.
func (w *Writer) Write(relPath string, body []byte) error {
	if w.cfg.IsProtected(relPath) {
		return fmt.Errorf("refusing to write %s: path is protected by a protect: glob", relPath)
	}

	abs, err := containedPath(w.root, relPath)
	if err != nil {
		return err
	}

	if err := w.checkConflict(relPath, abs); err != nil {
		return err
	}
	if w.skipped(relPath, abs) {
		return nil
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
	w.made = append(w.made, relPath)
	return nil
}

// checkConflict reports an error when something cairn does not own already
// occupies the path and the policy is to fail.
//
// Lstat, not Stat: a symlink sitting at the output path must count as a conflict
// rather than being followed and silently overwriting whatever it targets. Stat
// would also report a dangling link as absent and let the write through.
func (w *Writer) checkConflict(relPath, abs string) error {
	if !exists(abs) {
		return nil
	}
	if w.own[relPath] {
		return nil // cairn wrote it last run; overwriting is the whole point
	}
	if w.cfg.OnConflict == config.ConflictSkip {
		return nil
	}
	return fmt.Errorf("refusing to write %s: path already exists and cairn did not create it "+
		"(set on_conflict: %s, or index_basename to something unused)",
		relPath, config.ConflictSkip)
}

// skipped reports whether an existing, unowned path should be left alone.
func (w *Writer) skipped(relPath, abs string) bool {
	return exists(abs) && !w.own[relPath] && w.cfg.OnConflict == config.ConflictSkip
}

// exists reports whether anything occupies p — including a symlink, dangling or
// not. Lstat rather than Stat is the whole point: a link standing at an output
// path is something, and following it would overwrite its target.
func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// Written returns the paths this run produced.
func (w *Writer) Written() []string { return w.made }

// Save records this run's output so the next run knows what it may replace.
func (w *Writer) Save() error {
	b, err := json.Marshal(w.made)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	abs := filepath.Join(w.root, ManifestFile)
	if err := os.MkdirAll(w.root, outDirMode); err != nil {
		return fmt.Errorf("create output root: %w", err)
	}
	if err := os.WriteFile(abs, b, outFileMode); err != nil {
		return fmt.Errorf("write manifest %s: %w", abs, err)
	}
	return nil
}

// containedPath joins relPath onto outRoot and verifies the result stays inside
// it. filepath.Join cleans "..", so a crafted relPath resolves quietly outside
// the root unless containment is checked explicitly.
//
// Only outRoot is symlink-resolved. The target usually does not exist yet —
// that is the point of writing it — so resolving it would fail for every new
// file. A symlink already standing at the target is caught by the conflict
// check instead, which refuses rather than following it.
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
