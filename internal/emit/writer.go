// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Package emit writes cairn's output formats. Every write goes through Writer,
// which enforces path containment, the protect globs and the conflict policy.
// That single choke point is what keeps cairn from ever clobbering apt or yum
// repository metadata, or writing outside the directory it was given.
package emit

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
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
	prot []string        // paths a protect: glob kept this run from writing
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
//
// A protect: glob skips the write and reports no error. The glob is the operator
// declaring which paths another tool owns — apt's signed dists/, dnf's repodata/
// — so refusing to write there is exactly what was asked for, and failing the
// build over it would make protect: unusable for its only purpose. A protected
// path is never recorded as written: claiming it would let a later run overwrite
// the files protect: exists to shield, and Prune would delete them.
func (w *Writer) Write(relPath string, body []byte) error {
	if w.cfg.IsProtected(relPath) {
		w.prot = append(w.prot, relPath)
		return nil
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

// Protected lists the paths this run declined to write because a protect: glob
// covered them. The caller reports the count: a skip is silent by design, and
// without it an operator whose glob is wider than they meant sees no listing and
// no reason why.
func (w *Writer) Protected() []string { return w.prot }

// Prune deletes output from the previous run that this run did not write, and
// returns the paths it removed.
//
// Without this a removed file leaves its digest behind and, worse, a removed
// directory leaves a whole published listing for something that is gone — links
// and all. A generator that only ever adds is not maintaining a mirror.
//
// Only paths the previous manifest recorded are considered, so cairn can delete
// nothing it did not create. A missing or corrupt manifest prunes nothing, which
// is the safe failure: stale files are a nuisance, deleting someone's artifacts
// is not.
//
// Two configs sharing one output root would each prune the other's files. That
// is unsupported rather than guarded against — there is no way to tell that case
// apart from a directory that was legitimately removed.
// underScope reports whether a manifest path belongs to a scoped rebuild.
//
// An empty scope is the whole tree. Comparison is on path segments so that
// "docs" does not claim "docs-old": a prefix test on the raw string would
// prune a sibling directory's entire output.
func underScope(p, scope string) bool {
	if scope == "" || scope == "." {
		return true
	}
	rel := strings.TrimPrefix(p, "/")
	return rel == scope || strings.HasPrefix(rel, scope+"/")
}

// PruneScoped removes stale output from one subtree, leaving the rest of the
// manifest's claims alone.
//
// A scoped rebuild writes only part of the tree, so the unscoped Prune — which
// deletes everything the previous run owned and this one did not rewrite —
// would delete every listing outside the scope on the first watch event.
func (w *Writer) PruneScoped(scope string) ([]string, error) {
	return w.prune(scope)
}

func (w *Writer) Prune() ([]string, error) { return w.prune("") }

func (w *Writer) prune(scope string) ([]string, error) {
	wrote := make(map[string]bool, len(w.made))
	for _, p := range w.made {
		wrote[p] = true
	}

	var removed []string
	for p := range w.own {
		if wrote[p] || !underScope(p, scope) {
			continue
		}
		abs, err := containedPath(w.root, p)
		if err != nil {
			return removed, err
		}
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			return removed, fmt.Errorf("prune %s: %w", p, err)
		}
		removed = append(removed, p)
	}
	sort.Strings(removed)
	w.pruneEmptyDirs(removed)
	return removed, nil
}

// pruneEmptyDirs removes directories left holding nothing after a prune.
//
// Deepest first, so a nested pair collapses in one pass, and only when empty —
// os.Remove fails harmlessly on a directory that still holds an artifact, which
// is exactly the guard wanted here.
func (w *Writer) pruneEmptyDirs(removed []string) {
	dirs := make([]string, 0, len(removed))
	for _, p := range removed {
		if d := path.Dir(p); d != "." {
			dirs = append(dirs, d)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	for _, d := range dirs {
		abs, err := containedPath(w.root, d)
		if err != nil {
			continue
		}
		_ = os.Remove(abs)
	}
}

// Save records this run's output so the next run knows what it may replace.
func (w *Writer) Save() error { return w.save(w.made) }

// SaveScoped records a scoped rebuild: what it wrote, plus everything the
// previous run owned outside the scope. Saving only what a scoped run wrote
// would disown the rest of the tree, and the next run would refuse all of it
// as somebody else's files.
func (w *Writer) SaveScoped(scope string) error {
	seen := make(map[string]bool, len(w.made))
	all := make([]string, 0, len(w.own)+len(w.made))
	for _, p := range w.made {
		if !seen[p] {
			seen[p] = true
			all = append(all, p)
		}
	}
	outside := make([]string, 0, len(w.own))
	for p := range w.own {
		if !seen[p] && !underScope(p, scope) {
			outside = append(outside, p)
		}
	}
	sort.Strings(outside) // map order would churn the manifest between runs
	return w.save(append(all, outside...))
}

// SavePartial records this run's output together with everything the previous
// run claimed, for a build that died partway.
//
// The union matters. Saving only what a failed run managed to write would
// disown every file an earlier successful run had created but this one never
// reached, and the next build would refuse all of them as conflicts — a worse
// wedge than saving nothing. Stale entries are not a problem: the next run that
// completes prunes whatever it does not rewrite.
func (w *Writer) SavePartial() error {
	all := make([]string, 0, len(w.own)+len(w.made))
	seen := make(map[string]bool, len(w.own)+len(w.made))
	for _, p := range w.made {
		if !seen[p] {
			seen[p] = true
			all = append(all, p)
		}
	}
	prev := make([]string, 0, len(w.own))
	for p := range w.own {
		if !seen[p] {
			prev = append(prev, p)
		}
	}
	sort.Strings(prev) // map order would make the manifest churn between runs
	return w.save(append(all, prev...))
}

func (w *Writer) save(paths []string) error {
	b, err := json.Marshal(paths)
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
