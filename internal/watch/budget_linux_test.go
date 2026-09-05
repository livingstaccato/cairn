// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for the inotify watch budget, which is a per-user kernel setting rather
// than something this process can raise.

package watch

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReserveRefusesMoreDirectoriesThanInotifyAllows(t *testing.T) {
	limit, ok := readLimit(maxUserWatches)
	if !ok {
		t.Skipf("cannot read %s", maxUserWatches)
	}
	dirs := make([]string, limit+1)
	err := Reserve(Plan{Dirs: dirs})

	var budget *BudgetError
	if !errors.As(err, &budget) {
		t.Fatalf("Reserve = %v, want a BudgetError", err)
	}
	if budget.Have != limit {
		t.Errorf("BudgetError reports a limit of %d, want %d", budget.Have, limit)
	}
}

// Files, unlike directories, cost nothing here. A budget that counted them
// would refuse a tree inotify watches without complaint.
func TestReserveIgnoresFileCount(t *testing.T) {
	if err := Reserve(Plan{Dirs: []string{"a"}, Files: 10_000_000}); err != nil {
		t.Errorf("Reserve refused on a file count inotify does not charge for: %v", err)
	}
}

// A container or a kernel without the knob is no evidence either way, and
// refusing on no evidence would refuse a tree that works.
func TestReadLimitRejectsWhatItCannotUse(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{"missing": "", "garbage": "not a number\n", "zero": "0\n"}
	for name, body := range cases {
		p := filepath.Join(dir, name)
		if body != "" {
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if _, ok := readLimit(p); ok {
			t.Errorf("readLimit accepted %s", name)
		}
	}

	p := filepath.Join(dir, "good")
	if err := os.WriteFile(p, []byte(" 65536\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if n, ok := readLimit(p); !ok || n != 65536 {
		t.Errorf("readLimit = %d, %v; want 65536, true", n, ok)
	}
}
