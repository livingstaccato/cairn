// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) (configPath, out string) {
	t.Helper()
	root := t.TempDir()
	out = filepath.Join(root, "out")
	treeDir := filepath.Join(root, "tree", "bootstrap")
	if err := os.MkdirAll(treeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(treeDir, "bootstrap.sh"), []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath = filepath.Join(root, "cairn.yaml")
	if err := os.WriteFile(configPath, []byte(
		"version: 1\nroot: ./tree\nout: ./out\ndefaults:\n  outputs: [json, csv]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return configPath, out
}

func TestRunBuildEndToEnd(t *testing.T) {
	configPath, out := fixture(t)

	var stderr strings.Builder
	if code := runBuild(configPath, &stderr); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(out, "bootstrap", "index.json")); err != nil {
		t.Errorf("expected output not written: %v", err)
	}
	if !strings.Contains(stderr.String(), "directories") {
		t.Errorf("expected a summary on stderr, got %q", stderr.String())
	}
}

func TestRunBuildMissingConfigExitsNonZero(t *testing.T) {
	var stderr strings.Builder
	if code := runBuild(filepath.Join(t.TempDir(), "nope.yaml"), &stderr); code == 0 {
		t.Fatal("missing config must exit non-zero")
	}
	if stderr.Len() == 0 {
		t.Error("expected an explanatory message on stderr")
	}
}

func TestRunBuildFailedBuildExitsNonZero(t *testing.T) {
	configPath, _ := fixture(t)
	// An output format no emitter handles fails the build.
	if err := os.WriteFile(configPath, []byte(
		"version: 1\nroot: ./tree\nout: ./out\ndefaults:\n  outputs: [pdf]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	if code := runBuild(configPath, &stderr); code == 0 {
		t.Fatal("a failing build must exit non-zero")
	}
}

func TestUsageExitsTwo(t *testing.T) {
	var stderr strings.Builder
	if code := dispatch([]string{"cairn"}, &stderr); code != 2 {
		t.Errorf("no subcommand: exit %d, want 2", code)
	}
	if code := dispatch([]string{"cairn", "frobnicate"}, &stderr); code != 2 {
		t.Errorf("unknown subcommand: exit %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "usage") {
		t.Errorf("expected usage text, got %q", stderr.String())
	}
}

func TestDispatchBuild(t *testing.T) {
	configPath, out := fixture(t)
	var stderr strings.Builder
	if code := dispatch([]string{"cairn", "build", "-config", configPath}, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(out, "bootstrap", "index.json")); err != nil {
		t.Errorf("dispatch did not run the build: %v", err)
	}
}
