// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for what a path says and what a listing admits: where the indexed tree
// is served, and which files are content rather than cairn's own inputs.

package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/model"
)

// TestBasePathMakesPathsSiteAbsolute checks the case where cairn's root is not
// the web root. Entry.Path is rooted at cairn's root, so a tree indexed from
// static/_odds and served at /_odds yields /mockups/x.html for a file the site
// serves at /_odds/mockups/x.html: every link a 404, with internally consistent
// JSON and nothing to signal it.
func TestBasePathMakesPathsSiteAbsolute(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	c.BasePath = "/_odds"
	run(t, c, root, out)

	var l model.Listing
	b, err := os.ReadFile(filepath.Join(out, "bootstrap", "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &l); err != nil {
		t.Fatal(err)
	}
	if l.Path != "/_odds/bootstrap" {
		t.Errorf("listing path = %q, want it under the base path", l.Path)
	}
	for _, e := range l.Entries {
		if !strings.HasPrefix(e.Path, "/_odds/") {
			t.Errorf("entry %s path = %q, want it under the base path", e.Name, e.Path)
		}
	}
}

func TestBasePathDefaultsToUnchanged(t *testing.T) {
	root, out := tree(t), t.TempDir()
	run(t, conf(nil), root, out)

	b, err := os.ReadFile(filepath.Join(out, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var l model.Listing
	if err := json.Unmarshal(b, &l); err != nil {
		t.Fatal(err)
	}
	if l.Path != "/" {
		t.Errorf("root listing path = %q, want / when no base_path is set", l.Path)
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
