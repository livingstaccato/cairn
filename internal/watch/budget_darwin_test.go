// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for the descriptor budget on the kqueue platforms, where watching a
// tree costs one open file per directory and per file inside it.

package watch

import (
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

// The default soft limit is a few hundred descriptors, which is a small tree.
// Raising it needs no privileges, so a watcher asks rather than making the
// operator do it first.
func TestReserveRaisesTheSoftLimit(t *testing.T) {
	var before unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &before); err != nil {
		t.Skipf("cannot read RLIMIT_NOFILE: %v", err)
	}
	t.Cleanup(func() {
		if err := unix.Setrlimit(unix.RLIMIT_NOFILE, &before); err != nil {
			t.Errorf("could not restore RLIMIT_NOFILE: %v", err)
		}
	})

	// Lowered first, so the test asserts that Reserve raises a limit rather
	// than that the shell it inherited was already generous enough.
	low := before
	low.Cur = 256
	if err := unix.Setrlimit(unix.RLIMIT_NOFILE, &low); err != nil {
		t.Skipf("cannot lower RLIMIT_NOFILE: %v", err)
	}

	const want = 4096
	if err := Reserve(Plan{Files: want - headroom}); err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	var after unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &after); err != nil {
		t.Fatal(err)
	}
	if after.Cur < want {
		t.Errorf("soft limit is %d, want at least %d", after.Cur, want)
	}
}

// Room already made is room not asked for again.
func TestReserveLeavesAnAmpleLimitAlone(t *testing.T) {
	var before unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &before); err != nil {
		t.Skipf("cannot read RLIMIT_NOFILE: %v", err)
	}
	if err := Reserve(Plan{Dirs: []string{"a"}, Files: 1}); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	var after unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &after); err != nil {
		t.Fatal(err)
	}
	if after.Cur != before.Cur {
		t.Errorf("soft limit moved from %d to %d for a tree that fit", before.Cur, after.Cur)
	}
}

// Past the process ceiling the answer is a refusal, not a partial watch: a
// watcher that registers what it can reports some changes and says nowhere
// which ones it dropped.
func TestReserveRefusesPastTheProcessCeiling(t *testing.T) {
	ceiling, err := unix.SysctlUint32(maxPerProc)
	if err != nil {
		t.Skipf("cannot read %s: %v", maxPerProc, err)
	}
	err = Reserve(Plan{Files: int(ceiling) + 1})

	var budget *BudgetError
	if !errors.As(err, &budget) {
		t.Fatalf("Reserve = %v, want a BudgetError", err)
	}
	if budget.Need <= budget.Have {
		t.Errorf("BudgetError says %d needed and %d available", budget.Need, budget.Have)
	}
}
