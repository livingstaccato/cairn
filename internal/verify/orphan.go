// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package verify

import (
	"path"

	"github.com/livingstaccato/cairn/internal/build"
	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/emit"
	"github.com/livingstaccato/cairn/internal/hash"
)

// generatedNames is the set of basenames an orphan can wear.
//
// build.GeneratedNames is the one place that knows what cairn writes into a
// directory, and this has to agree with it exactly: a name missing from that set
// is stale output nothing will ever report, and a name that does not belong in
// it is somebody's own file reported as cairn's litter.
//
// Two are removed. The manifest and the hash cache are written outside
// emit.Writer, so no manifest ever claims them and every intact tree would
// otherwise report both as orphans on every run — a false positive on the one
// path that must stay quiet.
func generatedNames(cfg *config.Config) map[string]bool {
	names := build.GeneratedNames(cfg)
	delete(names, emit.ManifestFile)
	delete(names, hash.CacheFile)
	return names
}

// checkOrphan reports a file that wears one of cairn's output names and that no
// manifest claims.
//
// The test is on the basename, not on the content, and that is what makes the
// check safe in a mirror: root and out are the same directory there, so nearly
// everything the walk meets is somebody's artifact, and only a file named the
// way cairn names its own could ever have been cairn's.
//
// Ownership is per path rather than per name — index.json in one directory says
// nothing about index.json in another — so the manifest is consulted with the
// full relative path.
//
// A protected path is never an orphan. protect: is the operator naming paths
// another tool owns, cairn writes nothing there and therefore claims nothing
// there, so an unclaimed protected file is the documented outcome of the setting
// rather than a finding. Reporting it would fire on every run of every
// repository mirror that uses the feature, which is all of them.
//
// Non-regular files are included. A symlink wearing a generated name is not
// something cairn wrote and nothing claims it, which is exactly the question
// this check asks; a link at a path the manifest does claim is already reported
// as missing instead.
func (v *verifier) checkOrphan(rel string) {
	_, ours := v.claimed[rel]
	if !v.names[path.Base(rel)] || ours || v.cfg.IsProtected(rel) {
		return
	}
	v.orphaned[rel] = true
}
