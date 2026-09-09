// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/livingstaccato/cairndex/internal/build"
)

// The yaml-language-server directive is what gives an editor inline
// autocomplete and validation before a build ever runs. It has to be a
// comment on its own — a YAML comment line, not a key — or it stops meaning
// anything to the editor and starts meaning something to the decoder.
func TestInitConfigCarriesTheSchemaDirective(t *testing.T) {
	if !strings.HasPrefix(starterConfigDirect, "# yaml-language-server: $schema=") {
		t.Errorf("starterConfigDirect does not open with the schema directive:\n%s", starterConfigDirect)
	}
}

// The test that matters: what init writes has to build. A starter config that
// needs editing before it works is a worse start than no starter config, and
// the decoder is strict now, so a stale key here would refuse outright.
func TestInitWritesAConfigThatBuilds(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, DefaultConfigFile)
	if err := os.MkdirAll(filepath.Join(dir, "tree", "pool"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tree", "pool", "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stderr strings.Builder
	if err := runInit(configPath, "", &stderr); err != nil {
		t.Fatalf("init: %v, stderr:\n%s", err, stderr.String())
	}
	if err := runBuild(context.Background(), configPath, "", build.Options{}, &stderr); err != nil {
		t.Fatalf("a build of the config init wrote must succeed: %v\nstderr:\n%s", err, stderr.String())
	}

	// A browsable page is the point of the defaults it picks.
	for _, rel := range []string{"index.html", filepath.Join("pool", "index.html")} {
		if _, err := os.Stat(filepath.Join(dir, "site", rel)); err != nil {
			t.Errorf("the starter config produced no %s", rel)
		}
	}
}

// --mode hugo writes a different starter: no present:/outputs: to pick, since
// Hugo renders the HTML, and a reminder to import the module first — a
// config that otherwise fails on its first build with no clue why.
func TestInitModeHugoWritesAConfigThatBuilds(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, DefaultConfigFile)
	if err := os.MkdirAll(filepath.Join(dir, "tree", "pool"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tree", "pool", "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stderr strings.Builder
	if err := runInit(configPath, "hugo", &stderr); err != nil {
		t.Fatalf("init: %v, stderr:\n%s", err, stderr.String())
	}
	if err := runBuild(context.Background(), configPath, "", build.Options{}, &stderr); err != nil {
		t.Fatalf("a build of the hugo starter must succeed: %v\nstderr:\n%s", err, stderr.String())
	}
	body, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "mode: hugo") {
		t.Errorf("the hugo starter does not set mode: hugo:\n%s", body)
	}
	if !strings.Contains(string(body), "out:  ./content") {
		t.Errorf("the hugo starter does not point out: at content/:\n%s", body)
	}
	if _, err := os.Stat(filepath.Join(dir, "content", "pool", "_index.md")); err != nil {
		t.Error("the hugo starter produced no branch bundle for pool/")
	}
}

func TestInitRejectsAnUnknownMode(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), DefaultConfigFile)
	var stderr strings.Builder
	err := runInit(configPath, "wat", &stderr)
	if err == nil {
		t.Fatal("init must refuse a mode it does not recognise")
	}
	if _, statErr := os.Stat(configPath); statErr == nil {
		t.Error("init wrote a config despite refusing the mode")
	}
}

// present: styled is cairndex's default and needs Hugo; in direct mode it writes no
// HTML at all and warns. A newcomer's first build producing no page is the
// wrong first impression, so the starter must set bare explicitly.
func TestInitSetsPresentBare(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, DefaultConfigFile)
	var stderr strings.Builder
	if err := runInit(configPath, "", &stderr); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "present:  bare") &&
		!strings.Contains(string(body), "present: bare") {
		t.Errorf("the starter config does not set present: bare:\n%s", body)
	}
}

// Refusing beats clobbering, and it is the same promise the writer makes about
// every other file cairndex touches.
func TestInitRefusesToOverwriteAnExistingConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, DefaultConfigFile)
	mine := "version: 1\nroot: ./mine\nout: ./mine-out\n"
	if err := os.WriteFile(configPath, []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}

	var stderr strings.Builder
	err := runInit(configPath, "", &stderr)
	if err == nil {
		t.Fatal("init must not overwrite a config that is already there")
	}
	if !strings.Contains(err.Error(), configPath) {
		t.Errorf("the error should name the file it refused: %v", err)
	}
	got, _ := os.ReadFile(configPath)
	if string(got) != mine {
		t.Error("init overwrote an existing config")
	}
}

func TestInitIsWiredIntoTheCommandTree(t *testing.T) {
	var found bool
	for _, c := range newRootCmd().Commands() {
		if c.Name() == cmdInit {
			found = true
		}
	}
	if !found {
		t.Error("cairndex has no init subcommand")
	}
}

// The refusal has to be the create itself, not a check before it.
//
// Lstat and then WriteFile is a gap: two inits in the same directory, or an init
// racing an editor saving cairndex.yaml, both look and both see nothing, and
// O_CREATE|O_TRUNC then lets the later one clobber the earlier. Exactly one
// caller can create a file with O_EXCL, so exactly one can succeed here.
func TestInitCreatesExclusively(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), DefaultConfigFile)

	const racers = 8
	var wg sync.WaitGroup
	errs := make([]error, racers)
	start := make(chan struct{})
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var discard strings.Builder
			<-start
			errs[i] = runInit(configPath, "", &discard)
		}()
	}
	close(start)
	wg.Wait()

	won := 0
	for _, err := range errs {
		if err == nil {
			won++
		}
	}
	if won != 1 {
		t.Errorf("%d of %d inits created the same config; exactly one may", won, racers)
	}
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != starterConfigDirect {
		t.Errorf("the config is not what init writes:\n%s", got)
	}
}

// A create that succeeds and a write that does not must not leave a stub
// behind: the next run would refuse it, and the operator would be told a config
// exists when what is there is an empty file this command made.
func TestInitLeavesNothingBehindWhenTheWriteFails(t *testing.T) {
	dir := t.TempDir()
	// A directory in place of the file: the create fails, and nothing about the
	// tree should change.
	configPath := filepath.Join(dir, DefaultConfigFile)
	if err := os.Mkdir(configPath, 0o755); err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	if err := runInit(configPath, "", &stderr); err == nil {
		t.Fatal("writing over a directory must fail")
	}
	fi, err := os.Lstat(configPath)
	if err != nil || !fi.IsDir() {
		t.Error("the directory that was in the way did not survive")
	}
}

// The race removeIfSameFile used to exist for: something else creates
// configPath between this run starting and it trying to place its own file
// there. Now that placement is a single os.Link into configPath, that race
// collapses into the same exclusivity TestInitCreatesExclusively and
// TestInitRefusesToOverwriteAnExistingConfig already prove — whichever of two
// racing writers created configPath first wins, and Link, not a
// post-hoc inode comparison, is what makes that atomic.
//
// A temp file this run creates for its own content must not survive a
// successful init: only configPath itself should be left in the directory.
func TestInitLeavesNoTempFileBehindOnSuccess(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, DefaultConfigFile)
	var stderr strings.Builder
	if err := runInit(configPath, "", &stderr); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != DefaultConfigFile {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("directory holds more than the config init wrote: %v", names)
	}
}
