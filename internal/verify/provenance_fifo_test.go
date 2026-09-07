// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

//go:build unix

package verify

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestProvenOursDoesNotHangOnAFIFO covers what a missing- or unreadable-file
// check cannot: opening a FIFO for reading blocks until something opens it
// for writing, and nothing here ever will. A generated basename with no
// writer behind it is exactly what --remove-orphaned's walk can meet in the
// wild, and a hang here is a hang of the whole removal.
func TestProvenOursDoesNotHangOnAFIFO(t *testing.T) {
	p := filepath.Join(t.TempDir(), "index.json")
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Skipf("FIFOs unavailable: %v", err)
	}

	done := make(chan bool, 1)
	go func() { done <- provenOurs(p) }()

	select {
	case got := <-done:
		if got {
			t.Error("a FIFO was accepted as proof of authorship")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("provenOurs hung opening a FIFO with no writer")
	}
}
