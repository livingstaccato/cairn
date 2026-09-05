// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package build

import (
	"log/slog"
	"path/filepath"

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
