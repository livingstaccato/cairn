// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package watch

import (
	"log/slog"
	"os"
	"path"
	"path/filepath"

	"github.com/livingstaccato/cairndex/internal/build"
	"github.com/livingstaccato/cairndex/internal/config"
	"github.com/livingstaccato/cairndex/internal/walk"
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
	e := &enumerator{
		cfg:     cfg,
		rootDir: rootDir,
		log:     log,
		f:       NewFilter(cfg, rootDir, outDir, log),
		// Real, symlink-resolved identities of the followed links currently
		// on the descent's own path — added before descending into one and
		// removed right after, so it catches an ancestor reached again
		// through a link without also refusing two unrelated links that
		// merely point at the same target.
		seen: map[string]bool{},
	}
	e.descend(relDir)
	return e.p
}

// enumerator carries one Enumerate call's state across its recursive
// descent — a type rather than a tree of closures because gocyclo scores a
// closure as part of the function that declares it, and this doesn't fit as
// one function without either exceeding the budget or hiding real branches
// behind one that never trips.
type enumerator struct {
	cfg     *config.Config
	rootDir string
	log     *slog.Logger
	f       *Filter
	seen    map[string]bool
	p       Plan
}

// descend reads one directory, applying it to p and recursing into whatever
// it lists as a subtree.
func (e *enumerator) descend(relDir string) {
	e.p.Dirs = append(e.p.Dirs, filepath.Join(e.rootDir, filepath.FromSlash(relDir)))
	s := build.SettingsFor(e.cfg, e.rootDir, relDir, e.log)

	entries, err := os.ReadDir(filepath.Join(e.rootDir, filepath.FromSlash(relDir)))
	if err != nil {
		// One unreadable directory is a directory that will not report its
		// changes, not a reason to refuse to watch the rest of the tree.
		e.log.Warn("cannot list directory; it will not be watched", "path", relDir, "err", err)
		return
	}
	for _, entry := range entries {
		rel := path.Join(relDir, entry.Name())
		if walk.HiddenByGlob(s.Hide, rel) {
			continue
		}
		if e.f.outRel != "" && Covers(e.f.outRel, rel) {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			e.followEntry(rel, s)
			continue
		}
		if entry.IsDir() {
			e.descend(rel)
			continue
		}
		e.p.Files++
	}
}

// followEntry decides what a symlink entry costs the plan: nothing to
// descend if follow_symlinks is off, a dangling link, or one that resolves
// to a file, and a subtree otherwise — unless its real identity is already
// on the current descent's own path, which is a cycle rather than the
// two-unrelated-links-one-target case this must not also catch.
func (e *enumerator) followEntry(rel string, s config.Settings) {
	if !s.FollowSymlinks {
		// fsnotify does not follow links on its own, and descending through
		// one unbudgeted could let a loop consume every descriptor the
		// budget just reserved. Matches the build's own listing, which
		// lists an unfollowed symlink as an entry and never descends it.
		e.p.Files++
		return
	}
	real, isDir := symlinkTarget(e.rootDir, rel)
	switch {
	case !isDir:
		e.p.Files++
	case e.seen[real]:
		e.log.Warn("symlink loop; not watched", "path", rel)
	default:
		e.seen[real] = true
		e.descend(rel)
		delete(e.seen, real)
	}
}

// symlinkTarget resolves rel — a symlink cairndex has been told to follow —
// against rootDir. isDir is false for a dangling link or one that resolves
// to a file, either of which is a leaf a caller counts rather than descends.
func symlinkTarget(rootDir, rel string) (real string, isDir bool) {
	abs := filepath.Join(rootDir, filepath.FromSlash(rel))
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", false
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", false
	}
	return real, info.IsDir()
}
