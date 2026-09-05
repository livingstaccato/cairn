// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/livingstaccato/cairn/internal/build"
)

// --dry-run leaves the output directory exactly as it found it.
func TestBuildDryRunWritesNothing(t *testing.T) {
	configPath, out := fixture(t)

	var stderr strings.Builder
	if err := runBuild(configPath, "", build.Options{Dry: true}, &stderr); err != nil {
		t.Fatalf("%v, stderr:\n%s", err, stderr.String())
	}

	if _, err := os.Stat(filepath.Join(out, "bootstrap", "index.json")); err == nil {
		t.Error("--dry-run wrote a listing")
	}
	if !strings.Contains(stderr.String(), "nothing was written") {
		t.Errorf("the summary does not say the run was dry: %q", stderr.String())
	}
}

// The list of what would move is still written where the operator asked for
// it: --dry-run guards the output tree, and previewing a deployment's file
// list is the reason to ask for both at once.
func TestBuildDryRunStillWritesTheChangedList(t *testing.T) {
	configPath, out := fixture(t)
	list := filepath.Join(t.TempDir(), "changed.txt")

	var stderr strings.Builder
	if err := runBuild(configPath, list, build.Options{Dry: true}, &stderr); err != nil {
		t.Fatalf("%v, stderr:\n%s", err, stderr.String())
	}

	body, err := os.ReadFile(list)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "bootstrap/index.json") {
		t.Errorf("the changed list does not name what a build would write: %q", body)
	}
	if _, err := os.Stat(filepath.Join(out, "bootstrap", "index.json")); err == nil {
		t.Error("--dry-run wrote a listing")
	}
}

// The deletions a real run would make are named, and not made.
func TestBuildDryRunReportsWhatPruneWouldRemove(t *testing.T) {
	configPath, out := fixture(t)
	var stderr strings.Builder
	if err := runBuild(configPath, "", build.Options{}, &stderr); err != nil {
		t.Fatalf("%v, stderr:\n%s", err, stderr.String())
	}

	tree := filepath.Join(filepath.Dir(configPath), "tree")
	if err := os.RemoveAll(filepath.Join(tree, "bootstrap")); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(out, "bootstrap", "index.json")

	stderr.Reset()
	if err := runBuild(configPath, "", build.Options{Dry: true}, &stderr); err != nil {
		t.Fatalf("%v, stderr:\n%s", err, stderr.String())
	}

	if !strings.Contains(stderr.String(), "would remove stale output") {
		t.Errorf("the run did not report the deletions it would make: %q", stderr.String())
	}
	if _, err := os.Stat(stale); err != nil {
		t.Errorf("--dry-run deleted output it was only asked about: %v", err)
	}
}

func TestBuildHasADryRunFlag(t *testing.T) {
	for _, c := range newRootCmd().Commands() {
		if c.Name() != cmdBuild {
			continue
		}
		if c.Flags().Lookup("dry-run") == nil {
			t.Error("build has no --dry-run flag")
		}
		return
	}
	t.Error("root command has no build subcommand")
}
