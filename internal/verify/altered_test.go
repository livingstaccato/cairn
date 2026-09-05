// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for detecting cairn's own output being changed after it was written.

package verify

import (
	"slices"
	"testing"
)

// The gap the digests exist to close. Generated files are in no SHA256SUMS — a
// listing leaves cairn's own output out — and the watcher discards events on
// them by name, which is what stops a rebuild loop. Nothing else can see this.
func TestGeneratedOutputChangedAfterTheBuildIsReported(t *testing.T) {
	f := mirror(t)
	f.outFile("docs/index.json", `{"entries":[]}`)
	f.manifest("docs/index.json")
	f.outFile("docs/index.json", `{"tampered":true}`)

	rep := f.run()
	if !slices.Contains(rep.Altered, "docs/index.json") {
		t.Errorf("Altered = %v, want the edited listing", rep.Altered)
	}
	if rep.OK() {
		t.Error("OK() = true on a tree whose index was rewritten under it")
	}
}

func TestUntouchedGeneratedOutputIsNotReported(t *testing.T) {
	f := mirror(t)
	f.outFile("docs/index.json", `{"entries":[]}`)
	f.manifest("docs/index.json")

	rep := f.run()
	if len(rep.Altered) != 0 {
		t.Errorf("Altered = %v, want nothing", rep.Altered)
	}
	if rep.Compared == 0 {
		t.Error("Compared = 0; a digest was compared, so it was recomputed")
	}
}

// One absence, one heading. A claimed path with nothing behind it is Missing;
// reporting it as Altered too would name a single fault twice and imply two
// different repairs.
func TestAMissingClaimIsNotAlsoAltered(t *testing.T) {
	f := mirror(t)
	f.manifest("docs/index.json")

	rep := f.run()
	if !slices.Contains(rep.Missing, "docs/index.json") {
		t.Fatalf("Missing = %v, want the absent claim", rep.Missing)
	}
	if len(rep.Altered) != 0 {
		t.Errorf("Altered = %v; the same absence was reported twice", rep.Altered)
	}
}
