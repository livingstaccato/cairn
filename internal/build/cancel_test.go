// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for what a cancelled ctx does mid-build: stop at the next directory
// boundary and still claim what was written, rather than either running to
// completion regardless or leaving partial output nobody owns.

package build

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/livingstaccato/cairndex/internal/emit"
	"github.com/livingstaccato/cairndex/internal/obs"
)

// TestCancelledCtxStopsTheWalkAndSavesAPartialManifest cancels ctx from
// visited, a hook fired once per directory, rather than racing wall-clock
// time against however long the fixture takes to build — the same
// determinism reason cmd/cairndex's afterSignalRegistered test hook exists.
//
// tree(t)'s entries sort bootstrap before docs, so visit order is ".",
// "bootstrap", "bootstrap/linux", "docs". Cancelling as the second visit
// starts lets "." and "bootstrap" finish — their own ctx check already
// passed — and stops before "bootstrap/linux" or "docs" are ever reached.
func TestCancelledCtxStopsTheWalkAndSavesAPartialManifest(t *testing.T) {
	root, out := tree(t), t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())

	seen := 0
	orig := visited
	visited = func(string) {
		seen++
		if seen == 2 {
			cancel()
		}
	}
	t.Cleanup(func() { visited = orig })

	_, err := RunWith(ctx, conf(nil), root, out, obs.Discard(), Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunWith = %v, want context.Canceled", err)
	}

	if _, statErr := os.Stat(filepath.Join(out, emit.ManifestFile)); statErr != nil {
		t.Errorf("a cancelled build did not save a partial manifest: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(out, "bootstrap", "index.json")); statErr != nil {
		t.Errorf("bootstrap's own visit had already passed its ctx check and should have finished: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(out, "docs", "index.json")); statErr == nil {
		t.Error("docs was built after cancellation; the walk did not actually stop early")
	}
}

// buildScoped's own ancestor-refresh loop gets the same check as visit,
// separately: cancelling right as the scope itself finishes — after visit's
// own check already passed it — has nowhere left to be caught but the loop
// that refreshes bootstrap and "." above it. Without that loop's own check
// this would return nil: refresh does not consult ctx either.
func TestCancelledCtxStopsTheScopedAncestorRefresh(t *testing.T) {
	root, out := tree(t), t.TempDir()
	if _, err := RunWith(context.Background(), conf(nil), root, out, obs.Discard(), Options{}); err != nil {
		t.Fatalf("initial build: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	orig := visited
	visited = func(relDir string) {
		if relDir == "bootstrap/linux" {
			cancel()
		}
	}
	t.Cleanup(func() { visited = orig })

	_, err := RunScoped(ctx, conf(nil), root, out, obs.Discard(), "bootstrap/linux", "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunScoped = %v, want context.Canceled", err)
	}
}
