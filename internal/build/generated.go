// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// What a listing leaves out, and why cairndex's own output is not content.
//
// One question, asked by two walks that have to agree about it: the
// per-directory walk reaches it through dropGenerated and the recursive one
// through walk.Tree. They disagreed once, and the recursive listing published
// the output directory and every generated file as though somebody had put them
// there.
package build

import (
	"path"

	"github.com/livingstaccato/cairndex/internal/config"
	"github.com/livingstaccato/cairndex/internal/model"
	"github.com/livingstaccato/cairndex/internal/walk"
)

// dropGenerated removes cairndex's own output from a listing.
//
// Required for the deployment that matters most: writing index files into the
// artifact tree itself, so a mirror is one directory that rsyncs whole and
// verifies in place with sha256sum -c. Without this the second run lists the
// first run's index.json, SHA256SUMS covers files that change on every run, and
// the build never reaches a fixed point.
//
// Excluding is safe for a path cairndex would actually write: if such a file
// already exists and cairndex did not create it, emit.Writer refuses the whole
// build, so there is no case where this hides someone's own file from its own
// listing. A protected path is the exception — cairndex writes nothing there, so a
// file of that name belongs to whoever put it there and must stay listed.
func (r *runner) dropGenerated(relDir string, entries []model.Entry, s config.Settings) []model.Entry {
	keep := r.treeFilter()
	out := entries[:0:0]
	for _, e := range entries {
		if keep(relDir, e) {
			out = append(out, e)
		}
	}
	return out
}

// treeFilter returns the rule for what cairndex's own output is, for the two walks
// that have to agree about it.
//
// One function because two callers must agree, the same reason OutRel is one
// function. The per-directory walk reaches it through dropGenerated and the
// recursive walk through walk.Tree, and they disagreed once: the exclusion was
// applied on the collect side only, so tree.json and the search index built from
// it published the output directory and every generated file as content, and a
// rebuild that changed nothing still rewrote tree.json.
//
// The generated-name set is built once and captured rather than rebuilt per
// entry: a package pool directory is thousands of entries and this is called for
// every one of them.
func (r *runner) treeFilter() walk.Filter {
	skip := r.generatedNames()
	return func(relDir string, e model.Entry) bool {
		if r.isOutputDir(relDir, e) {
			return false
		}
		// A directory can be generated too now (emit.SearchPageDir): the old
		// !e.IsDir guard predates that and would leave search/ walked and
		// listed like any real subdirectory.
		generated := skip[e.Name] &&
			!r.cfg.IsProtected(path.Join(relDir, e.Name))
		return !generated
	}
}

// isOutputDir reports whether this entry is the output directory, sitting
// inside the tree being indexed.
//
// Dropping it here covers both halves at once: entries is what the listing
// renders and what recurse descends into, so the directory is neither named nor
// walked. Leaving it listed would publish a link to a page that is never
// written, and walking it made every build index the previous build's output
// one level deeper.
//
// This is the same exclusion cairndex already applies to its own generated
// filenames, one level up: a directory cairndex fills is no more part of the tree
// it describes than a file cairndex wrote.
func (r *runner) isOutputDir(relDir string, e model.Entry) bool {
	if !e.IsDir || r.outRel == "" {
		return false
	}
	return path.Join(relDir, e.Name) == r.outRel
}

// generatedNames is every filename cairndex writes into one directory.
func (r *runner) generatedNames() map[string]bool {
	return GeneratedNames(r.cfg)
}
