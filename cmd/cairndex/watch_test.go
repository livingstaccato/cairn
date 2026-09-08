// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/livingstaccato/cairndex/internal/build"
)

// The initial build is not optional. A watcher knows nothing about what changed
// before it started, so whatever was already stale would stay stale until
// something touched it again.
func TestWatchBuildsBeforeItWatches(t *testing.T) {
	configPath, out := fixture(t)

	// build.Run observes ctx now, so a pre-cancelled one would stop the
	// build too instead of isolating just the watch loop after it.
	// afterInitialBuild cancels only once the build this test cares about
	// has already succeeded.
	ctx, cancel := context.WithCancel(context.Background())
	built := make(chan struct{})
	orig := afterInitialBuild
	afterInitialBuild = func() { close(built); cancel() }
	t.Cleanup(func() { afterInitialBuild = orig })

	var stderr strings.Builder
	done := make(chan error, 1)
	go func() {
		done <- runWatch(ctx, watchOpts{configPath: configPath, settle: 10 * time.Millisecond}, &stderr)
	}()

	select {
	case <-built:
	case <-time.After(5 * time.Second):
		t.Fatal("the initial build never finished")
	}

	if err := <-done; err != nil {
		t.Fatalf("%v, stderr:\n%s", err, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(out, "bootstrap", "index.json")); err != nil {
		t.Errorf("watch did not build before watching: %v", err)
	}
}

// A watch that cannot build cannot watch: the manifest recording what cairndex
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

// checkRoot exists to improve on an opaque syscall error, so it must not
// replace one opaque answer with a confidently wrong one.
//
// Every os.Stat failure became "does not exist" and the underlying error was
// discarded. A root: whose parent denies traversal is right there on disk, and
// the operator was told to fix a path that was already correct — as was the case
// for a symlink loop, or a non-directory partway along the path.
func TestCheckRootSaysWhyWhenItIsNotAbsence(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root traverses a 0o000 directory, so there is no denial to report")
	}
	base := t.TempDir()
	locked := filepath.Join(base, "locked")
	if err := os.MkdirAll(filepath.Join(locked, "tree"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	// Restored so the temp directory can be cleaned up.
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	err := checkRoot("./locked/tree", filepath.Join(locked, "tree"))
	if err == nil {
		t.Fatal("a root: that cannot be read must be refused")
	}
	if strings.Contains(err.Error(), "does not exist") {
		t.Errorf("a permission failure was reported as absence: %v", err)
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("the underlying cause was discarded: %v", err)
	}
	if !strings.Contains(err.Error(), "./locked/tree") {
		t.Errorf("the message does not name the setting as it was written: %v", err)
	}
}

// A root: that genuinely is not there still says so, in the words that sent an
// operator to the right line of the config.
func TestCheckRootStillReportsRealAbsence(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	err := checkRoot("./nope", missing)
	if err == nil {
		t.Fatal("a missing root: must be refused")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("a missing directory should say so: %v", err)
	}
}

// A root: that is not there produced "read dir .: open nope: no such file or
// directory" -- which names neither the setting that is wrong nor the path it
// resolved to, and leads with a "." that reads like part of the mistake. It is
// the first error a new config produces and it should say what to edit.
func TestAMissingRootNamesTheSettingAndThePath(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "cairndex.yaml")
	body := "version: 1\nroot: ./nope\nout: ./site\n"
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	var stderr strings.Builder
	err := runBuild(context.Background(), configPath, "", build.Options{}, &stderr)
	if err == nil {
		t.Fatal("a build whose root: does not exist must fail")
	}
	for _, want := range []string{"root:", "./nope"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "read dir .") {
		t.Errorf("error still leads with the walk's relative dir: %v", err)
	}
}

// A root: that is a file, not a directory, is the same class of mistake and was
// reported as an opaque syscall error too.
func TestARootThatIsAFileIsRefusedClearly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tree"), []byte("not a directory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "cairndex.yaml")
	if err := os.WriteFile(configPath, []byte("version: 1\nroot: ./tree\nout: ./site\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stderr strings.Builder
	err := runBuild(context.Background(), configPath, "", build.Options{}, &stderr)
	if err == nil {
		t.Fatal("a root: pointing at a file must fail")
	}
	if !strings.Contains(err.Error(), "root:") || !strings.Contains(err.Error(), "directory") {
		t.Errorf("error should say root: must be a directory: %v", err)
	}
}
