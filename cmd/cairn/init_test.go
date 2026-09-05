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
	if err := runInit(configPath, &stderr); err != nil {
		t.Fatalf("init: %v, stderr:\n%s", err, stderr.String())
	}
	if err := runBuild(configPath, "", build.Options{}, &stderr); err != nil {
		t.Fatalf("a build of the config init wrote must succeed: %v\nstderr:\n%s", err, stderr.String())
	}

	// A browsable page is the point of the defaults it picks.
	for _, rel := range []string{"index.html", filepath.Join("pool", "index.html")} {
		if _, err := os.Stat(filepath.Join(dir, "site", rel)); err != nil {
			t.Errorf("the starter config produced no %s", rel)
		}
	}
}

// present: styled is cairn's default and needs Hugo; in direct mode it writes no
// HTML at all and warns. A newcomer's first build producing no page is the
// wrong first impression, so the starter must set bare explicitly.
func TestInitSetsPresentBare(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, DefaultConfigFile)
	var stderr strings.Builder
	if err := runInit(configPath, &stderr); err != nil {
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
// every other file cairn touches.
func TestInitRefusesToOverwriteAnExistingConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, DefaultConfigFile)
	mine := "version: 1\nroot: ./mine\nout: ./mine-out\n"
	if err := os.WriteFile(configPath, []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}

	var stderr strings.Builder
	err := runInit(configPath, &stderr)
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
		t.Error("cairn has no init subcommand")
	}
}
