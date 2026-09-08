// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for what a cancelled ctx does to a verification: stop rather than
// finish reading a mirror nobody is waiting on anymore. checkMissing and
// checkAltered iterate a map, whose order Go leaves undefined, so an
// already-cancelled ctx — stopping before the first iteration does any work,
// regardless of which claim it would have looked at first — is what these
// prove, rather than chasing a specific iteration the way build's per-
// directory walk allows.

package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/livingstaccato/cairndex/internal/hash"
)

// A cancelled ctx makes Run fail outright rather than return a Report,
// since checkMissing — the first thing Run does — cannot tell a manifest
// claim was really missing from one it simply never got to check.
func TestRunFailsOnACancelledCtx(t *testing.T) {
	f := mirror(t)
	f.file("pool/nginx.deb", "deb payload\n")
	f.manifest("pool/index.json")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, f.cfg, f.root, f.out, f.log); err == nil {
		t.Fatal("Run with an already-cancelled ctx must fail rather than report")
	}
}

// checkMissing stops before marking anything missing: every path in
// f.manifest below is genuinely absent from out, so an uncancelled run
// would mark all of them, and this proves none were reached.
func TestCheckMissingStopsOnACancelledCtx(t *testing.T) {
	f := mirror(t)
	f.manifest("a/index.json", "b/index.json", "c/index.json")

	v := &verifier{
		out: f.out, claimed: map[string]string{
			"a/index.json": "x", "b/index.json": "x", "c/index.json": "x",
		},
		missing: map[string]bool{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	v.ctx = ctx

	if err := v.checkMissing(); err == nil {
		t.Fatal("checkMissing with a cancelled ctx must return an error")
	}
	if len(v.missing) != 0 {
		t.Errorf("missing = %v, want none: the cancelled ctx should have stopped before the first check", v.missing)
	}
}

// checkAltered stops before comparing anything: every claim below has a
// digest that will not match what is on disk, so an uncancelled run would
// mark all of them altered, and this proves none were reached.
func TestCheckAlteredStopsOnACancelledCtx(t *testing.T) {
	f := mirror(t)
	f.outFile("a/index.json", `{"path":"/a"}`)
	f.outFile("b/index.json", `{"path":"/b"}`)

	v := &verifier{
		out: f.out, log: f.log,
		claimed: map[string]string{
			"a/index.json": "0000000000000000000000000000000000000000000000000000000000000000",
			"b/index.json": "0000000000000000000000000000000000000000000000000000000000000000",
		},
		missing: map[string]bool{},
		altered: map[string]bool{},
		cache:   hash.NewCache(""),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	v.ctx = ctx

	if err := v.checkAltered(); err == nil {
		t.Fatal("checkAltered with a cancelled ctx must return an error")
	}
	if len(v.altered) != 0 {
		t.Errorf("altered = %v, want none: the cancelled ctx should have stopped before the first check", v.altered)
	}
}

// RemoveOrphaned stops before deleting anything, and the files it would
// otherwise have removed are still there to prove it.
func TestRemoveOrphanedStopsOnACancelledCtx(t *testing.T) {
	f := mirror(t)
	f.outFile("index.json", `{"path":"/"}`)
	rep := &Report{Orphaned: []string{"stray1.txt", "stray2.txt"}, Claims: 1}
	f.outFile("stray1.txt", "cairndex's own signature\n")
	f.outFile("stray2.txt", "cairndex's own signature\n")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := RemoveOrphaned(ctx, f.out, rep)
	if err == nil {
		t.Fatal("RemoveOrphaned with a cancelled ctx must return an error")
	}
	if len(res.Removed) != 0 {
		t.Errorf("Removed = %v, want none", res.Removed)
	}
	for _, name := range []string{"stray1.txt", "stray2.txt"} {
		if _, statErr := os.Stat(filepath.Join(f.out, name)); statErr != nil {
			t.Errorf("%s was removed despite the cancelled ctx: %v", name, statErr)
		}
	}
}
