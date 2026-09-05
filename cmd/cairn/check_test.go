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

	// tree.csv is a name cairn generates but this config does not ask for, so
	// nothing claims it: the exact case Prune cannot reach. It carries cairn's
	// own column header, which is what allows removal to delete it — a file that
	// only wore the name would be kept.
	stale := filepath.Join(out, "bootstrap", "tree.csv")
	header := strings.Join(emit.CSVHeader, ",") + "\n"
	if err := os.WriteFile(stale, []byte(header), 0o644); err != nil {
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

// The narrowing, from the command's side. A file that only wears a generated
// name is kept, still reported, and still fails the check — so an operator is
// told about the collision instead of losing the file to it.
//
// index.txt is one filename per line, which is what a listing of anything looks
// like; SHA256SUMS is coreutils format, which every publisher's is. Neither can
// show who wrote it, so neither is ever deleted on the strength of its name.
func TestCheckKeepsOrphansItCannotProveAreCairnsOwn(t *testing.T) {
	configPath, out := fixture(t)
	var stderr strings.Builder
	if err := runBuild(configPath, "", build.Options{}, &stderr); err != nil {
		t.Fatalf("%v, stderr:\n%s", err, stderr.String())
	}

	foreign := filepath.Join(out, "bootstrap", "index.txt")
	body := []byte("a-real-artifact.tar.gz\nanother.tar.gz\n")
	if err := os.WriteFile(foreign, body, 0o644); err != nil {
		t.Fatal(err)
	}

	stderr.Reset()
	err := runCheck(context.Background(), configPath, true, &stderr)
	if !errors.Is(err, ErrNotIntact) {
		t.Fatalf("a kept orphan is still a finding and must fail the check, got %v", err)
	}
	got, readErr := os.ReadFile(foreign)
	if readErr != nil {
		t.Fatalf("--remove-orphaned deleted a file nothing showed was cairn's: %v", readErr)
	}
	if string(got) != string(body) {
		t.Errorf("the file was modified: %q", got)
	}
	if !strings.Contains(stderr.String(), "kept: nothing in its content shows cairn wrote it") {
		t.Errorf("keeping it was not reported: %q", stderr.String())
	}
}

// A removal that succeeded is not an error, and must not be logged as a stream
// of them.
//
// The findings were reported before the removal ran, so a --remove-orphaned run
// that cleaned up perfectly emitted one ERROR per orphan, then one INFO per
// removal, then exited zero. Any log-based alerting keyed on severity pages
// somebody for a tidy-up that worked, and the exit code says the opposite of
// what the log says.
func TestCheckDoesNotReportRemovedOrphansAsErrors(t *testing.T) {
	configPath, out := fixture(t)
	var stderr strings.Builder
	if err := runBuild(configPath, "", build.Options{}, &stderr); err != nil {
		t.Fatalf("%v, stderr:\n%s", err, stderr.String())
	}

	stale := filepath.Join(out, "bootstrap", "tree.csv")
	header := strings.Join(emit.CSVHeader, ",") + "\n"
	if err := os.WriteFile(stale, []byte(header), 0o644); err != nil {
		t.Fatal(err)
	}

	stderr.Reset()
	if err := runCheck(context.Background(), configPath, true, &stderr); err != nil {
		t.Fatalf("the tree is intact once the orphan is gone: %v\n%s", err, stderr.String())
	}
	if strings.Contains(stderr.String(), "level=ERROR") {
		t.Errorf("a clean removal logged an error and then exited zero:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "removed output cairn does not own") {
		t.Errorf("the removal was not reported: %q", stderr.String())
	}
}

// A directory the sweep took is reported like every other thing removed. It
// disappeared silently before, and in a mirror that is a directory of the
// published artifact tree.
func TestCheckReportsDirectoriesTheSweepRemoved(t *testing.T) {
	configPath, out := fixture(t)
	var stderr strings.Builder
	if err := runBuild(configPath, "", build.Options{}, &stderr); err != nil {
		t.Fatalf("%v, stderr:\n%s", err, stderr.String())
	}

	nested := filepath.Join(out, "gone", "deeper")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	header := strings.Join(emit.CSVHeader, ",") + "\n"
	if err := os.WriteFile(filepath.Join(nested, "index.csv"), []byte(header), 0o644); err != nil {
		t.Fatal(err)
	}

	stderr.Reset()
	if err := runCheck(context.Background(), configPath, true, &stderr); err != nil {
		t.Fatalf("%v\n%s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "removed a directory the removals left empty") {
		t.Errorf("the emptied directories were removed without a word:\n%s", stderr.String())
	}
	if _, err := os.Lstat(filepath.Join(out, "gone")); err == nil {
		t.Error("the sweep stopped at the immediate parent")
	}
}
