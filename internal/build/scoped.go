// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package build

import (
	"fmt"
	"log/slog"
	"path"
	"path/filepath"
	"strings"

	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/emit"
	"github.com/livingstaccato/cairn/internal/hash"
	"github.com/livingstaccato/cairn/internal/meta"
)

// Scope resolves which directory a rebuild has to start from when relDir
// changed.
//
// Not relDir itself. A recursive listing describes a whole subtree, so a change
// anywhere beneath one invalidates it, and the directory that owns it has to be
// the root of the rebuild. Walking up from the change to the highest such owner
// gives the smallest correct scope; with no recursive listing above it, that is
// the changed directory.
func Scope(cfg *config.Config, rootDir, relDir string, log *slog.Logger) string {
	scope := relDir
	for dir := relDir; ; dir = path.Dir(dir) {
		s := SettingsFor(cfg, rootDir, dir, log)
		if s.Recursive {
			scope = dir
		}
		if dir == "." || dir == "/" || dir == "" {
			break
		}
	}
	return scope
}

// RunScoped rebuilds one subtree and the listings above it that name it.
//
// The ancestors matter and are easy to miss: a parent's entry for a directory
// carries that directory's child count and modification time, so a file added
// three levels down changes what every listing above it should say. Each
// ancestor is re-emitted without recursing, since its other children have not
// moved.
func RunScoped(cfg *config.Config, rootDir, outDir string, log *slog.Logger, scope string) (*Result, error) {
	if scope == "" {
		scope = "."
	}
	r := &runner{
		cfg:    cfg,
		root:   rootDir,
		out:    outDir,
		log:    log,
		cache:  hash.NewCache(filepath.Join(outDir, hash.CacheFile)),
		writer: emit.NewWriter(cfg, outDir),
		result: &Result{},
	}
	r.warnAboutTheManifest()

	err := r.buildScoped(scope)
	if err != nil {
		if saveErr := r.writer.SavePartial(); saveErr != nil {
			r.log.Warn("could not record partial output; a retry may report conflicts",
				"path", emit.ManifestFile, "err", saveErr)
		}
	}
	r.result.Written = r.writer.Written()
	r.result.Protected = len(r.writer.Protected())
	r.result.Unchanged = r.writer.Unchanged()
	r.result.Changed = r.writer.Changed()
	return r.result, err
}

func (r *runner) buildScoped(scope string) error {
	if err := r.visit(scope); err != nil {
		return err
	}
	// Every directory between the scope and the root, refreshed but not
	// recursed: their listings name the scope, their subtrees are untouched.
	for _, dir := range ancestors(scope) {
		if err := r.refresh(dir); err != nil {
			return err
		}
	}
	r.saveCache()
	pruned, err := r.writer.PruneScoped(scope)
	if err != nil {
		return err
	}
	r.reportPruned(pruned)
	return r.writer.SaveScoped(scope)
}

// ancestors lists the directories above relDir, nearest first, ending at the
// root. The scope itself is not among them; visit has already rebuilt it.
func ancestors(relDir string) []string {
	if relDir == "." || relDir == "" {
		return nil
	}
	var out []string
	for dir := path.Dir(relDir); ; dir = path.Dir(dir) {
		out = append(out, dir)
		if dir == "." {
			break
		}
	}
	return out
}

// refresh re-emits one directory's own listings without descending into it.
//
// This is visit without the recursion: the directory's entries are re-read, so
// a child's new count and modification time reach the listing that names it,
// but the child's own listings are left alone.
func (r *runner) refresh(relDir string) error {
	absDir := filepath.Join(r.root, filepath.FromSlash(relDir))
	s := r.cfg.Resolve(relDir, r.dirOverride(absDir))

	entries, err := r.collect(relDir, absDir, s)
	if err != nil {
		return fmt.Errorf("refresh %s: %w", relDir, err)
	}

	prose, err := meta.Prose(absDir)
	if err != nil {
		return err
	}
	src, err := meta.Source(absDir)
	if err != nil {
		return err
	}
	if err := r.emitFor(relDir, r.cfg.IndexBasename, r.listing(relDir, entries), s, prose, src); err != nil {
		return err
	}
	if s.Recursive {
		return r.emitTree(relDir, s, prose, src)
	}
	return nil
}

// RelDirOf turns an absolute path that changed into the directory a rebuild
// should be scoped from, relative to the indexed root. It reports false for a
// path outside the root, which a watcher should ignore rather than act on.
func RelDirOf(rootDir, absPath string) (string, bool) {
	rel, err := filepath.Rel(rootDir, absPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	dir := path.Dir(filepath.ToSlash(rel))
	if dir == "" {
		dir = "."
	}
	return dir, true
}
