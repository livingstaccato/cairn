// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The initial build is not optional. A watcher knows nothing about what changed
// before it started, so whatever was already stale would stay stale until
// something touched it again.
func TestWatchBuildsBeforeItWatches(t *testing.T) {
	configPath, out := fixture(t)

	// Cancelled before the watch loop reads its first event, so the test
	// asserts on the build the command runs on its way in.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var stderr strings.Builder
	if err := runWatch(ctx, watchOpts{configPath: configPath, settle: 10 * time.Millisecond}, &stderr); err != nil {
		t.Fatalf("%v, stderr:\n%s", err, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(out, "bootstrap", "index.json")); err != nil {
		t.Errorf("watch did not build before watching: %v", err)
	}
}

// A watch that cannot build cannot watch: the manifest recording what cairn
// owns is written by the build, and watching without it would rebuild against
// output nothing claims.
func TestWatchFailsWhenTheFirstBuildFails(t *testing.T) {
	configPath, _ := fixture(t)
	if err := os.WriteFile(configPath, []byte(
		"version: 1\nroot: ./tree\nout: ./out\ndefaults:\n  outputs: [pdf]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	if err := runWatch(context.Background(), watchOpts{configPath: configPath, settle: time.Millisecond}, &stderr); err == nil {
		t.Fatal("a failing initial build must be an error")
	}
}

func TestWatchMissingConfigFails(t *testing.T) {
	var stderr strings.Builder
	err := runWatch(context.Background(),
		watchOpts{configPath: filepath.Join(t.TempDir(), "nope.yaml"), settle: time.Millisecond}, &stderr)
	if err == nil {
		t.Fatal("missing config must be an error")
	}
	if stderr.Len() == 0 {
		t.Error("expected an explanatory message on stderr")
	}
}

func TestWatchRejectsPositionalArgs(t *testing.T) {
	if _, err := exec(t, cmdWatch, "somewhere"); err == nil {
		t.Fatal("watch must reject positional arguments")
	}
}

// The tree ships with the command, under the name and shorthand the flags say.
func TestRootCommandHasWatch(t *testing.T) {
	for _, c := range newRootCmd().Commands() {
		if c.Name() != cmdWatch {
			continue
		}
		if c.Flags().Lookup("settle") == nil {
			t.Error("watch has no --settle flag")
		}
		if c.Flags().ShorthandLookup("c") == nil {
			t.Error("watch has no -c shorthand for --config")
		}
		return
	}
	t.Error("root command has no watch subcommand")
}
