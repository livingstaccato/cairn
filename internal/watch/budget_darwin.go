// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package watch

import (
	"fmt"
	"math"

	"golang.org/x/sys/unix"
)

// maxPerProc is the sysctl holding the hard ceiling on one process's open
// files. RLIMIT_NOFILE cannot be raised past it even when the hard limit says
// unlimited, and a Setrlimit that asks for more fails outright rather than
// clamping.
const maxPerProc = "kern.maxfilesperproc"

// limitOpenFiles names what a kqueue watch spends, for the refusal message.
const limitOpenFiles = "open files"

// Reserve raises this process's descriptor limit to cover the plan.
//
// kqueue has no watch descriptors: fsnotify opens the directory and every file
// in it, so watching a tree costs one descriptor per entry. The default soft
// limit is a few hundred, which is a small tree, and the hard limit is high
// enough that raising the soft one needs no privileges — so a watcher asks for
// what it needs rather than making the operator do it first.
func Reserve(p Plan) error {
	cost := p.cost()
	if cost <= 0 {
		return nil
	}
	// #nosec G115 -- cost is positive, checked above, and is a count of
	// directory entries plus a constant.
	want := uint64(cost)

	var lim unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &lim); err != nil {
		// Nothing measured is nothing to refuse on. fsnotify reports the
		// descriptor exhaustion itself if it happens.
		return nil //nolint:nilerr // an unreadable limit is not a budget failure
	}
	if lim.Cur >= want {
		return nil
	}

	ceiling := lim.Max
	if m, err := unix.SysctlUint32(maxPerProc); err == nil && uint64(m) < ceiling {
		ceiling = uint64(m)
	}

	raised := lim
	raised.Cur = min(want, ceiling)
	if err := unix.Setrlimit(unix.RLIMIT_NOFILE, &raised); err == nil && raised.Cur >= want {
		return nil
	}

	// #nosec G115 -- clamped to MaxInt on the same line; a rlimit of
	// RLIM_INFINITY is otherwise a negative count in the message.
	have := int(min(ceiling, math.MaxInt))
	return &BudgetError{
		Need: cost, Have: have, Limit: limitOpenFiles,
		Hint: fmt.Sprintf("%s caps it; watch a subtree, or hide: what does not need indexing", maxPerProc),
	}
}
