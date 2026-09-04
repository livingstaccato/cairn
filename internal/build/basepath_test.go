// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

// Tests for base_path: where the indexed tree is served, as opposed to where it
// sits on disk. The two are only the same when cairn's root is the web root.

package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/livingstaccato/cairn/internal/model"
)

// TestBasePathMakesPathsSiteAbsolute covers a trap the first real consumer hit.
// Entry.Path is rooted at cairn's root, so a tree indexed from static/_odds and
// served at /_odds produced /mockups/x.html where the site serves
// /_odds/mockups/x.html — every link a fresh 404, and nothing in cairn said so.
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
