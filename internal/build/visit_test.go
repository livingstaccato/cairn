// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package build

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/model"
	"github.com/livingstaccato/cairn/internal/obs"
)

// Producing one directory's entries: the source the settings name, the metadata
// merged beside them, and what a walk that cannot read something does.

func TestRunAppliesMetadata(t *testing.T) {
	root, out := tree(t), t.TempDir()
	run(t, conf(nil), root, out)

	b, err := os.ReadFile(filepath.Join(out, "bootstrap/linux/index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var l model.Listing
	if err := json.Unmarshal(b, &l); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range l.Entries {
		if e.Name == "apt.list" {
			found = true
			if e.Title != "APT sources" {
				t.Errorf("Title = %q, want APT sources from _meta.yaml", e.Title)
			}
		}
		if strings.HasPrefix(e.Name, "_") {
			t.Errorf("_meta.yaml leaked into the listing as %q", e.Name)
		}
	}
	if !found {
		t.Error("apt.list missing from the listing")
	}
}

func TestRunHonorsDirectoryOverride(t *testing.T) {
	root, out := tree(t), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "docs", ".cairn.yaml"),
		[]byte("outputs: [json]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, conf(nil), root, out)

	if _, err := os.Stat(filepath.Join(out, "docs/index.json")); err != nil {
		t.Error("docs/index.json missing")
	}
	if _, err := os.Stat(filepath.Join(out, "docs/index.csv")); err == nil {
		t.Error(".cairn.yaml narrowed outputs to json; csv must not be written")
	}
}

// The three producers are dispatched on config, and until now only the fs branch
// was exercised through a build — the others were tested one layer down, which
// says nothing about whether a rule reaches them.
func TestRunDispatchesOnSource(t *testing.T) {
	root, out := tree(t), t.TempDir()
	pages, manifest := config.SourcePages, config.SourceManifest

	if err := os.MkdirAll(filepath.Join(root, "external"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "external", ".cairn.yaml"),
		[]byte("entries:\n  - name: upstream.iso\n    path: https://example.invalid/upstream.iso\n    title: Fetched elsewhere\n"),
		0o644); err != nil {
		t.Fatal(err)
	}

	c := conf([]config.Rule{
		{Match: "docs/**", Override: config.Override{Source: &pages}},
		{Match: "external/**", Override: config.Override{Source: &manifest}},
	})
	run(t, c, root, out)

	listing := func(rel string) model.Listing {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(out, rel))
		if err != nil {
			t.Fatal(err)
		}
		var l model.Listing
		if err := json.Unmarshal(b, &l); err != nil {
			t.Fatal(err)
		}
		return l
	}

	// pages lists Hugo content: intro.md becomes the page /docs/intro/, not a file.
	docs := listing("docs/index.json")
	var foundPage bool
	for _, e := range docs.Entries {
		if e.Name == "intro" {
			foundPage = true
			if e.Kind != "page" || e.Path != "/docs/intro/" {
				t.Errorf("pages producer not used: %+v", e)
			}
		}
	}
	if !foundPage {
		t.Errorf("docs listing missing its page: %v", docs.Entries)
	}

	// manifest lists what was authored, including a path pointing off-host.
	ext := listing("external/index.json")
	if len(ext.Entries) != 1 {
		t.Fatalf("external entries = %v, want 1 from the manifest", ext.Entries)
	}
	if ext.Entries[0].Title != "Fetched elsewhere" ||
		ext.Entries[0].Path != "https://example.invalid/upstream.iso" {
		t.Errorf("manifest producer not used: %+v", ext.Entries[0])
	}
}

// A file that cannot be read warns and goes without a digest. The listing is
// still correct; it just cannot be verified, and that is better than refusing to
// publish a mirror over one bad permission bit.
func TestRunUnreadableFileWarnsAndContinues(t *testing.T) {
	root, out := tree(t), t.TempDir()
	locked := filepath.Join(root, "bootstrap", "locked.bin")
	if err := os.WriteFile(locked, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Skipf("cannot drop read permission: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })
	if _, err := os.ReadFile(locked); err == nil {
		t.Skip("running with privileges that ignore file modes")
	}

	c := conf(nil)
	sha := config.ChecksumSHA256
	outs := []string{config.OutputJSON, config.OutputSums}
	c.Defaults = config.Override{Checksum: &sha, Outputs: &outs}

	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if _, err := Run(c, root, out, log); err != nil {
		t.Fatalf("one unreadable file must not fail the build: %v", err)
	}
	if !strings.Contains(buf.String(), "locked.bin") {
		t.Errorf("expected a warning naming the file, got %q", buf.String())
	}

	sums, err := os.ReadFile(filepath.Join(out, "bootstrap", "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sums), "locked.bin") {
		t.Error("a file that could not be hashed must not appear in SHA256SUMS")
	}
}

// Exceeding the cap fails the build rather than truncating. A silently short
// index is a wrong index.
func TestRunTreeCapFailsTheBuild(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	c.TreeMaxEntries = 1
	yes := true
	c.Rules = []config.Rule{{Match: "bootstrap/**", Override: config.Override{Recursive: &yes}}}

	if _, err := Run(c, root, out, obs.Discard()); err == nil {
		t.Fatal("expected the build to fail when a recursive listing exceeds the cap")
	}
}

func TestRunLogsWarningsWithoutFailing(t *testing.T) {
	root, out := tree(t), t.TempDir()
	// _meta.yaml naming a file that is not there: a mirror populated after
	// deploy legitimately does this.
	if err := os.WriteFile(filepath.Join(root, "docs", "_meta.yaml"),
		[]byte("ghost.iso:\n  title: Not here yet\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if _, err := Run(conf(nil), root, out, log); err != nil {
		t.Fatalf("a warning must not fail the build: %v", err)
	}
	if !strings.Contains(buf.String(), "ghost.iso") {
		t.Errorf("expected the skipped entry to be logged, got %q", buf.String())
	}
}

// TestMetadataSidecarsAreNeverListed pins the rule that what describes a listing
// is not part of it. _meta.yaml and <file>.meta.yaml are cairn's own inputs, and
// excluding them must not depend on a hide: glob happening to cover them — a
// tree that shows underscore-prefixed names would otherwise publish its own
// sidecars, with digests, in SHA256SUMS, on the page.
func TestMetadataSidecarsAreNeverListed(t *testing.T) {
	root, out := tree(t), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "bootstrap", "linux", "apt.list.meta.yaml"),
		[]byte("title: APT sources\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := conf(nil)
	// Nothing hidden, so a sidecar could only stay out of the listing by being
	// recognised as cairn's own input.
	none := []string{}
	c.Defaults = config.Override{Hide: &none}
	run(t, c, root, out)

	b, err := os.ReadFile(filepath.Join(out, "bootstrap", "linux", "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var l model.Listing
	if err := json.Unmarshal(b, &l); err != nil {
		t.Fatal(err)
	}
	for _, e := range l.Entries {
		if e.Name == "_meta.yaml" || strings.HasSuffix(e.Name, ".meta.yaml") {
			t.Errorf("listed %s; a metadata sidecar is cairn's input, not content", e.Name)
		}
	}
}
