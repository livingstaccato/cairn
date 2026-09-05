// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for the platform limit a watch has to fit inside.

package watch

import (
	"strings"
	"testing"
)

// Whatever the platform charges for, a handful of directories fits.
func TestReserveAcceptsASmallTree(t *testing.T) {
	p := Plan{Dirs: []string{"a", "b", "c"}, Files: 12}
	if err := Reserve(p); err != nil {
		t.Errorf("Reserve refused a three-directory tree: %v", err)
	}
}

func TestPlanCostIncludesHeadroom(t *testing.T) {
	p := Plan{Dirs: []string{"a", "b"}, Files: 5}
	if got := p.cost(); got != 2+5+headroom {
		t.Errorf("cost = %d, want %d", got, 2+5+headroom)
	}
}

// The message has to name the limit and say what to do, because the operator
// reading it is the only one who can raise it.
func TestBudgetErrorNamesTheLimitAndTheFix(t *testing.T) {
	err := &BudgetError{Need: 9000, Have: 256, Limit: "handles", Hint: "raise it"}
	msg := err.Error()
	for _, want := range []string{"9000", "256", "handles", "raise it"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Error() = %q, missing %q", msg, want)
		}
	}
	if got := (&BudgetError{Need: 1, Have: 0, Limit: "watches"}).Error(); strings.HasSuffix(got, "; ") {
		t.Errorf("Error() = %q, left a dangling separator with no hint", got)
	}
}
