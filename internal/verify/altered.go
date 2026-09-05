// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package verify

import "path/filepath"

// unkeyed stands in for the size and mtime the cache keys on. A verify run
// never reuses a digest across paths — every one is read from the bytes — so
// there is nothing for the key to distinguish.
const unkeyed = -1

// checkAltered reports cairn's own output that no longer holds what cairn wrote.
//
// This is the gap the manifest's digests exist to close. Generated files appear
// in no SHA256SUMS — a listing leaves cairn's own output out, or the build never
// reaches a fixed point — so nothing else records what an index.json should
// contain. A watcher cannot catch it either: it discards events on its own
// output by name, which is what stops a rebuild loop, and a name cannot tell
// cairn's write from anyone else's.
func (v *verifier) checkAltered() {
	for claim, want := range v.claimed {
		// checkMissing has already spoken for a path that is gone; re-reporting
		// it here would name one absence twice under two different headings.
		if v.missing[claim] {
			continue
		}
		abs, err := filepath.Abs(filepath.Join(v.out, filepath.FromSlash(claim)))
		if err != nil {
			continue
		}
		got, err := v.cache.Sum(abs, unkeyed, unkeyed)
		if err != nil {
			// Unreadable but present. checkMissing has already spoken for a path
			// that is gone, so this is a file standing there that cannot be
			// compared — reported rather than passed over in silence.
			v.log.Warn("could not re-hash generated output", "path", claim, "err", err)
			v.altered[claim] = true
			continue
		}
		v.compared++
		if got != want {
			v.altered[claim] = true
		}
	}
}
