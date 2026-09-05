// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package verify

import (
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
)

// ErrNoClaims is why removal refuses on a tree whose manifest records nothing.
var ErrNoClaims = errors.New(
	"the manifest claims nothing, so every generated file looks unowned; that is a " +
		"lost manifest rather than a tree of foreign files — repair it with " +
		"cairn build --adopt before removing anything")

// Removal is what RemoveOrphaned deleted, and what it refused to.
//
// Kept is not a weaker Removed. It is the set the report named and the content
// test would not vouch for, and every path in it is still a finding an operator
// has to settle by hand — so it stays in the report and still decides the exit
// code. Counting it as dealt with is the behaviour that lost files.
type Removal struct {
	Removed []string
	Kept    []string
	// RemovedDirs are the directories the removals left holding nothing.
	//
	// Returned rather than swept silently, for the reason check states about
	// every other finding: a directory that disappears with no record anywhere
	// is one nobody can account for afterwards, and in a mirror it is a
	// directory of the published artifact tree.
	RemovedDirs []string
}

// RemoveOrphaned deletes the output a report found unowned and returns what it
// took and what it left.
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
//
// What it deletes is narrower than what check reports. A report answers "could
// cairn have written a file with this name", which is the right question to put
// in front of a person and the wrong one to hand to rm: in a mirror the names
// cairn generates are the most ordinary names in the tree. Every deletion here
// is gated on provenOurs, so a file is removed only when its own bytes say cairn
// wrote it. The rest come back in Kept.
func RemoveOrphaned(outDir string, rep *Report) (*Removal, error) {
	res := &Removal{}
	if len(rep.Orphaned) == 0 {
		return res, nil
	}
	if rep.Claims == 0 {
		return res, ErrNoClaims
	}

	// Resolved once for the whole removal rather than per path: it cannot
	// change under us, and a mirror can present tens of thousands of orphans.
	outAbs, err := resolveRoot(outDir)
	if err != nil {
		return res, err
	}

	for _, rel := range rep.Orphaned {
		deleted, err := removeOneOrphan(outAbs, rel)
		if err != nil {
			return res, err
		}
		if deleted {
			res.Removed = append(res.Removed, rel)
		} else {
			res.Kept = append(res.Kept, rel)
		}
	}
	sort.Strings(res.Removed)
	sort.Strings(res.Kept)
	res.RemovedDirs = removeEmptyDirs(outAbs, res.Removed)
	return res, nil
}

// removeOneOrphan decides one path's fate and, if its content proves it,
// deletes it.
//
// verifiedAncestors is called twice rather than once and trusted for both
// steps: an ancestor a concurrent writer swapped for a symlink between the
// provenance read and the unlink is exactly the gap a single check up front
// would leave open, and each of those two filesystem operations is a place a
// swap could have landed by the time it runs.
func removeOneOrphan(outAbs, rel string) (deleted bool, err error) {
	abs, err := containedIn(outAbs, rel)
	if err != nil {
		return false, err
	}
	if err := verifiedAncestors(outAbs, rel); err != nil {
		return false, err
	}
	// The name got it reported; only the content gets it deleted.
	if !provenOurs(abs) {
		return false, nil
	}
	if err := verifiedAncestors(outAbs, rel); err != nil {
		return false, err
	}
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("remove %s: %w", rel, err)
	}
	return true, nil
}

// removeEmptyDirs clears the directories the removals left holding nothing, and
// reports which ones went.
//
// Every ancestor, not only the immediate parent. Taking a/b/c/index.csv empties
// a/b/c, and once that goes it empties a/b, and then a — a sweep that stopped at
// the first level left the rest of the chain standing, which is the litter it
// exists to clear.
//
// Deepest first, so a parent is only ever tried after its children have had
// their turn. Best effort past that: os.Remove refuses a directory that still
// holds anything, which is exactly the test wanted, so a failure here means the
// directory was still in use and should stay.
//
// The output root is never a candidate. path.Dir stops at ".", and the root is
// not litter however empty a removal leaves it.
func removeEmptyDirs(outAbs string, removed []string) []string {
	seen := map[string]bool{}
	var dirs []string
	for _, rel := range removed {
		for d := path.Dir(rel); d != "." && d != "/"; d = path.Dir(d) {
			if !seen[d] {
				seen[d] = true
				dirs = append(dirs, d)
			}
		}
	}

	// Deepest first by segment count; name order only breaks ties, so that a
	// sibling never comes between a directory and its own parent.
	sort.Slice(dirs, func(i, j int) bool {
		di, dj := strings.Count(dirs[i], "/"), strings.Count(dirs[j], "/")
		if di != dj {
			return di > dj
		}
		return dirs[i] < dirs[j]
	})

	var gone []string
	for _, d := range dirs {
		abs, err := containedIn(outAbs, d)
		if err != nil {
			continue
		}
		// Best effort, like the rest of this function: a directory a concurrent
		// writer is standing in front of is a directory this sweep leaves alone,
		// the same as one os.Remove refuses because it still holds something.
		if err := verifiedAncestors(outAbs, d); err != nil {
			continue
		}
		if os.Remove(abs) == nil {
			gone = append(gone, d)
		}
	}
	sort.Strings(gone)
	return gone
}
