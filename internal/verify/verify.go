// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Package verify answers two questions about a published tree: is what cairn
// recorded still intact, and is anything here wearing cairn's output names that
// cairn does not own.
//
// Only cairn can answer the second one. A client with sha256sum -c can confirm
// the artifacts it was told about; nothing but the manifest knows which files in
// the tree cairn actually wrote, so nothing else can tell a current index from
// one left behind when index_basename or outputs: changed. Stale output is the
// dangerous kind: it is served, it looks authoritative, and it describes a
// directory as it was.
//
// Nothing here writes. Verification that repairs is verification an operator
// cannot run on a mirror they are unsure about.
package verify

import (
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"sort"

	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/emit"
	"github.com/livingstaccato/cairn/internal/hash"
)

// Report is what one verification found.
//
// The three lists are separate because the operator's next move differs with
// each: restore, investigate, delete. Paths are relative and slash-separated,
// against the root that holds the thing named — cairn's own output against the
// output directory, an artifact named by SHA256SUMS against the indexed root.
// In a mirror, which is the deployment this was written for, those are one
// directory and the distinction does not arise.
type Report struct {
	Missing  []string // manifest claims it, disk does not have it
	Modified []string // SHA256SUMS lists a digest that no longer matches
	Orphaned []string // a file that looks like cairn output but no manifest claims it
	Checked  int      // files whose digest was actually recomputed
}

// OK reports whether the mirror is intact.
//
// Checked is deliberately not part of the answer: a tree with no SHA256SUMS in
// it has nothing to re-hash and is still intact. Requiring a digest count here
// would report every unhashed mirror as a failure.
func (r *Report) OK() bool {
	return len(r.Missing) == 0 && len(r.Modified) == 0 && len(r.Orphaned) == 0
}

// verifier carries one run's inputs and the findings as they accumulate. The
// three sets dedupe rather than append: a file can be reached both through the
// manifest and through a digest line, and an operator should see it once.
type verifier struct {
	cfg     *config.Config
	root    string
	out     string
	log     *slog.Logger
	claimed map[string]bool
	names   map[string]bool
	cache   *hash.Cache

	missing  map[string]bool
	modified map[string]bool
	orphaned map[string]bool
	checked  int
}

// Run verifies the output under outDir against what cairn recorded.
//
// rootDir and outDir are separate arguments because SHA256SUMS names resolve
// against the first and the manifest's claims against the second; passing one
// value for both is the mirror deployment, not a simplification available in
// general.
//
// An error means the verification could not be performed — an unreadable tree,
// a manifest that is not a manifest. A tree with findings in it is a successful
// run reporting them, which is why the caller checks Report.OK rather than err.
func Run(cfg *config.Config, rootDir, outDir string, log *slog.Logger) (*Report, error) {
	claimed, err := loadManifest(outDir)
	if err != nil {
		return nil, err
	}
	v := &verifier{
		cfg: cfg, root: rootDir, out: outDir, log: log,
		claimed: claimed,
		names:   generatedNames(cfg),
		// An empty cache with nowhere to save to. The on-disk cache answers from
		// (path, size, mtime), which is precisely the wrong oracle for a tamper
		// check — a same-size edit with the mtime restored would verify clean off
		// a digest nobody recomputed. Every digest in a verify run is read from
		// the bytes, and nothing is written back.
		cache:    hash.NewCache(""),
		missing:  map[string]bool{},
		modified: map[string]bool{},
		orphaned: map[string]bool{},
	}

	v.checkMissing()
	if err := v.walkOut(); err != nil {
		return nil, err
	}
	return v.report(), nil
}

// walkOut makes one pass over the output tree, asking both questions of every
// file it meets. One pass rather than two because on a mirror holding a hundred
// thousand artifacts the walk is the expensive part, and the two checks need
// exactly the same information about each entry.
func (v *verifier) walkOut() error {
	return filepath.WalkDir(v.out, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// Not skipped. A directory verify cannot read is one whose orphans it
			// cannot see, and reporting a clean tree on the strength of a walk that
			// did not finish is the failure mode this command exists to avoid.
			return fmt.Errorf("verify %s: %w", p, err)
		}
		if d.IsDir() {
			return nil
		}
		rel := filepath.ToSlash(mustRel(v.out, p))
		v.checkOrphan(rel)
		if d.Name() == emit.SumsFile && d.Type().IsRegular() {
			return v.checkSums(rel)
		}
		return nil
	})
}

// mustRel is filepath.Rel for the one case where it cannot fail: p came from a
// walk rooted at base, so it is under base by construction. The fallback keeps
// a path rather than an empty string if that ever stops being true.
func mustRel(base, p string) string {
	rel, err := filepath.Rel(base, p)
	if err != nil {
		return p
	}
	return rel
}

// report freezes the findings in a deterministic order, so two runs of an
// unchanged tree produce identical output and a diff of two reports shows what
// moved in the tree rather than what moved in a map.
func (v *verifier) report() *Report {
	return &Report{
		Missing:  sorted(v.missing),
		Modified: sorted(v.modified),
		Orphaned: sorted(v.orphaned),
		Checked:  v.checked,
	}
}

// sorted returns the set's members in order, and nil for an empty set so that a
// clean report holds no allocated-but-empty slices to distinguish from absent
// ones.
func sorted(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
