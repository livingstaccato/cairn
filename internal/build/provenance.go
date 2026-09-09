// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// provenance.json: a build attestation, opt-in, written only by a full
// build.
package build

import (
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/livingstaccato/cairndex/internal/emit"
	"github.com/livingstaccato/cairndex/internal/hash"
)

// writeProvenance writes provenance.json: cairndex's own version, a hash of
// the config that drove this run, and a digest of every file the run wrote,
// so a verifier need not trust the tree's bytes on faith. Called only from
// the full-build path (build(), reached through Run/RunWith) — RunScoped's
// own buildScoped never calls this, since a scoped rebuild's Written only
// covers the subtree it touched, not the whole tree a manifest at out:'s
// root would claim to describe. A long-running `watch --serve` process's
// manifest therefore ages as later scoped rebuilds change files under it;
// a fresh `cairndex build`, or restarting watch, is what refreshes it — the
// same boundary the hash cache's own sweep already draws at "the region
// the run rebuilt" (see runner.forget).
//
// A no-op unless provenance: true, and on a dry run: dry already reports
// what a build would write without changing out:, and this would otherwise
// be the one artifact it actually wrote.
func (r *runner) writeProvenance() error {
	if !r.cfg.Provenance || r.dry {
		return nil
	}

	written := append([]string(nil), r.writer.Written()...)
	sort.Strings(written)

	jobs := make([]hash.Job, 0, len(written))
	paths := make([]string, 0, len(written))
	for _, rel := range written {
		abs := filepath.Join(r.out, filepath.FromSlash(rel))
		fi, err := os.Stat(abs)
		if err != nil {
			// The listings themselves are already correct and written; a
			// provenance entry that cannot be produced for one of them is
			// this attestation's own gap to report, not a reason to fail a
			// build that already succeeded.
			r.log.Warn("could not stat a written output; omitted from provenance",
				"path", rel, "err", err)
			continue
		}
		jobs = append(jobs, hash.Job{Path: abs, Size: fi.Size(), ModTime: fi.ModTime().Unix()})
		paths = append(paths, rel)
	}

	outputs := make([]emit.ProvenanceFile, 0, len(paths))
	// A throwaway cache: these are files this run just wrote, so nothing
	// from an earlier run could still be valid, and the batch exists here
	// for SumAll's own worker pool, not for its memoization.
	for i, res := range hash.NewCache("").SumAll(jobs) {
		if res.Err != nil {
			r.log.Warn("could not hash a written output; omitted from provenance",
				"path", paths[i], "err", res.Err)
			continue
		}
		outputs = append(outputs, emit.ProvenanceFile{Path: paths[i], SHA256: res.Sum})
	}

	b, err := emit.Manifest(emit.Provenance{
		CairndexVersion: r.version,
		ConfigSHA256:    r.configSHA256,
		StartedAt:       r.started.UTC().Format(time.RFC3339),
		FinishedAt:      time.Now().UTC().Format(time.RFC3339),
		Dirs:            r.result.Dirs,
		Files:           r.result.Files,
		Outputs:         outputs,
	})
	if err != nil {
		return err
	}
	return r.write(".", "provenance.json", b)
}
