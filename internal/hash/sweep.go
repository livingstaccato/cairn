// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package hash

import (
	"path/filepath"
	"strings"
)

// Sweep forgets every record under prefix that this run never consulted, and
// reports how many it dropped.
//
// Without it the cache only ever grows. A record is keyed by the path of a file
// in the indexed tree, and nothing has ever removed the record for a file that
// was deleted or renamed, so a long-lived mirror accumulates one entry per file
// that ever existed in it. On a tree of two million files that is a cache
// measured in hundreds of megabytes, read and re-written on every build, almost
// all of it describing files that are gone.
//
// The prefix is what makes this safe, and is not optional. A scoped rebuild —
// every `cairn watch` event is one — consults a single subtree, so "drop what
// this run did not consult" applied to the whole cache would discard the
// digests for the rest of the mirror and force a full re-hash on the next
// build. Bounding the sweep to the region the run was actually authoritative
// for makes the rule true again: a full build passes the root, a scoped
// rebuild passes its scope, and everything outside is left exactly as it was.
//
// Call it only after a run that completed. A build that failed halfway never
// reached the rest of its scope, and sweeping then would throw away digests
// that are still perfectly good.
func (c *Cache) Sweep(prefix string) int { return c.sweep(prefix, true) }

// Stale reports what Sweep would drop, and drops nothing. This is the number a
// dry run prints: asking twice has to give the same answer.
func (c *Cache) Stale(prefix string) int { return c.sweep(prefix, false) }

func (c *Cache) sweep(prefix string, apply bool) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	prefix = filepath.Clean(prefix)
	n := 0
	for p, r := range c.entries {
		if r.touched || !under(p, prefix) {
			continue
		}
		n++
		if apply {
			// Deleting during a range over a map is defined: an entry removed
			// before it is reached is simply not produced.
			delete(c.entries, p)
		}
	}
	if apply && n > 0 {
		c.dirty = true
	}
	return n
}

// under reports whether the record keyed by p lies within prefix.
//
// Segment-aware, because a raw string prefix would match "docs-old" against a
// scope of "docs" and sweep a sibling directory's digests along with the
// scope's own. The root cases are match-all: a config with a relative root:
// produces relative keys, for which the whole-tree prefix cleans to ".", and
// nothing is under "./" by string comparison.
func under(p, prefix string) bool {
	if prefix == "." || prefix == string(filepath.Separator) {
		return true
	}
	return p == prefix || strings.HasPrefix(p, prefix+string(filepath.Separator))
}
