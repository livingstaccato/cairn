// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for provenance.json: off by default, written by a full build when
// asked, left alone by a scoped rebuild.
package build

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/livingstaccato/cairndex/internal/emit"
	"github.com/livingstaccato/cairndex/internal/obs"
)

func provenancePath(out string) string { return filepath.Join(out, "provenance.json") }

func TestProvenanceOffByDefault(t *testing.T) {
	root, out := tree(t), t.TempDir()
	run(t, conf(nil), root, out)

	if _, err := os.Stat(provenancePath(out)); err == nil {
		t.Error("provenance.json exists with provenance: unset (default false)")
	}
}

func TestProvenanceWritesAManifestWhenEnabled(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	c.Provenance = true
	res := run(t, c, root, out)

	b, err := os.ReadFile(provenancePath(out))
	if err != nil {
		t.Fatalf("provenance.json was not written: %v", err)
	}
	var m emit.Provenance
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("provenance.json does not parse: %v\n%s", err, b)
	}
	if m.Dirs != res.Dirs || m.Files != res.Files {
		t.Errorf("Dirs/Files = %d/%d, want %d/%d (the same build's own result)",
			m.Dirs, m.Files, res.Dirs, res.Files)
	}
	if len(m.Outputs) == 0 {
		t.Fatal("Outputs is empty; a build that wrote listings must be able to digest them")
	}
	seen := map[string]bool{}
	for _, o := range m.Outputs {
		if o.SHA256 == "" {
			t.Errorf("output %s has no digest", o.Path)
		}
		seen[o.Path] = true
	}
	if !seen["bootstrap/index.json"] {
		t.Errorf("Outputs does not name bootstrap/index.json: %+v", m.Outputs)
	}
	// provenance.json is not its own entry: it did not exist yet when its
	// own contents were computed.
	if seen["provenance.json"] {
		t.Error("provenance.json lists itself as one of its own outputs")
	}
}

// provenance.json is itself claimed by this run's manifest save, or the
// next run finds a file nothing owns and on_conflict: error refuses it.
func TestProvenanceIsOwnedSoARebuildSucceeds(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	c.Provenance = true
	run(t, c, root, out)
	run(t, c, root, out) // must not fail with an ownership conflict
}

func TestProvenanceOmittedOnADryRun(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	c.Provenance = true
	if _, err := RunWith(context.Background(), c, root, out, obs.Discard(), Options{Dry: true}); err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	if _, err := os.Stat(provenancePath(out)); err == nil {
		t.Error("provenance.json exists after --dry-run, which must write nothing")
	}
}

func TestProvenanceCarriesTheConfigHashItWasGiven(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	c.Provenance = true
	if _, err := RunWith(context.Background(), c, root, out, obs.Discard(),
		Options{ConfigSHA256: "deadbeef"}); err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	b, err := os.ReadFile(provenancePath(out))
	if err != nil {
		t.Fatal(err)
	}
	var m emit.Provenance
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m.ConfigSHA256 != "deadbeef" {
		t.Errorf("ConfigSHA256 = %q, want %q", m.ConfigSHA256, "deadbeef")
	}
}

// A scoped rebuild covers a subtree, not the whole tree provenance.json
// claims to describe, so it must leave a manifest a full build already
// wrote exactly as it found it rather than silently narrowing what it
// claims.
func TestScopedRebuildLeavesProvenanceUntouched(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	c.Provenance = true
	run(t, c, root, out)

	before, err := os.ReadFile(provenancePath(out))
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "docs", "intro.md"), []byte("# changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RunScoped(context.Background(), c, root, out, obs.Discard(), ScopePath("docs"), ""); err != nil {
		t.Fatalf("RunScoped: %v", err)
	}

	after, err := os.ReadFile(provenancePath(out))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("a scoped rebuild changed provenance.json; it should only ever be written by a full build")
	}
}
