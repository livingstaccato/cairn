// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package watch

import (
	"log/slog"
	"os"
	"path"
	"path/filepath"

	"github.com/livingstaccato/cairn/internal/build"
	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/walk"
)

// Plan is the set of directories a watcher intends to register, and how many
// files they hold.
//
// Both numbers are kept because the platforms disagree about which one costs.
// inotify charges a watch per directory and nothing for the files inside it;
// kqueue opens a descriptor for every file as well, so a flat directory of ten
// thousand artifacts is ten thousand descriptors on macOS and one watch on
// Linux.
type Plan struct {
	Dirs  []string
	Files int
}

// Enumerate lists the directories a watcher has to register for a config.
//
// It hides what the build hides, using the same globs and the same per-
// directory overrides. Watching a directory the build ignores costs a
// descriptor to learn about a change that cannot alter any output — and on a
// tree holding a .git directory, that is most of the descriptors.
func Enumerate(cfg *config.Config, rootDir, outDir string, log *slog.Logger) Plan {
	return EnumerateUnder(cfg, rootDir, outDir, ".", log)
}

// EnumerateUnder lists the directories under one subtree of the watched root.
//
// relDir stays relative to the root rather than becoming a root of its own: a
// rule matching "bootstrap/**" has to resolve the same for a directory that
// appeared a moment ago as for one that was there at startup.
func EnumerateUnder(cfg *config.Config, rootDir, outDir, relDir string, log *slog.Logger) Plan {
	f := NewFilter(cfg, rootDir, outDir, log)
	var p Plan

	var descend func(relDir string)
	descend = func(relDir string) {
		p.Dirs = append(p.Dirs, filepath.Join(rootDir, filepath.FromSlash(relDir)))
		s := build.SettingsFor(cfg, rootDir, relDir, log)

		entries, err := os.ReadDir(filepath.Join(rootDir, filepath.FromSlash(relDir)))
		if err != nil {
			// One unreadable directory is a directory that will not report its
			// changes, not a reason to refuse to watch the rest of the tree.
			log.Warn("cannot list directory; it will not be watched", "path", relDir, "err", err)
			return
		}
		for _, e := range entries {
			rel := path.Join(relDir, e.Name())
			if walk.HiddenByGlob(s.Hide, rel) {
				continue
			}
			if f.outRel != "" && Covers(f.outRel, rel) {
				continue
			}
			// Not IsDir on a symlink: os.DirEntry reports link type, and that
			// is the answer wanted here. fsnotify does not follow links, and
			// descending through one would let a loop consume every descriptor
			// the budget just reserved.
			if e.IsDir() {
				descend(rel)
				continue
			}
			p.Files++
		}
	}

	descend(relDir)
	return p
}
