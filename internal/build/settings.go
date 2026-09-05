// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package build

import (
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/emit"
	"github.com/livingstaccato/cairn/internal/hash"
)

// SettingsFor resolves the settings that apply to one directory, including its
// own .cairn.yaml.
//
// Exported for callers outside a build that have to agree with one: a watcher
// deciding which directories to register has to hide exactly what the build
// hides, or it watches a tree the build does not index.
func SettingsFor(cfg *config.Config, rootDir, relDir string, log *slog.Logger) config.Settings {
	return cfg.Resolve(relDir, dirOverrideAt(rootDir, relDir, log))
}

// OutRel reports where the output directory sits inside the indexed tree, as a
// slash-separated path relative to root, and "" when it is not inside one.
//
// "" covers three different arrangements and they all want the same answer:
// separate trees, the output holding the root, and the mirror where root and
// out are one directory. The mirror is the interesting one — there is no
// subtree to skip there, and it is cairn's own generated filenames that keep a
// build from feeding itself.
//
// One function because two callers must agree. The walk skips this subtree so a
// build does not index its own output, and internal/watch skips events under it
// so a rebuild does not wake itself. They disagreed once: the watcher had the
// rule and the builder did not, so every build indexed the previous build's
// output one level deeper — site/, then site/site/ — and never reached a fixed
// point.
func OutRel(rootDir, outDir string) string {
	rel, err := filepath.Rel(rootDir, outDir)
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return ""
	}
	return rel
}

// GeneratedNames lists the basenames cairn writes into a directory it indexes.
//
// A single set drives two decisions that have to agree: which entries a listing
// leaves out, and which filesystem events a watcher discards. Disagreement
// between them is a rebuild loop — cairn's own output waking the watcher that
// asks cairn to write it again.
func GeneratedNames(cfg *config.Config) map[string]bool {
	names := map[string]bool{
		emit.SumsFile:     true,
		emit.SearchFile:   true,
		emit.ManifestFile: true,
		hash.CacheFile:    true,
	}
	for _, base := range []string{cfg.IndexBasename, treeBasename} {
		for _, ext := range []string{".html", ".json", ".csv", ".txt"} {
			names[base+ext] = true
		}
	}
	if cfg.Mode == config.ModeHugo {
		names[emit.HugoContentFile] = true
	}
	return names
}

// dirOverrideAt reads a directory's own settings file, tolerating a directory
// that has just been deleted: a rebuild triggered by a removal still has to
// resolve settings for the path that is gone.
//
// The logger is required rather than optional. A malformed .cairn.yaml is
// reported, and a runner assembled without one would panic on the only path
// that has anything to say.
func dirOverrideAt(rootDir, relDir string, log *slog.Logger) *config.Override {
	r := &runner{root: rootDir, log: log}
	return r.dirOverride(filepath.Join(rootDir, filepath.FromSlash(relDir)))
}
