// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/livingstaccato/cairn/internal/build"
	"github.com/livingstaccato/cairn/internal/emit"
)

// wedge takes away cairn's record of having written the output, leaving the
// output itself in place: every file is one cairn wrote and none of them are
// claimed, which is what on_conflict: error then refuses.
func wedge(t *testing.T, out string) {
	t.Helper()
	p := filepath.Join(out, emit.ManifestFile)
	if err := os.WriteFile(p, []byte(`{"version":1,"outputs":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildAdoptRecoversALostManifest(t *testing.T) {
	configPath, out := fixture(t)
	var stderr strings.Builder
	if err := runBuild(configPath, "", build.Options{}, &stderr); err != nil {
		t.Fatalf("%v, stderr:\n%s", err, stderr.String())
	}
	wedge(t, out)

	// Without the flag the tree stays wedged, which is the state --adopt exists
	// for and the reason it must not be the default.
	stderr.Reset()
	if err := runBuild(configPath, "", build.Options{}, &stderr); err == nil {
		t.Fatal("expected a plain build to refuse output it no longer claims")
	}

	stderr.Reset()
	if err := runBuild(configPath, "", build.Options{Adopt: true}, &stderr); err != nil {
		t.Fatalf("--adopt did not recover the tree: %v, stderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "claimed output cairn had no record of writing") {
		t.Errorf("waiving the conflict check was not reported: %q", stderr.String())
	}

	// And the claim sticks: the next ordinary build needs no flag.
	stderr.Reset()
	if err := runBuild(configPath, "", build.Options{}, &stderr); err != nil {
		t.Errorf("a later ordinary build must own the adopted output: %v", err)
	}
}

// TestBuildDryAdoptReportsAndClaimsNothing: reading the list before waiving the
// check is the reason to have a preview at all.
func TestBuildDryAdoptReportsAndClaimsNothing(t *testing.T) {
	configPath, out := fixture(t)
	var stderr strings.Builder
	if err := runBuild(configPath, "", build.Options{}, &stderr); err != nil {
		t.Fatalf("%v, stderr:\n%s", err, stderr.String())
	}
	wedge(t, out)

	stderr.Reset()
	if err := runBuild(configPath, "", build.Options{Adopt: true, Dry: true}, &stderr); err != nil {
		t.Fatalf("%v, stderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "would claim output cairn has no record of writing") {
		t.Errorf("the preview does not say what it would claim: %q", stderr.String())
	}

	stderr.Reset()
	if err := runBuild(configPath, "", build.Options{}, &stderr); err == nil {
		t.Error("a dry run claimed the output it was only asked to describe")
	}
}

// TestBuildAdoptFlagIsWired goes through the command line rather than calling
// runBuild directly. Every test above would still pass with --adopt bound to
// the wrong field, because they set the struct themselves.
func TestBuildAdoptFlagIsWired(t *testing.T) {
	configPath, out := fixture(t)
	if _, err := exec(t, "build", "--config", configPath); err != nil {
		t.Fatal(err)
	}
	wedge(t, out)

	if _, err := exec(t, "build", "--config", configPath); err == nil {
		t.Fatal("expected a plain build to refuse output it no longer claims")
	}
	if _, err := exec(t, "build", "--config", configPath, "--adopt"); err != nil {
		t.Errorf("--adopt on the command line did not reach the build: %v", err)
	}
}
