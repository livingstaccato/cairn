// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package watch

import "fmt"

// headroom is what a watcher leaves for everything that is not a watch: the Go
// runtime's own descriptors, the config, and the files a rebuild opens while
// the watches are held.
const headroom = 64

// BudgetError says a tree is larger than the platform will watch.
//
// It is an error and not a warning on purpose. A watcher that registers what it
// can and starts anyway is worse than one that refuses: it reports some changes
// and silently drops the rest, and the directories it dropped are whichever the
// walk happened to reach last. A stale index nobody is told about is the one
// failure a rebuild-on-change tool must not have.
type BudgetError struct {
	// Need is what the plan costs, headroom included.
	Need int
	// Have is what the platform allows.
	Have int
	// Limit names the setting, so the message says what to raise.
	Limit string
	// Hint is how to raise it, when raising it is the operator's call.
	Hint string
}

func (e *BudgetError) Error() string {
	msg := fmt.Sprintf("watching this tree needs %d %s and the limit is %d", e.Need, e.Limit, e.Have)
	if e.Hint != "" {
		msg += "; " + e.Hint
	}
	return msg
}

// cost is what a plan asks of a per-descriptor platform.
func (p Plan) cost() int { return len(p.Dirs) + p.Files + headroom }
