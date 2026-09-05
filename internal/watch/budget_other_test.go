// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

//go:build !darwin && !linux

// Tests for the platforms with no watch limit this can read.

package watch

import "testing"

// A refusal here would be invented rather than measured. Windows bounds
// directory handles by memory, and fsnotify reports the real failure if one
// comes.
func TestReserveAcceptsAnyPlan(t *testing.T) {
	if err := Reserve(Plan{Dirs: make([]string, 5_000_000), Files: 50_000_000}); err != nil {
		t.Errorf("Reserve refused on a limit it cannot read: %v", err)
	}
}
