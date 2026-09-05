// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package watch

import (
	"log/slog"
	"sort"
	"strings"

	"github.com/livingstaccato/cairn/internal/build"
	"github.com/livingstaccato/cairn/internal/config"
)

// Coalesce turns the directories that changed during one settle window into the
// smallest set of rebuilds that covers them.
//
// Two reductions, in order. Each changed directory is first widened to the
// scope that owns it, because a recursive listing above it describes the
// subtree the change is in. The widened scopes are then reduced against each
// other: a scope inside another is already rebuilt by it, so keeping both would
// walk the same subtree twice and prune it twice.
func Coalesce(cfg *config.Config, rootDir string, dirs []string, log *slog.Logger) []string {
	seen := map[string]bool{}
	for _, d := range dirs {
		s := build.Scope(cfg, rootDir, d, log)
		if s == "." {
			// The root owns everything; nothing else can narrow it.
			return []string{"."}
		}
		seen[s] = true
	}

	scopes := make([]string, 0, len(seen))
	for s := range seen {
		scopes = append(scopes, s)
	}
	sort.Strings(scopes)

	// Compared against every scope kept, not just the last one. Sorting does
	// not put a parent next to its children: "docs-old" falls between "docs"
	// and "docs/a", because "-" sorts below "/".
	out := scopes[:0:0]
	for _, s := range scopes {
		if covered(out, s) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// covered reports whether any scope already kept rebuilds s.
func covered(kept []string, s string) bool {
	for _, k := range kept {
		if Covers(k, s) {
			return true
		}
	}
	return false
}

// Covers reports whether rebuilding parent also rebuilds child.
//
// Matched on whole path segments. A prefix test would have "docs" cover
// "docs-old", quietly dropping a rebuild that shares nothing but spelling.
func Covers(parent, child string) bool {
	if parent == "." || parent == "" {
		return true
	}
	return child == parent || strings.HasPrefix(child, parent+"/")
}
