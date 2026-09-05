// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for RunDry: a build that decides everything and changes nothing.

package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/emit"
	"github.com/livingstaccato/cairn/internal/hash"
	"github.com/livingstaccato/cairn/internal/obs"
)

func dry(t *testing.T, c *config.Config, root, out string) *Result {
	t.Helper()
	res, err := RunDry(c, root, out, obs.Discard())
	if err != nil {
		t.Fatalf("RunDry: %v", err)
	}
	return res
}

// The whole promise: the output directory is untouched afterwards.
func TestDryRunWritesNothing(t *testing.T) {
	root, out := tree(t), t.TempDir()

	res := dry(t, conf(nil), root, out)

	if len(res.Written) == 0 {
		t.Fatal("a dry run that reports no outputs is reporting nothing at all")
	}
	ents, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		t.Errorf("dry run left %d entries in the output directory: %v", len(ents), ents)
	}
}

// Not even the bookkeeping. A manifest or a hash cache appearing under out: is
// still a dry run having changed something.
func TestDryRunWritesNoSidecars(t *testing.T) {
	root, out := tree(t), t.TempDir()

	dry(t, conf(nil), root, out)

	for _, name := range []string{emit.ManifestFile, hash.CacheFile} {
		if _, err := os.Stat(filepath.Join(out, name)); err == nil {
			t.Errorf("dry run wrote %s", name)
		}
	}
}

// A dry run over a tree with no output yet reports everything as changed. The
// alternative — reporting a file as unchanged because nothing is there to
// differ from — would tell an operator a first build has nothing to do.
func TestDryRunCountsEverythingAsChangedOnAnEmptyOutput(t *testing.T) {
	root, out := tree(t), t.TempDir()

	res := dry(t, conf(nil), root, out)

	if len(res.Changed) != len(res.Written) {
		t.Errorf("Changed = %d, Written = %d; want them equal with no output on disk",
			len(res.Changed), len(res.Written))
	}
	if res.Unchanged != 0 {
		t.Errorf("Unchanged = %d, want 0", res.Unchanged)
	}
}

// After a real build the same dry run reports nothing changed, because every
// body it would write is already on disk. That is the check that a dry run
// compares against reality rather than assuming it.
func TestDryRunReportsNoChangeAfterARealBuild(t *testing.T) {
	root, out := tree(t), t.TempDir()
	run(t, conf(nil), root, out)

	res := dry(t, conf(nil), root, out)

	if len(res.Changed) != 0 {
		t.Errorf("Changed = %v, want none after an identical build", res.Changed)
	}
	if res.Unchanged != len(res.Written) {
		t.Errorf("Unchanged = %d, want all %d outputs", res.Unchanged, len(res.Written))
	}
}

// The reason the flag exists. Deleting published artifacts is the one thing
// cairn does that running it again cannot undo, so an operator has to be able
// to see the list first.
func TestDryRunNamesWhatPruneWouldRemoveWithoutRemovingIt(t *testing.T) {
	root, out := tree(t), t.TempDir()
	run(t, conf(nil), root, out)

	stale := filepath.Join(out, "docs", "index.json")
	if _, err := os.Stat(stale); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "docs")); err != nil {
		t.Fatal(err)
	}

	res := dry(t, conf(nil), root, out)

	var named bool
	for _, p := range res.Pruned {
		if p == "docs/index.json" {
			named = true
		}
	}
	if !named {
		t.Errorf("Pruned = %v, want it to name docs/index.json", res.Pruned)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Errorf("dry run deleted the output it was only asked about: %v", err)
	}
}

// A dry run must not disown what the last real build claimed. Rewriting the
// manifest with this run's view would leave the next real build refusing every
// file as somebody else's, or pruning what it should keep.
func TestDryRunLeavesThePreviousManifestAlone(t *testing.T) {
	root, out := tree(t), t.TempDir()
	run(t, conf(nil), root, out)

	mf := filepath.Join(out, emit.ManifestFile)
	before, err := os.ReadFile(mf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "docs")); err != nil {
		t.Fatal(err)
	}

	dry(t, conf(nil), root, out)

	after, err := os.ReadFile(mf)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("the dry run rewrote the manifest")
	}
}
