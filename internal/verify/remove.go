// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package verify

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ErrNoClaims is why removal refuses on a tree whose manifest records nothing.
var ErrNoClaims = errors.New(
	"the manifest claims nothing, so every generated file looks unowned; that is a " +
		"lost manifest rather than a tree of foreign files — repair it with " +
		"cairn build --adopt before removing anything")

// RemoveOrphaned deletes the output a report found unowned, and returns the
// paths it removed, sorted.
//
// check could always name stale output and nothing could act on it. Prune only
// ever removes what the manifest records, by design, so output from an earlier
// config — a renamed index_basename, a dropped format — stayed unclaimed and
// therefore unprunable for the life of the mirror.
//
// It refuses outright when the manifest claims nothing. In that state cairn owns
// nothing, so every file wearing a generated name is reported as an orphan, and
// removing them would delete the whole published output. A tree in which every
// single generated file is genuinely foreign does not really happen; a tree
// whose manifest was lost happens often enough that --adopt exists for it, and
// that is the repair this points at.
//
// Containment is re-checked per path rather than trusted. A Report is data, and
// a caller that built or edited one must not be able to reach outside the
// output root through it.
func RemoveOrphaned(outDir string, rep *Report) ([]string, error) {
	if len(rep.Orphaned) == 0 {
		return nil, nil
	}
	if rep.Claims == 0 {
		return nil, ErrNoClaims
	}

	removed := make([]string, 0, len(rep.Orphaned))
	for _, rel := range rep.Orphaned {
		abs, err := containedPath(outDir, rel)
		if err != nil {
			return removed, err
		}
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			return removed, fmt.Errorf("remove %s: %w", rel, err)
		}
		removed = append(removed, rel)
	}
	sort.Strings(removed)
	removeEmptyDirs(outDir, removed)
	return removed, nil
}

// removeEmptyDirs clears directories the removals left holding nothing.
//
// Deepest first, and best effort: os.Remove refuses a directory that still has
// anything in it, which is exactly the test wanted, so a failure here means the
// directory was still in use and should stay.
func removeEmptyDirs(outDir string, removed []string) {
	dirs := make([]string, 0, len(removed))
	for _, rel := range removed {
		if d := filepath.Dir(rel); d != "." {
			dirs = append(dirs, d)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	for _, d := range dirs {
		abs, err := containedPath(outDir, d)
		if err != nil {
			continue
		}
		_ = os.Remove(abs)
	}
}
