// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package watch

import (
	"log/slog"
	"path"
	"path/filepath"
	"strings"

	"github.com/livingstaccato/cairn/internal/build"
	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/walk"
)

// Filter decides which filesystem events a watcher acts on.
//
// The decision cairn cannot get wrong is its own output. When the index is
// written into the tree it indexes — the mirror deployment, one directory that
// rsyncs whole — every rebuild writes files inside the watched tree, and a
// watcher that treats those writes as changes rebuilds forever. The names it
// discards therefore come from the same set the build uses to keep its output
// out of its listings, so the two cannot drift apart.
type Filter struct {
	root      string
	cfg       *config.Config
	log       *slog.Logger
	generated map[string]bool
	// outRel is the output directory relative to the watched root, empty when
	// output does not land in a subtree of it.
	outRel string
}

// NewFilter builds the filter for one configuration.
func NewFilter(cfg *config.Config, rootDir, outDir string, log *slog.Logger) *Filter {
	f := &Filter{
		root:      rootDir,
		cfg:       cfg,
		log:       log,
		generated: build.GeneratedNames(cfg),
	}
	// A separate output directory inside the tree is skipped whole. Output that
	// lands in the tree itself has no subtree to skip, and the generated names
	// are what keep the rebuild from feeding itself.
	if rel, err := filepath.Rel(rootDir, outDir); err == nil {
		rel = filepath.ToSlash(rel)
		if rel != "." && !strings.HasPrefix(rel, "../") && rel != ".." {
			f.outRel = rel
		}
	}
	return f
}

// Rel converts an event's path to a path relative to the watched root, and
// reports false for anything outside it or ignored.
func (f *Filter) Rel(absPath string) (string, bool) {
	rel, err := filepath.Rel(f.root, absPath)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return rel, true
}

// Ignore reports whether an event on absPath should be discarded.
func (f *Filter) Ignore(absPath string) bool {
	rel, ok := f.Rel(absPath)
	if !ok {
		return true
	}
	if rel == "." {
		return false
	}
	if f.outRel != "" && Covers(f.outRel, rel) {
		return true
	}
	if f.generated[path.Base(rel)] {
		return true
	}
	return f.hidden(rel)
}

// hidden reports whether the path, or any directory above it, is one the build
// leaves out of its listings.
//
// Checked all the way up because hide: globs describe an entry, not a subtree:
// the default "**/.*" matches ".git" and not ".git/config". A build never
// descends past the first match, so neither may a watcher — a repository's own
// churn would otherwise rebuild the site on every object it writes.
func (f *Filter) hidden(rel string) bool {
	s := build.SettingsFor(f.cfg, f.root, path.Dir(rel), f.log)
	for p := rel; p != "." && p != "/"; p = path.Dir(p) {
		if walk.HiddenByGlob(s.Hide, p) {
			return true
		}
	}
	return false
}
