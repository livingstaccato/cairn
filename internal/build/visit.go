// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Walking one directory: producing its entries from whichever source the
// settings name, merging what is authored beside them, hashing what is asked
// for, and descending.
package build

import (
	"os"
	"path"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/livingstaccato/cairndex/internal/config"
	"github.com/livingstaccato/cairndex/internal/hash"
	"github.com/livingstaccato/cairndex/internal/meta"
	"github.com/livingstaccato/cairndex/internal/model"
	"github.com/livingstaccato/cairndex/internal/walk"
)

// visited runs once per directory, right after visit's own cancellation
// check and before any of its work. A test hook, a no-op in production: it
// lets a test cancel ctx deterministically after a specific directory
// instead of racing wall-clock time against however long a walk takes —
// the same reason cmd/cairndex's afterSignalRegistered exists.
var visited = func(relDir string) {}

// visit processes one directory and recurses into its children.
//
// ctx is checked here and nowhere finer-grained: a directory is the
// smallest unit build.Run's own error path — SavePartial, the manifest
// recording exactly what was written — already treats as atomic, so
// stopping between directories rather than mid-directory needs nothing new
// from that path to stay correct.
func (r *runner) visit(relDir string) error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	visited(relDir)
	absDir := filepath.Join(r.root, filepath.FromSlash(relDir))
	s := r.cfg.Resolve(relDir, r.dirOverride(absDir))

	entries, err := r.collect(relDir, absDir, s)
	if err != nil {
		return err
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
		if err := r.emitTree(relDir, s, prose, src); err != nil {
			return err
		}
	}

	r.result.Dirs++
	return r.recurse(relDir, entries)
}

// collect walks one directory, merges its authored metadata, and hashes what
// the settings ask for.
func (r *runner) collect(relDir, absDir string, s config.Settings) ([]model.Entry, error) {
	entries, warns, err := r.produce(relDir, absDir, s)
	if err != nil {
		return nil, err
	}
	r.warn(warns)

	entries = r.dropGenerated(relDir, entries, s)

	m, mwarns, err := meta.Load(absDir)
	if err != nil {
		return nil, err
	}
	r.warn(mwarns)
	entries = meta.Apply(entries, m)
	// Weight comes from the sidecar, so the walker's order predates it.
	walk.Sort(entries, s)

	if s.Checksum == config.ChecksumSHA256 {
		r.hashEntries(absDir, entries)
	}
	return entries, nil
}

// produce lists a directory using the configured source. fs walks the disk,
// pages reads an existing Hugo content section, and manifest reads an authored
// list for contents that are not on disk at build time.
func (r *runner) produce(relDir, absDir string, s config.Settings) ([]model.Entry, []walk.Warning, error) {
	switch s.Source {
	case config.SourcePages:
		return walk.Pages(r.root, relDir, s)
	case config.SourceManifest:
		return walk.Manifest(absDir, s)
	default:
		return walk.Dir(r.root, relDir, s)
	}
}

// recurse descends into each subdirectory of a completed listing.
func (r *runner) recurse(relDir string, entries []model.Entry) error {
	for _, e := range entries {
		if !e.IsDir {
			r.result.Files++
			continue
		}
		child := e.Name
		if relDir != "." {
			child = path.Join(relDir, e.Name)
		}
		if err := r.visit(child); err != nil {
			return err
		}
	}
	return nil
}

// hashEntries fills SHA256 for every file in the listing. A file that cannot be
// hashed warns and is left without a digest rather than failing the build: the
// listing is still correct, it just cannot be verified.
func (r *runner) hashEntries(absDir string, entries []model.Entry) {
	// Collected first, then hashed together. One directory of a package pool is
	// thousands of files, and digesting them one at a time leaves every core but
	// one idle waiting on a read that has already returned.
	jobs := make([]hash.Job, 0, len(entries))
	at := make([]int, 0, len(entries))
	for i := range entries {
		if entries[i].IsDir {
			continue
		}
		jobs = append(jobs, hash.Job{
			Path:    filepath.Join(absDir, entries[i].Name),
			Size:    entries[i].Size,
			ModTime: entries[i].ModTime.Unix(),
		})
		at = append(at, i)
	}

	for j, res := range r.cache.SumAll(jobs) {
		i := at[j]
		if res.Err != nil {
			// A file that cannot be hashed is left without a digest rather than
			// failing the build: the listing is still correct, it just cannot be
			// verified.
			r.log.Warn("could not hash file; omitted from SHA256SUMS",
				"path", entries[i].Path, "err", res.Err)
			continue
		}
		entries[i].SHA256 = res.Sum
	}
}

// dirOverride reads a directory's .cairndex.yaml, if present. An unreadable or
// malformed one is ignored rather than fatal — it is a local preference, not
// authored content whose loss would make the index wrong.
//
// Deliberately not decoded with KnownFields, unlike the root cairndex.yaml. The
// same file is read twice by two decoders: this one takes the settings, and
// walk.Manifest takes entries: from it when source: manifest. Each has to
// ignore what the other owns, so strictness here would refuse every authored
// manifest in the repository.
func (r *runner) dirOverride(absDir string) *config.Override {
	p := filepath.Join(absDir, ".cairndex.yaml")
	// #nosec G304 -- p is a directory cairndex was configured to scan plus a fixed
	// filename.
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var o config.Override
	if err := yaml.Unmarshal(b, &o); err != nil {
		r.log.Warn("ignoring malformed .cairndex.yaml", "path", p, "err", err)
		return nil
	}
	// The same checks the root cairndex.yaml's defaults: and every rule go
	// through — otherwise a checksum: typo here does not fail the way the
	// identical typo at the root does. It just never gives this directory's
	// entries a digest, silently.
	if err := config.ValidateOverride(p, o); err != nil {
		r.log.Warn("ignoring invalid .cairndex.yaml", "path", p, "err", err)
		return nil
	}
	return &o
}
