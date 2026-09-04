// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

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
	if err := runBuild(configPath, &stderr); err != nil {
		t.Fatalf("%v, stderr:\n%s", err, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(out, "bootstrap", "index.json")); err != nil {
		t.Errorf("expected output not written: %v", err)
	}
	if !strings.Contains(stderr.String(), "directories") {
		t.Errorf("expected a summary on stderr, got %q", stderr.String())
	}
}

func TestRunBuildMissingConfigFails(t *testing.T) {
	var stderr strings.Builder
	if err := runBuild(filepath.Join(t.TempDir(), "nope.yaml"), &stderr); err == nil {
		t.Fatal("missing config must be an error")
	}
	if stderr.Len() == 0 {
		t.Error("expected an explanatory message on stderr")
	}
}

func TestRunBuildFailedBuildFails(t *testing.T) {
	configPath, _ := fixture(t)
	// An output format no emitter handles fails the build.
	if err := os.WriteFile(configPath, []byte(
		"version: 1\nroot: ./tree\nout: ./out\ndefaults:\n  outputs: [pdf]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	if err := runBuild(configPath, &stderr); err == nil {
		t.Fatal("a failing build must be an error")
	}
}

// exec runs the wired command tree the way a shell would, and reports what a
// user would have seen.
func exec(t *testing.T, args ...string) (*strings.Builder, error) {
	t.Helper()
	var out strings.Builder
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	return &out, root.Execute()
}

// An unknown subcommand is a usage error, not a silent success.
func TestUnknownSubcommandFails(t *testing.T) {
	out, err := exec(t, "frobnicate")
	if err == nil {
		t.Fatal("unknown subcommand must be an error")
	}
	if !strings.Contains(out.String(), "frobnicate") {
		t.Errorf("expected the unknown name in the message, got %q", out.String())
	}
}

// build takes no positional arguments; one is a mistake worth reporting rather
// than ignoring.
func TestBuildRejectsPositionalArgs(t *testing.T) {
	if _, err := exec(t, "build", "somewhere"); err == nil {
		t.Fatal("build must reject positional arguments")
	}
}

func TestBuildCommandRunsTheBuild(t *testing.T) {
	configPath, out := fixture(t)
	if _, err := exec(t, "build", "--config", configPath); err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "bootstrap", "index.json")); err != nil {
		t.Errorf("the build command did not run the build: %v", err)
	}
}

// The tree ships with the commands the tests exercise.
func TestRootCommandHasBuild(t *testing.T) {
	var found bool
	for _, c := range newRootCmd().Commands() {
		if c.Name() == "build" {
			found = true
		}
	}
	if !found {
		t.Error("root command has no build subcommand")
	}
}
