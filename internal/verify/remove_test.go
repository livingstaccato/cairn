// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package verify

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/livingstaccato/cairndex/internal/emit"
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
	// csv left outputs: a release ago. Cairndex's own bytes, because that is what
	// makes it removable — a file merely named index.csv is kept.
	f.outFile("pool/index.csv", string(mustBytes(emit.CSV(sampleListing()))))
	f.manifest("pool/index.json")

	rep := f.run()
	res, err := RemoveOrphaned(context.Background(), f.out, rep)
	if err != nil {
		t.Fatalf("RemoveOrphaned: %v", err)
	}
	if len(res.Removed) != 1 || res.Removed[0] != "pool/index.csv" {
		t.Errorf("Removed = %v, want [pool/index.csv]", res.Removed)
	}
	if f.exists("pool/index.csv") {
		t.Error("the orphan is still there")
	}
	if !f.exists("pool/index.json") {
		t.Error("removed output cairndex owns")
	}
	if _, err := os.Lstat(filepath.Join(f.root, "pool/nginx.deb")); err != nil {
		t.Error("removed an artifact")
	}
}

// The guard that matters. A manifest claiming nothing means cairndex owns nothing,
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
	res, err := RemoveOrphaned(context.Background(), f.out, rep)
	if err == nil {
		t.Fatal("removing every output because the manifest was lost must be refused")
	}
	if !strings.Contains(err.Error(), "--adopt") {
		t.Errorf("the refusal should point at the recovery: %v", err)
	}
	if len(res.Removed) != 0 {
		t.Errorf("removed %v despite refusing", res.Removed)
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
	if err := os.WriteFile(outside, []byte("not cairndex's\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := f.run()
	rep.Orphaned = []string{"../outside.json"}

	if _, err := RemoveOrphaned(context.Background(), f.out, rep); err == nil {
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

	res, err := RemoveOrphaned(context.Background(), f.out, f.run())
	if err != nil {
		t.Fatalf("a clean tree must not error: %v", err)
	}
	if len(res.Removed) != 0 {
		t.Errorf("Removed = %v, want nothing", res.Removed)
	}
}

// Nothing to remove and no manifest either: an output tree that has not been
// built yet. The claims guard is about refusing a destructive act on bad
// information, and there is no act here to refuse.
func TestRemoveOrphanedWithNothingToDoDoesNotTripTheGuard(t *testing.T) {
	f := mirror(t)
	f.file("pool/nginx.deb", "deb\n")

	res, err := RemoveOrphaned(context.Background(), f.out, f.run())
	if err != nil {
		t.Fatalf("an unbuilt tree has nothing to remove and must not error: %v", err)
	}
	if len(res.Removed) != 0 {
		t.Errorf("Removed = %v, want nothing", res.Removed)
	}
}

// The data-loss case, end to end.
//
// A mirror is root and out in one directory, so nearly every file in it is
// somebody's artifact, and the names cairndex generates are the most ordinary
// names in a published tree. GeneratedNames covers all four extensions whatever
// outputs: is set, so cairndex need never have written HTML into this tree — which
// also means emit.Writer's conflict check, the guard that stops cairndex walking
// over a file it did not write, never fires on these paths.
//
// Reported: correct, and the report is what a person reads. Deleted: a mirrored
// package index and an extracted documentation tree, gone, with the command
// logging success and exiting zero.
func TestRemoveOrphanedKeepsForeignFilesWearingGeneratedNames(t *testing.T) {
	f := mirror(t)
	f.file("simple/requests/index.html", "<a href='requests-2.0.tar.gz'>requests</a>\n")
	f.file("docs/api/index.html", "<h1>API reference</h1>\n")
	// Real cairndex output in the same tree, so the manifest is not empty and the
	// claims guard does not fire. A normal mirror has thousands of these.
	f.outFile("simple/requests/index.json", "{}")
	f.manifest("simple/requests/index.json")

	rep := f.run()
	foreign := []string{"docs/api/index.html", "simple/requests/index.html"}
	for _, rel := range foreign {
		if !slices.Contains(rep.Orphaned, rel) {
			t.Fatalf("Orphaned = %v, want it to report %s: the premise of this test",
				rep.Orphaned, rel)
		}
	}

	res, err := RemoveOrphaned(context.Background(), f.out, rep)
	if err != nil {
		t.Fatalf("RemoveOrphaned: %v", err)
	}
	if len(res.Removed) != 0 {
		t.Errorf("deleted %v; nothing in this tree is cairndex's", res.Removed)
	}
	if !slices.Equal(res.Kept, foreign) {
		t.Errorf("Kept = %v, want %v", res.Kept, foreign)
	}
	for _, rel := range foreign {
		if _, err := os.Lstat(filepath.Join(f.root, filepath.FromSlash(rel))); err != nil {
			t.Errorf("deleted a foreign artifact: %s", rel)
		}
	}
}

// A kept path is still a finding. Reporting it and then removing it from the
// report would tell an operator the tree is intact when a name collision they
// have to settle is sitting in it.
func TestKeptOrphansAreStillReported(t *testing.T) {
	f := mirror(t)
	f.file("simple/requests/index.html", "<a href='x'>x</a>\n")
	f.outFile("simple/requests/index.json", "{}")
	f.manifest("simple/requests/index.json")

	res, err := RemoveOrphaned(context.Background(), f.out, f.run())
	if err != nil {
		t.Fatalf("RemoveOrphaned: %v", err)
	}
	if len(res.Kept) == 0 {
		t.Fatal("the collision was neither removed nor kept, so nothing reports it")
	}
}

// Stale output cairndex really did write is still removed: the feature exists for
// exactly this, and a fix that only ever refuses would be a fix that broke it.
func TestRemoveOrphanedStillRemovesCairndexsOwnStaleOutput(t *testing.T) {
	f := mirror(t)
	l := sampleListing()
	f.outFile("pool/index.json", "{}")
	f.outFile("pool/tree.csv", string(mustBytes(emit.CSV(l))))
	f.outFile("pool/index.html", string(mustBytes(emit.BareHTML(emit.BarePage{Listing: l}))))
	f.manifest("pool/index.json")

	res, err := RemoveOrphaned(context.Background(), f.out, f.run())
	if err != nil {
		t.Fatalf("RemoveOrphaned: %v", err)
	}
	want := []string{"pool/index.html", "pool/tree.csv"}
	if !slices.Equal(res.Removed, want) {
		t.Errorf("Removed = %v, want %v", res.Removed, want)
	}
	if len(res.Kept) != 0 {
		t.Errorf("Kept = %v, want nothing: every one of these is cairndex's", res.Kept)
	}
}

// The sweep has to report what it did and finish what it started.
//
// It returned nothing, so a directory vanished with no record anywhere that
// anything had removed it — and in a mirror that is a directory of the published
// artifact tree. It also swept only immediate parents, so removing
// a/b/c/index.csv took a/b/c and left a/b and a standing empty behind it.
func TestRemoveOrphanedReportsAndFullySweepsEmptiedDirectories(t *testing.T) {
	f := mirror(t)
	l := sampleListing()
	f.outFile("keep/index.json", "{}")
	f.outFile("a/b/c/index.csv", string(mustBytes(emit.CSV(l))))
	f.manifest("keep/index.json")

	res, err := RemoveOrphaned(context.Background(), f.out, f.run())
	if err != nil {
		t.Fatalf("RemoveOrphaned: %v", err)
	}
	if !slices.Equal(res.Removed, []string{"a/b/c/index.csv"}) {
		t.Fatalf("Removed = %v, want [a/b/c/index.csv]", res.Removed)
	}
	want := []string{"a", "a/b", "a/b/c"}
	if !slices.Equal(res.RemovedDirs, want) {
		t.Errorf("RemovedDirs = %v, want %v", res.RemovedDirs, want)
	}
	for _, d := range want {
		if f.exists(d) {
			t.Errorf("%s was left standing empty", d)
		}
	}
	// The sweep stops at anything still holding something.
	if !f.exists("keep") || !f.exists("keep/index.json") {
		t.Error("the sweep removed a directory that still had something in it")
	}
}

// The output root is never swept away, however empty the removals leave it.
func TestRemoveOrphanedNeverSweepsTheOutputRoot(t *testing.T) {
	f := mirror(t)
	f.outFile("index.csv", string(mustBytes(emit.CSV(sampleListing()))))
	f.outFile("index.json", "{}")
	f.manifest("index.json")

	res, err := RemoveOrphaned(context.Background(), f.out, f.run())
	if err != nil {
		t.Fatalf("RemoveOrphaned: %v", err)
	}
	if len(res.RemovedDirs) != 0 {
		t.Errorf("RemovedDirs = %v, want nothing: the only parent is the root", res.RemovedDirs)
	}
	if _, err := os.Lstat(f.out); err != nil {
		t.Fatal("the output root itself was removed")
	}
}

// The race containedIn's own doc leaves to callers: rootAbs is resolved once
// for the whole removal, but nothing re-checks an intermediate directory
// between then and the moment a file under it is read or deleted. Swap that
// directory for a symlink pointing outside outAbs in between, and a lexically
// contained path stops describing what it names.
//
// This is not a live race — Go's stdlib has no portable, no-follow,
// descriptor-relative open, so a real concurrent swap cannot be closed outright
// on every platform this builds for. What can be tested is the state the race
// produces: an ancestor that has become a symlink by the time the file under it
// is acted on.
func TestRemoveOrphanedRefusesWhenAnAncestorBecameASymlink(t *testing.T) {
	f := mirror(t)
	l := sampleListing()
	f.outFile("a/b/index.csv", string(mustBytes(emit.CSV(l))))
	f.outFile("keep/index.json", "{}")
	f.manifest("keep/index.json")

	rep := f.run()
	if !slices.Contains(rep.Orphaned, "a/b/index.csv") {
		t.Fatalf("Orphaned = %v, want a/b/index.csv: the premise of this test", rep.Orphaned)
	}

	// The swap: "a" becomes a symlink to a directory that mirrors the relative
	// shape the orphan path expects — b/index.csv — so the resolved path
	// through the symlink names a real, deletable file, and cairndex's own bytes
	// at it so provenOurs would accept it if the swap is not caught first.
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(outside, "b", "index.csv")
	if err := os.WriteFile(victim, []byte(header(t)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(f.out, "a")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(f.out, "a")); err != nil {
		t.Fatal(err)
	}

	res, err := RemoveOrphaned(context.Background(), f.out, rep)
	if err == nil && slices.Contains(res.Removed, "a/b/index.csv") {
		t.Error("reported a/b/index.csv removed while acting through a swapped ancestor")
	}
	if _, statErr := os.Lstat(victim); statErr != nil {
		t.Error("a file outside the output root was removed through a swapped ancestor")
	}
}

// header is cairndex's real CSV header, for a body a foreign path can carry to
// look like cairndex's own output without going through the emitter.
func header(t *testing.T) string {
	t.Helper()
	return string(mustBytes(emit.CSV(sampleListing())))
}
