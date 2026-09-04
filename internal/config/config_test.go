// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"testing"
)

const sample = `
version: 1
root: ./tree
out: ./web/content
index_basename: index
tree_max_entries: 50000
protect:
  - "dists/**"
  - "repodata/**"
  - "**/Release"
on_conflict: error
defaults:
  present: styled
  checksum: none
rules:
  - match: "bootstrap/**"
    present: bare
    checksum: sha256
    recursive: true
  - match: "bootstrap/legacy/**"
    checksum: none
  - match: "docs/**"
    source: pages
`

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "cairn.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad(t *testing.T) {
	c, err := Load(writeConfig(t, sample))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Version != 1 || c.Root != "./tree" || c.Out != "./web/content" {
		t.Errorf("unexpected top-level: %+v", c)
	}
	if c.IndexBasename != "index" || c.TreeMaxEntries != 50000 || c.OnConflict != "error" {
		t.Errorf("unexpected policy fields: %+v", c)
	}
	if len(c.Rules) != 3 {
		t.Fatalf("Rules = %d, want 3", len(c.Rules))
	}
	if c.Rules[0].Match != "bootstrap/**" {
		t.Errorf("rule order not preserved: %v", c.Rules)
	}
}

func TestLoadDefaultsFilledIn(t *testing.T) {
	c, err := Load(writeConfig(t, "version: 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.IndexBasename != "index" {
		t.Errorf("IndexBasename = %q, want index", c.IndexBasename)
	}
	if c.TreeMaxEntries != 50000 {
		t.Errorf("TreeMaxEntries = %d, want 50000", c.TreeMaxEntries)
	}
	if c.OnConflict != "error" {
		t.Errorf("OnConflict = %q, want error", c.OnConflict)
	}
}

func TestLoadErrors(t *testing.T) {
	cases := map[string]string{
		"unsupported version": "version: 99\n",
		"bad on_conflict":     "version: 1\non_conflict: maybe\n",
		"malformed yaml":      "version: 1\nrules: [unclosed\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, body)); err == nil {
				t.Fatal("expected an error, got nil")
			}
		})
	}
	if _, err := Load(filepath.Join(t.TempDir(), "absent.yaml")); err == nil {
		t.Fatal("a missing config file must error")
	}
}

func TestResolvePrecedence(t *testing.T) {
	c, err := Load(writeConfig(t, sample))
	if err != nil {
		t.Fatal(err)
	}

	t.Run("unmatched dir gets root defaults", func(t *testing.T) {
		got := c.Resolve("misc/things", nil)
		if got.Present != "styled" || got.Checksum != "none" || got.Recursive {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("root dir gets root defaults", func(t *testing.T) {
		got := c.Resolve(".", nil)
		if got.Present != "styled" || got.Source != "fs" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("matching rule applies", func(t *testing.T) {
		got := c.Resolve("bootstrap/linux", nil)
		if got.Present != "bare" || got.Checksum != "sha256" || !got.Recursive {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("rule matches the directory it names, not only its contents", func(t *testing.T) {
		got := c.Resolve("bootstrap", nil)
		if got.Present != "bare" {
			t.Errorf("Present = %q, want bare — bootstrap/** must cover bootstrap itself", got.Present)
		}
	})

	t.Run("later matching rule wins over earlier", func(t *testing.T) {
		got := c.Resolve("bootstrap/legacy/x", nil)
		if got.Checksum != "none" {
			t.Errorf("Checksum = %q, want none from the later rule", got.Checksum)
		}
		if got.Present != "bare" {
			t.Errorf("Present = %q, want bare still inherited from the earlier rule", got.Present)
		}
	})

	t.Run("directory override beats every rule", func(t *testing.T) {
		got := c.Resolve("bootstrap/linux", &Override{Present: strp("styled")})
		if got.Present != "styled" {
			t.Errorf("Present = %q, want styled from .cairn.yaml", got.Present)
		}
		if got.Checksum != "sha256" {
			t.Errorf("Checksum = %q, want sha256 still from the rule", got.Checksum)
		}
	})
}

func TestIsProtected(t *testing.T) {
	c, err := Load(writeConfig(t, sample))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]bool{
		"dists/stable/Release":       true,
		"repodata/repomd.xml":        true,
		"pool/main/Release":          true,
		"bootstrap/linux/index.json": false,
		"docs/index.html":            false,
	}
	for p, want := range cases {
		if got := c.IsProtected(p); got != want {
			t.Errorf("IsProtected(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestIsProtectedIgnoresBadGlob(t *testing.T) {
	c := &Config{Protect: []string{"["}}
	if c.IsProtected("anything") {
		t.Error("a malformed glob must not match everything")
	}
}

func TestModeDefaultsAndValidation(t *testing.T) {
	c, err := Load(writeConfig(t, "version: 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Mode != ModeDirect {
		t.Errorf("Mode = %q, want %q", c.Mode, ModeDirect)
	}
	if _, err := Load(writeConfig(t, "version: 1\nmode: hugo\n")); err != nil {
		t.Errorf("hugo mode rejected: %v", err)
	}
	if _, err := Load(writeConfig(t, "version: 1\nmode: sideways\n")); err == nil {
		t.Fatal("expected an error for an unknown mode")
	}
}

func TestUnknownSourceIsRejected(t *testing.T) {
	// A typo must not fall through to the fs default and silently index the
	// wrong thing.
	if _, err := Load(writeConfig(t, "version: 1\ndefaults:\n  source: filesystem\n")); err == nil {
		t.Fatal("expected an error for an unknown default source")
	}
	if _, err := Load(writeConfig(t,
		"version: 1\nrules:\n  - match: \"x/**\"\n    source: pagez\n")); err == nil {
		t.Fatal("expected an error for an unknown source in a rule")
	}
	for _, src := range []string{SourceFS, SourcePages, SourceManifest} {
		body := "version: 1\ndefaults:\n  source: " + src + "\n"
		if _, err := Load(writeConfig(t, body)); err != nil {
			t.Errorf("source %q rejected: %v", src, err)
		}
	}
}
