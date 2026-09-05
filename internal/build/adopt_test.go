// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/livingstaccato/cairn/internal/emit"
	"github.com/livingstaccato/cairn/internal/obs"
)

// loseTheManifest leaves the output in place and takes away cairn's record of
// having written it — the state a build that died before SavePartial could
// finish leaves behind, and the state that has no way out.
func loseTheManifest(t *testing.T, out string) {
	t.Helper()
	p := filepath.Join(out, emit.ManifestFile)
	if err := os.WriteFile(p, []byte(`{"version":1,"outputs":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestWithoutAdoptALostManifestStillStopsTheBuild is the state this recovers
// from. It is not a hypothetical: every file in the output is one cairn wrote,
// and on_conflict: error refuses all of them, naming a path and saying it
// already exists.
func TestWithoutAdoptALostManifestStillStopsTheBuild(t *testing.T) {
	root, out := tree(t), t.TempDir()
	run(t, conf(nil), root, out)
	loseTheManifest(t, out)

	if _, err := Run(conf(nil), root, out, obs.Discard()); err == nil {
		t.Fatal("expected the build to refuse output it no longer claims")
	}
}

// TestAdoptRecoversATreeWhoseManifestWasLost: the only escapes before this were
// deleting the output — impossible when root: and out: are the same directory,
// because that deletes the artifacts — or on_conflict: skip, which then writes
// nothing and prunes nothing, freezing the mirror for good.
func TestAdoptRecoversATreeWhoseManifestWasLost(t *testing.T) {
	root, out := tree(t), t.TempDir()
	first := run(t, conf(nil), root, out)
	loseTheManifest(t, out)

	res, err := RunWith(conf(nil), root, out, obs.Discard(), Options{Adopt: true})
	if err != nil {
		t.Fatalf("--adopt must reclaim cairn's own output: %v", err)
	}
	if len(res.Adopted) != len(first.Written) {
		t.Errorf("adopted %d paths, want the %d the first build wrote",
			len(res.Adopted), len(first.Written))
	}

	// The claim has to stick, or the next ordinary run refuses the same files.
	if _, err := Run(conf(nil), root, out, obs.Discard()); err != nil {
		t.Errorf("a later ordinary build must own the adopted output: %v", err)
	}
}

// TestAdoptClaimsNothingWhenThereIsNothingToClaim: on a healthy tree the flag
// is inert, so leaving it in a script does not quietly disarm the conflict
// check for files that turn up later.
func TestAdoptClaimsNothingWhenThereIsNothingToClaim(t *testing.T) {
	root, out := tree(t), t.TempDir()
	run(t, conf(nil), root, out)

	res, err := RunWith(conf(nil), root, out, obs.Discard(), Options{Adopt: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Adopted) != 0 {
		t.Errorf("Adopted = %v, want nothing on a tree cairn already owns", res.Adopted)
	}
}

// TestADryAdoptReportsWhatItWouldClaim: the two compose, and reading the list
// before waiving the conflict check is the reason to.
func TestADryAdoptReportsWhatItWouldClaim(t *testing.T) {
	root, out := tree(t), t.TempDir()
	first := run(t, conf(nil), root, out)
	loseTheManifest(t, out)

	res, err := RunWith(conf(nil), root, out, obs.Discard(), Options{Adopt: true, Dry: true})
	if err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	if len(res.Adopted) != len(first.Written) {
		t.Errorf("would adopt %d paths, want %d", len(res.Adopted), len(first.Written))
	}

	// Nothing was claimed, so the tree is still wedged and a plain build still
	// refuses it. A preview that quietly fixed things would be no preview.
	if _, err := Run(conf(nil), root, out, obs.Discard()); err == nil {
		t.Error("a dry run claimed the output it was only asked to describe")
	}
}
