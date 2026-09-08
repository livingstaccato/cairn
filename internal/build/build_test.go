// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package build

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/livingstaccato/cairndex/internal/config"
	"github.com/livingstaccato/cairndex/internal/emit"
	"github.com/livingstaccato/cairndex/internal/obs"
)

func tree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mk := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("bootstrap/bootstrap.sh", "#!/bin/sh\n")
	mk("bootstrap/linux/apt.list", "deb http://example.invalid stable main\n")
	mk("bootstrap/linux/_meta.yaml", "apt.list:\n  title: APT sources\n")
	mk("docs/intro.md", "# Intro\n")
	return root
}

func conf(rules []config.Rule) *config.Config {
	return &config.Config{
		Version: 1, IndexBasename: "index", TreeMaxEntries: 1000,
		OnConflict: config.ConflictError, Rules: rules,
	}
}

func run(t *testing.T, c *config.Config, root, out string) *Result {
	t.Helper()
	res, err := Run(context.Background(), c, root, out, obs.Discard())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

func TestRunWritesIndexPerDirectory(t *testing.T) {
	root, out := tree(t), t.TempDir()
	res := run(t, conf(nil), root, out)
	// root, bootstrap, bootstrap/linux, docs
	if res.Dirs != 4 {
		t.Errorf("Dirs = %d, want 4", res.Dirs)
	}
	for _, rel := range []string{
		"index.json", "index.csv",
		"bootstrap/index.json", "bootstrap/linux/index.json", "docs/index.json",
	} {
		if _, err := os.Stat(filepath.Join(out, rel)); err != nil {
			t.Errorf("missing expected output %s", rel)
		}
	}
}

func TestRunCountsFiles(t *testing.T) {
	root, out := tree(t), t.TempDir()
	res := run(t, conf(nil), root, out)
	// bootstrap.sh, apt.list, intro.md — _meta.yaml is hidden.
	if res.Files != 3 {
		t.Errorf("Files = %d, want 3", res.Files)
	}
	if len(res.Written) == 0 {
		t.Error("Written should list every emitted path")
	}
}

// In-place mode is the deployment that matters for a mirror: index files live
// beside the artifacts, so the tree rsyncs whole and verifies where it sits.
// That only works if a build reaches a fixed point — the second run must not
// list the first run's output, or SHA256SUMS covers files that change every run.
func TestRunInPlaceIsIdempotent(t *testing.T) {
	root := tree(t)
	c := conf(nil)
	sha := config.ChecksumSHA256
	outs := []string{config.OutputJSON, config.OutputCSV, config.OutputText, config.OutputSums}
	c.Defaults = config.Override{Checksum: &sha, Outputs: &outs}

	read := func(rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	// out == root: cairndex writes into the tree it is indexing.
	run(t, c, root, root)
	firstSums := read("bootstrap/SHA256SUMS")
	firstList := read("bootstrap/index.txt")

	run(t, c, root, root)
	if got := read("bootstrap/SHA256SUMS"); got != firstSums {
		t.Errorf("SHA256SUMS drifted between runs:\nfirst:\n%s\nsecond:\n%s", firstSums, got)
	}
	if got := read("bootstrap/index.txt"); got != firstList {
		t.Errorf("listing drifted between runs:\nfirst:\n%s\nsecond:\n%s", firstList, got)
	}

	for _, generated := range []string{"index.json", "index.csv", "index.txt", "SHA256SUMS"} {
		if strings.Contains(firstList, generated) {
			t.Errorf("listing includes cairndex's own output %q", generated)
		}
	}
	if !strings.Contains(firstList, "bootstrap.sh") {
		t.Errorf("listing lost a real file: %q", firstList)
	}
}

// A generator that only ever adds is not maintaining a mirror. When a file goes
// away its digest must go with it, and when a directory goes away so must the
// whole listing published for it — otherwise the site serves a page full of
// links to things that no longer exist.
func TestRunPrunesRemovedFilesAndDirectories(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	sha := config.ChecksumSHA256
	outs := []string{config.OutputJSON, config.OutputText, config.OutputSums}
	c.Defaults = config.Override{Checksum: &sha, Outputs: &outs}

	run(t, c, root, out)
	for _, rel := range []string{"bootstrap/index.json", "bootstrap/linux/index.json", "docs/index.json"} {
		if _, err := os.Stat(filepath.Join(out, rel)); err != nil {
			t.Fatalf("expected %s from the first run: %v", rel, err)
		}
	}

	// One file gone, one whole directory gone.
	if err := os.Remove(filepath.Join(root, "bootstrap", "bootstrap.sh")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "docs")); err != nil {
		t.Fatal(err)
	}

	res := run(t, c, root, out)
	if len(res.Pruned) == 0 {
		t.Error("nothing was pruned")
	}

	// The removed directory's whole listing goes.
	if _, err := os.Stat(filepath.Join(out, "docs")); !os.IsNotExist(err) {
		t.Errorf("stale directory survived: %v", err)
	}

	// The removed file leaves the listing and the digests.
	b, err := os.ReadFile(filepath.Join(out, "bootstrap", "index.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "bootstrap.sh") {
		t.Errorf("removed file still listed: %q", b)
	}
	// bootstrap/ now holds only a subdirectory, so there is nothing left to
	// hash and SHA256SUMS is pruned outright. Either way the removed file must
	// not still carry a digest.
	switch sums, err := os.ReadFile(filepath.Join(out, "bootstrap", "SHA256SUMS")); {
	case os.IsNotExist(err):
	case err != nil:
		t.Fatal(err)
	case strings.Contains(string(sums), "bootstrap.sh"):
		t.Errorf("removed file still has a digest: %q", sums)
	}

	// What still exists is untouched.
	if _, err := os.Stat(filepath.Join(out, "bootstrap/linux/index.json")); err != nil {
		t.Errorf("a live listing was pruned: %v", err)
	}
}

// Pruning may only ever touch paths cairndex recorded writing. An artifact sharing
// the output root is not cairndex's to delete.
func TestRunPruneLeavesForeignFilesAlone(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	run(t, c, root, out)

	foreign := filepath.Join(out, "bootstrap", "ubuntu.iso")
	if err := os.WriteFile(foreign, []byte("mirrored artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "docs")); err != nil {
		t.Fatal(err)
	}
	run(t, c, root, out)

	if _, err := os.Stat(foreign); err != nil {
		t.Errorf("pruning deleted a file cairndex did not create: %v", err)
	}
}

// A missing manifest must prune nothing. Stale files are a nuisance; deleting
// someone's artifacts is not.
func TestRunPruneWithoutManifestRemovesNothing(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	run(t, c, root, out)

	if err := os.Remove(filepath.Join(out, emit.ManifestFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "docs")); err != nil {
		t.Fatal(err)
	}

	// Without the manifest cairndex no longer knows it owns its own output, so the
	// conflict policy has to be relaxed for the build to proceed at all — see
	// TestRunWithoutManifestConflicts.
	c.OnConflict = config.ConflictSkip
	res := run(t, c, root, out)

	if len(res.Pruned) != 0 {
		t.Errorf("Pruned = %v, want none without a manifest", res.Pruned)
	}
	if _, err := os.Stat(filepath.Join(out, "docs/index.json")); err != nil {
		t.Errorf("stale output should survive a missing manifest: %v", err)
	}
}

// Losing the manifest is recoverable but not silent. cairndex stops rather than
// overwriting files it can no longer prove it wrote, and the error says how to
// proceed — an rsync --delete or a git clean over the output directory is enough
// to land here.
func TestRunWithoutManifestConflicts(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	run(t, c, root, out)

	if err := os.Remove(filepath.Join(out, emit.ManifestFile)); err != nil {
		t.Fatal(err)
	}
	_, err := Run(context.Background(), c, root, out, obs.Discard())
	if err == nil {
		t.Fatal("expected a conflict once cairndex cannot prove it wrote its output")
	}
	if !strings.Contains(err.Error(), config.ConflictSkip) {
		t.Errorf("error should name the way out, got: %v", err)
	}
}

// Wrong permissions on the output root is an operational mistake, not a
// programming one, so it must surface as an error naming the path rather than a
// partially written site.
func TestRunUnwritableOutputFails(t *testing.T) {
	root, out := tree(t), t.TempDir()
	if err := os.Chmod(out, 0o500); err != nil {
		t.Skipf("cannot drop write permission: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(out, 0o755) })
	if err := os.WriteFile(filepath.Join(out, "probe"), nil, 0o644); err == nil {
		t.Skip("running with privileges that ignore file modes")
	}

	for _, mode := range []string{config.ModeDirect, config.ModeHugo} {
		t.Run(mode, func(t *testing.T) {
			c := conf(nil)
			c.Mode = mode
			sha := config.ChecksumSHA256
			outs := []string{config.OutputJSON, config.OutputCSV, config.OutputText, config.OutputSums}
			c.Defaults = config.Override{Checksum: &sha, Outputs: &outs}
			if _, err := Run(context.Background(), c, root, out, obs.Discard()); err == nil {
				t.Fatal("expected an error writing into a read-only output root")
			}
		})
	}
}

// A panic mid-build must not leave what it already wrote unclaimed:
// safeBuild converts it to an error, which takes the same SavePartial path as
// any other build failure. buildStep is a var, like atomicfile's rename,
// because a panic mid-build is a failure this package otherwise has no way to
// produce in a test.
func TestPanicMidBuildStillClaimsPartialOutput(t *testing.T) {
	root, out := tree(t), t.TempDir()

	orig := buildStep
	t.Cleanup(func() { buildStep = orig })
	buildStep = func(r *runner) error {
		if err := r.writer.Write("index.json", []byte(`{"entries":[]}`)); err != nil {
			t.Fatal(err)
		}
		panic("simulated panic mid-build")
	}
	if _, err := Run(context.Background(), conf(nil), root, out, obs.Discard()); err == nil {
		t.Fatal("expected the recovered panic to surface as an error")
	}
	buildStep = orig

	// A second, ordinary build must find index.json already claimed by the
	// panicked run — proof SavePartial ran on the panic path too, not only the
	// returned-error one.
	if _, err := Run(context.Background(), conf(nil), root, out, obs.Discard()); err != nil {
		t.Fatalf("a later build must own what the panicked run wrote, got: %v", err)
	}
}
