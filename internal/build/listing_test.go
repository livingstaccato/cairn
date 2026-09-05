// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for what a listing says about where it lives: base_path moves the whole
// tree under a prefix, and every path a listing publishes has to move with it.

package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
