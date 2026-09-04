// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/model"
)

// Warning is a non-fatal problem encountered during a walk. Warnings are
// returned rather than swallowed: a mirror that quietly omits a file is worse
// than one that says which file it could not read.
type Warning struct {
	Path string
	Err  error
}

func (w Warning) String() string { return w.Path + ": " + w.Err.Error() }

// scanner carries the two values every step of a walk needs, so the per-entry
// functions take a relative path and a depth rather than six arguments.
type scanner struct {
	root string
	s    config.Settings
}

// Dir lists relDir non-recursively. Every returned entry has Depth 1.
func Dir(root, relDir string, s config.Settings) ([]model.Entry, []Warning, error) {
	sc := &scanner{root: root, s: s}
	entries, warns, err := sc.readDir(relDir, 1)
	if err != nil {
		return nil, warns, err
	}
	sortEntries(entries, s)
	return entries, warns, nil
}

// Tree lists relDir and all descendants. Depth counts from relDir, whose direct
// children are Depth 1. Exceeding maxEntries is an error, never a truncation: a
// silently short index is a wrong index.
func Tree(root, relDir string, s config.Settings, maxEntries int) ([]model.Entry, []Warning, error) {
	sc := &scanner{root: root, s: s}
	var out []model.Entry
	var warns []Warning
	seen := map[string]bool{}

	var recurse func(rel string, depth int) error
	recurse = func(rel string, depth int) error {
		batch, w, err := sc.readDir(rel, depth)
		warns = append(warns, w...)
		if err != nil {
			return err
		}
		sortEntries(batch, s)
		for _, e := range batch {
			out = append(out, e)
			if len(out) > maxEntries {
				return fmt.Errorf("tree under %q exceeds tree_max_entries (%d); "+
					"raise the cap or set recursive: false for this rule", relDir, maxEntries)
			}
			if !e.IsDir {
				continue
			}
			child := path.Join(rel, e.Name)
			if sc.alreadyVisited(child, seen) {
				warns = append(warns, Warning{Path: child, Err: fmt.Errorf("symlink loop, skipped")})
				continue
			}
			if err := recurse(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}

	if err := recurse(relDir, 1); err != nil {
		return nil, warns, err
	}
	return out, warns, nil
}

// alreadyVisited records a directory's resolved identity and reports whether it
// has been walked before. An unresolvable path is treated as unvisited; readDir
// will report the real error.
func (sc *scanner) alreadyVisited(rel string, seen map[string]bool) bool {
	abs, err := filepath.EvalSymlinks(filepath.Join(sc.root, filepath.FromSlash(rel)))
	if err != nil {
		return false
	}
	if seen[abs] {
		return true
	}
	seen[abs] = true
	return false
}

// readDir reads a single directory into entries at the given depth.
func (sc *scanner) readDir(relDir string, depth int) ([]model.Entry, []Warning, error) {
	abs := filepath.Join(sc.root, filepath.FromSlash(relDir))
	des, err := os.ReadDir(abs)
	if err != nil {
		return nil, nil, fmt.Errorf("read dir %s: %w", relDir, err)
	}

	var out []model.Entry
	var warns []Warning
	for _, de := range des {
		e, w, ok := sc.entry(relDir, de, depth)
		warns = append(warns, w...)
		if ok {
			out = append(out, e)
		}
	}
	return out, warns, nil
}

// entry builds one Entry, reporting whether it belongs in the listing.
func (sc *scanner) entry(relDir string, de os.DirEntry, depth int) (model.Entry, []Warning, bool) {
	name := de.Name()
	rel := path.Join(relDir, name)

	if hiddenByPolicy(sc.s.Hidden, name) {
		return model.Entry{}, nil, false
	}
	if de.Type()&os.ModeSymlink != 0 {
		if !sc.s.FollowSymlinks {
			return model.Entry{}, nil, false
		}
		// A followed symlink can point anywhere. Listing its target would
		// publish a path from outside the tree cairn was pointed at.
		if !withinRoot(sc.root, filepath.Join(sc.root, filepath.FromSlash(rel))) {
			return model.Entry{}, []Warning{{
				Path: rel, Err: fmt.Errorf("symlink escapes the root, skipped"),
			}}, false
		}
	}

	info, err := de.Info()
	if err != nil {
		return model.Entry{}, []Warning{{Path: rel, Err: err}}, false
	}
	isDir := de.IsDir() || (info.Mode()&os.ModeDir != 0)
	kind, mimeType := KindOf(name, isDir)
	e := model.Entry{
		Name:    name,
		Path:    "/" + rel,
		IsDir:   isDir,
		Size:    info.Size(),
		ModTime: modTime(info.ModTime()),
		Kind:    kind,
		MIME:    mimeType,
		Depth:   depth,
	}
	if !isDir {
		return e, nil, true
	}

	e.Path += "/"
	e.Size = 0
	count, warns := sc.countChildren(rel)
	e.Count = count
	return e, warns, true
}

// countChildren counts a directory's visible children for the listing's
// "N items" column.
func (sc *scanner) countChildren(rel string) (int, []Warning) {
	children, err := os.ReadDir(filepath.Join(sc.root, filepath.FromSlash(rel)))
	if err != nil {
		return 0, []Warning{{Path: rel, Err: err}}
	}
	n := 0
	for _, c := range children {
		if !hiddenByPolicy(sc.s.Hidden, c.Name()) {
			n++
		}
	}
	return n, nil
}

// withinRoot reports whether target, after symlink resolution, is inside root.
//
// Both sides are resolved, not just the target. A root can itself sit behind a
// symlink — on macOS every path under /var does, since /var links to
// /private/var — and comparing a resolved target against an unresolved root
// makes every contained entry look like an escape.
func withinRoot(root, target string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rootAbs, err = filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return false
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, resolved)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// hiddenByPolicy reports whether name is hidden under the configured policy.
//
// The two conventions are not the same thing and a real mirror needs them
// apart. A leading dot is a filesystem convention meaning hidden. A leading
// underscore is a Hugo convention meaning "not a page", which says nothing
// about a directory of static artifacts that the site publishes and links to —
// so skip: would drop a whole published subtree, and show: would put .DS_Store
// on the page.
func hiddenByPolicy(policy, name string) bool {
	switch policy {
	case config.HiddenShow:
		return false
	case config.HiddenDotfiles:
		return strings.HasPrefix(name, ".")
	default:
		return isHidden(name)
	}
}

// isHidden covers both the dotfile convention and the underscore convention the
// listings this replaces already used.
func isHidden(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// sortEntries orders a listing in place.
func sortEntries(es []model.Entry, s config.Settings) {
	sort.SliceStable(es, func(i, j int) bool {
		a, b := es[i], es[j]
		// Directories-first is a grouping, not an ordering, so Order never
		// reverses it — a descending listing still opens with its directories.
		if s.DirsFirst && a.IsDir != b.IsDir {
			return a.IsDir
		}
		less := lessBy(s.Sort, a, b)
		if s.Order == config.OrderDesc {
			return !less
		}
		return less
	})
}

// lessBy compares two entries on one key, falling back to name so the order is
// total and therefore stable across runs.
func lessBy(key string, a, b model.Entry) bool {
	switch key {
	case config.SortSize:
		if a.Size != b.Size {
			return a.Size < b.Size
		}
	case config.SortModTime:
		if !a.ModTime.Equal(b.ModTime) {
			return a.ModTime.Before(b.ModTime)
		}
	case config.SortKind:
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
	}
	return a.Name < b.Name
}
