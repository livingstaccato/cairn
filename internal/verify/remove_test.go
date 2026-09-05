// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package verify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func (f *fixture) exists(rel string) bool {
	f.t.Helper()
	_, err := os.Lstat(filepath.Join(f.out, filepath.FromSlash(rel)))
	return err == nil
}

// check reported stale output and nothing could act on it: after an --adopt
// recovery, output from an earlier config stayed unclaimed and unprunable
// forever, because Prune only ever removes what the manifest records.
func TestRemoveOrphanedDeletesWhatWasReported(t *testing.T) {
	f := mirror(t)
	f.file("pool/nginx.deb", "deb\n")
	f.outFile("pool/index.json", "{}")
	f.outFile("pool/index.csv", "name,size\n") // csv left outputs: a release ago
	f.manifest("pool/index.json")

	rep := f.run()
	removed, err := RemoveOrphaned(f.out, rep)
	if err != nil {
		t.Fatalf("RemoveOrphaned: %v", err)
	}
	if len(removed) != 1 || removed[0] != "pool/index.csv" {
		t.Errorf("removed = %v, want [pool/index.csv]", removed)
	}
	if f.exists("pool/index.csv") {
		t.Error("the orphan is still there")
	}
	if !f.exists("pool/index.json") {
		t.Error("removed output cairn owns")
	}
	if _, err := os.Lstat(filepath.Join(f.root, "pool/nginx.deb")); err != nil {
		t.Error("removed an artifact")
	}
}

// The guard that matters. A manifest claiming nothing means cairn owns nothing,
// so every generated-looking file reads as an orphan -- and removing them would
// delete the entire published output. That state is a lost manifest, which
// build --adopt exists to repair, not a tree that is genuinely all foreign.
func TestRemoveOrphanedRefusesWhenTheManifestClaimsNothing(t *testing.T) {
	f := mirror(t)
	f.outFile("pool/index.json", "{}")
	f.outFile("pool/index.csv", "name,size\n")
	// No manifest at all: loadManifest reports no claims and no error.

	rep := f.run()
	if len(rep.Orphaned) != 2 {
		t.Fatalf("Orphaned = %v, want both files: the premise of this test", rep.Orphaned)
	}
	removed, err := RemoveOrphaned(f.out, rep)
	if err == nil {
		t.Fatal("removing every output because the manifest was lost must be refused")
	}
	if !strings.Contains(err.Error(), "--adopt") {
		t.Errorf("the refusal should point at the recovery: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("removed %v despite refusing", removed)
	}
	if !f.exists("pool/index.json") || !f.exists("pool/index.csv") {
		t.Error("files were deleted despite the refusal")
	}
}

// Containment is not a policy here either: a report is data, and a path in one
// must not be able to reach outside the output root.
func TestRemoveOrphanedStaysUnderTheOutputRoot(t *testing.T) {
	f := mirror(t)
	f.outFile("pool/index.json", "{}")
	f.manifest("pool/index.json")

	outside := filepath.Join(filepath.Dir(f.out), "outside.json")
	if err := os.WriteFile(outside, []byte("not cairn's\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := f.run()
	rep.Orphaned = []string{"../outside.json"}

	if _, err := RemoveOrphaned(f.out, rep); err == nil {
		t.Error("a path resolving outside the output root must be refused")
	}
	if _, err := os.Lstat(outside); err != nil {
		t.Error("removed a file outside the output root")
	}
}

// Nothing to do is not an error, and must not trip the claims guard on a tree
// that is simply intact.
func TestRemoveOrphanedOnACleanTreeDoesNothing(t *testing.T) {
	f := mirror(t)
	f.outFile("pool/index.json", "{}")
	f.manifest("pool/index.json")

	removed, err := RemoveOrphaned(f.out, f.run())
	if err != nil {
		t.Fatalf("a clean tree must not error: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want nothing", removed)
	}
}

// Nothing to remove and no manifest either: an output tree that has not been
// built yet. The claims guard is about refusing a destructive act on bad
// information, and there is no act here to refuse.
func TestRemoveOrphanedWithNothingToDoDoesNotTripTheGuard(t *testing.T) {
	f := mirror(t)
	f.file("pool/nginx.deb", "deb\n")

	removed, err := RemoveOrphaned(f.out, f.run())
	if err != nil {
		t.Fatalf("an unbuilt tree has nothing to remove and must not error: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want nothing", removed)
	}
}
