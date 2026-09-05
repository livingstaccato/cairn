// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package watch

import (
	"os"
	"strconv"
	"strings"
)

// maxUserWatches holds the per-user ceiling on inotify watch descriptors.
const maxUserWatches = "/proc/sys/fs/inotify/max_user_watches"

// Reserve checks the plan against the inotify watch ceiling.
//
// inotify costs one watch descriptor per directory and nothing for the files
// inside it, so a tree of fifty thousand artifacts in a hundred directories is
// a hundred watches here and fifty thousand descriptors on macOS. Nothing to
// raise: the ceiling is a sysctl, shared with every other process owned by this
// user, and writing it needs root — so this reports rather than raises.
func Reserve(p Plan) error {
	limit, ok := readLimit(maxUserWatches)
	if !ok {
		// A container or a kernel without the knob. fsnotify surfaces ENOSPC on
		// the watch that fails; refusing on an unread file would be refusing on
		// no evidence.
		return nil
	}
	if len(p.Dirs) <= limit {
		return nil
	}
	return &BudgetError{
		Need: len(p.Dirs), Have: limit, Limit: "inotify watches",
		Hint: "raise fs.inotify.max_user_watches, or hide: what does not need indexing",
	}
}

// readLimit reads a single-integer sysctl file.
func readLimit(p string) (int, bool) {
	// #nosec G304 -- p is a package constant naming a kernel sysctl.
	b, err := os.ReadFile(p)
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}
