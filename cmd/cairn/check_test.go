// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/livingstaccato/cairn/internal/build"
	"github.com/livingstaccato/cairn/internal/emit"
)

// checkFixture is a tree that carries checksums, so a check has something to
// re-hash. The build fixture omits them; a mirror worth verifying does not.
func checkFixture(t *testing.T) (configPath, root, out string) {
	t.Helper()
	base := t.TempDir()
	root = filepath.Join(base, "tree")
	out = filepath.Join(base, "out")
	if err := os.MkdirAll(filepath.Join(root, "bootstrap"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bootstrap", "boot.sh"),
		[]byte("#!/bin/sh\necho hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath = filepath.Join(base, "cairn.yaml")
	if err := os.WriteFile(configPath, []byte(
		"version: 1\nroot: ./tree\nout: ./out\ndefaults:\n  outputs: [json, sums]\n  checksum: sha256\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	return configPath, root, out
}

func TestCheckPassesOnAnIntactTree(t *testing.T) {
	configPath, _, _ := checkFixture(t)
	var stderr strings.Builder
	if err := runBuild(configPath, "", build.Options{}, &stderr); err != nil {
		t.Fatalf("%v, stderr:\n%s", err, stderr.String())
	}
	if err := runCheck(context.Background(), configPath, false, &stderr); err != nil {
		t.Fatalf("%v, stderr:\n%s", err, stderr.String())
	}
}

// The whole point: an artifact edited after publication is found, named, and
// the process exits non-zero so a deployment script can act on it.
func TestCheckFindsATamperedArtifact(t *testing.T) {
	configPath, root, _ := checkFixture(t)
	var stderr strings.Builder
	if err := runBuild(configPath, "", build.Options{}, &stderr); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(root, "bootstrap", "boot.sh")
	if err := os.WriteFile(target, []byte("#!/bin/sh\necho pwned\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stderr.Reset()
	err := runCheck(context.Background(), configPath, false, &stderr)
	if !errors.Is(err, ErrNotIntact) {
		t.Fatalf("runCheck = %v, want ErrNotIntact", err)
	}
	if !strings.Contains(stderr.String(), "boot.sh") {
		t.Errorf("the report does not name the file:\n%s", stderr.String())
	}
}

// Stale output is the dangerous kind: still served, still authoritative-looking,
// describing a directory as it was.
func TestCheckFindsOutputCairnDoesNotOwn(t *testing.T) {
	configPath, _, out := checkFixture(t)
	var stderr strings.Builder
	if err := runBuild(configPath, "", build.Options{}, &stderr); err != nil {
		t.Fatal(err)
	}

	// What a changed index_basename leaves behind.
	stale := filepath.Join(out, "bootstrap", "index.csv")
	if err := os.WriteFile(stale, []byte("name,size\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stderr.Reset()
	if err := runCheck(context.Background(), configPath, false, &stderr); !errors.Is(err, ErrNotIntact) {
		t.Fatalf("runCheck = %v, want ErrNotIntact", err)
	}
	if !strings.Contains(stderr.String(), "index.csv") {
		t.Errorf("the report does not name the orphan:\n%s", stderr.String())
	}
}

func TestCheckMissingConfigFails(t *testing.T) {
	var stderr strings.Builder
	err := runCheck(context.Background(), filepath.Join(t.TempDir(), "nope.yaml"), false, &stderr)
	if err == nil {
		t.Fatal("missing config must be an error")
	}
	if errors.Is(err, ErrNotIntact) {
		t.Error("a config that cannot be read is not a damaged tree")
	}
}

func TestCheckRejectsPositionalArgs(t *testing.T) {
	if _, err := exec(t, cmdCheck, "somewhere"); err == nil {
		t.Fatal("check must reject positional arguments")
	}
}

func TestRootCommandHasCheck(t *testing.T) {
	for _, c := range newRootCmd().Commands() {
		if c.Name() == "check" {
			return
		}
	}
	t.Error("root command has no check subcommand")
}

// The end-to-end of the one thing check could report and never act on.
func TestCheckRemovesOrphanedOutputWhenAsked(t *testing.T) {
	configPath, out := fixture(t)
	var stderr strings.Builder
	if err := runBuild(configPath, "", build.Options{}, &stderr); err != nil {
		t.Fatalf("%v, stderr:\n%s", err, stderr.String())
	}

	// index.txt is a name cairn generates but this config does not ask for, so
	// nothing claims it: the exact case Prune cannot reach.
	stale := filepath.Join(out, "bootstrap", "index.txt")
	if err := os.WriteFile(stale, []byte("name,size\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stderr.Reset()
	if err := runCheck(context.Background(), configPath, false, &stderr); !errors.Is(err, ErrNotIntact) {
		t.Fatalf("check should report the orphan and fail, got %v", err)
	}
	if _, err := os.Lstat(stale); err != nil {
		t.Fatal("a plain check must not delete anything")
	}

	stderr.Reset()
	if err := runCheck(context.Background(), configPath, true, &stderr); err != nil {
		t.Fatalf("--remove-orphaned should clear the finding: %v\n%s", err, stderr.String())
	}
	if _, err := os.Lstat(stale); err == nil {
		t.Error("the orphan was not removed")
	}
	if !strings.Contains(stderr.String(), "removed output cairn does not own") {
		t.Errorf("the removal was not reported: %q", stderr.String())
	}
}

// The guard, from the command's side: a lost manifest makes everything look
// unowned, and the flag must not turn that into a wipe.
func TestCheckRefusesToRemoveWhenTheManifestClaimsNothing(t *testing.T) {
	configPath, out := fixture(t)
	var stderr strings.Builder
	if err := runBuild(configPath, "", build.Options{}, &stderr); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(out, emit.ManifestFile)); err != nil {
		t.Fatal(err)
	}

	stderr.Reset()
	err := runCheck(context.Background(), configPath, true, &stderr)
	if err == nil {
		t.Fatal("removing everything because the manifest is gone must be refused")
	}
	if _, statErr := os.Lstat(filepath.Join(out, "bootstrap", "index.json")); statErr != nil {
		t.Error("output was deleted despite the refusal")
	}
}
