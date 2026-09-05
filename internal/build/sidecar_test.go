// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for the two files cairn writes about itself rather than about the tree:
// the manifest of what it owns and the hash cache.

package build

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/emit"
	"github.com/livingstaccato/cairn/internal/hash"
)

// A rebuild that changes nothing must not rewrite the manifest either.
//
// On a mirror of any size it is tens of megabytes, and it is written on every
// build; worse, in a mirror — where the indexed tree and the output are one
// directory — rewriting it moves an mtime that the parent's listing records and
// a watcher reacts to.
//
// The mtime is set to a distinctive past value first, so the assertion does not
// depend on the filesystem's timestamp resolution being finer than a build.
func TestASecondBuildDoesNotRewriteAnIdenticalManifest(t *testing.T) {
	root, out := tree(t), t.TempDir()
	run(t, conf(nil), root, out)

	mf := filepath.Join(out, emit.ManifestFile)
	long := time.Now().Add(-72 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(mf, long, long); err != nil {
		t.Fatal(err)
	}

	run(t, conf(nil), root, out)

	fi, err := os.Stat(mf)
	if err != nil {
		t.Fatal(err)
	}
	if !fi.ModTime().Equal(long) {
		t.Errorf("the manifest was rewritten: mtime %v, want it left at %v",
			fi.ModTime(), long)
	}
}

// The same for the hash cache: nothing re-hashed means nothing to record. It
// is the larger of the two — one record per file in the tree, not one per
// listing — so on a mirror it is the bigger pointless write of the pair.
func TestASecondBuildDoesNotRewriteAnIdenticalCache(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	sha := config.ChecksumSHA256
	outs := []string{config.OutputJSON, config.OutputSums}
	c.Defaults = config.Override{Checksum: &sha, Outputs: &outs}
	run(t, c, root, out)

	cf := filepath.Join(out, hash.CacheFile)
	long := time.Now().Add(-72 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(cf, long, long); err != nil {
		t.Fatal(err)
	}

	run(t, c, root, out)

	fi, err := os.Stat(cf)
	if err != nil {
		t.Fatal(err)
	}
	if !fi.ModTime().Equal(long) {
		t.Errorf("the cache was rewritten: mtime %v, want it left at %v",
			fi.ModTime(), long)
	}
}

// A manifest that has to change still does, and still parses. Skipping the
// write when the bytes match is worthless if it also skips when they differ.
func TestAManifestThatChangesIsStillWritten(t *testing.T) {
	root, out := tree(t), t.TempDir()
	run(t, conf(nil), root, out)

	mf := filepath.Join(out, emit.ManifestFile)
	before, err := os.ReadFile(mf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "docs")); err != nil {
		t.Fatal(err)
	}

	run(t, conf(nil), root, out)

	after, err := os.ReadFile(mf)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) == string(before) {
		t.Fatal("the manifest still claims a directory that is gone")
	}
	own, err := emit.ParseManifest(after)
	if err != nil {
		t.Fatalf("the rewritten manifest does not parse: %v", err)
	}
	if _, still := own["docs/index.json"]; still {
		t.Error("the manifest still claims docs/index.json")
	}
}

// A manifest cairn cannot read is the worst failure it has, because of what
// happens next: it claims nothing, and on_conflict: error then refuses a file
// cairn itself wrote, with a message about a path that already exists. The run
// has to say what actually went wrong before that starts.
func TestAnUnreadableManifestIsReportedBeforeTheConflicts(t *testing.T) {
	root, out := tree(t), t.TempDir()
	run(t, conf(nil), root, out)

	// The shape a manifest from an older cairn has: outputs recorded as a list
	// of paths, with no digest against any of them.
	if err := os.WriteFile(filepath.Join(out, emit.ManifestFile),
		[]byte(`{"version":1,"outputs":["index.json","docs/index.json"]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var logged bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logged, nil))
	_, err := Run(conf(nil), root, out, log)
	if err == nil {
		t.Fatal("a build that owns nothing must still refuse to overwrite what is there")
	}
	if !strings.Contains(logged.String(), "manifest could not be read") {
		t.Errorf("the run did not say why it owns nothing:\n%s", logged.String())
	}
}
