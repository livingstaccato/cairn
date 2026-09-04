// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/model"
	"github.com/livingstaccato/cairn/internal/obs"
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
	res, err := Run(c, root, out, obs.Discard())
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

func TestRunChecksumRuleEmitsSums(t *testing.T) {
	root, out := tree(t), t.TempDir()
	sha := config.ChecksumSHA256
	rules := []config.Rule{{
		Match: "bootstrap/**",
		Override: config.Override{
			Checksum: &sha,
			Outputs:  &[]string{config.OutputJSON, config.OutputCSV, config.OutputSums},
		},
	}}
	run(t, conf(rules), root, out)

	b, err := os.ReadFile(filepath.Join(out, "bootstrap/SHA256SUMS"))
	if err != nil {
		t.Fatalf("SHA256SUMS not written: %v", err)
	}
	if !strings.Contains(string(b), "bootstrap.sh") {
		t.Errorf("SHA256SUMS = %q", b)
	}
	if _, err := os.Stat(filepath.Join(out, "docs/SHA256SUMS")); err == nil {
		t.Error("docs/ does not match the rule and must not get SHA256SUMS")
	}
}

func TestRunRecursiveRuleEmitsTree(t *testing.T) {
	root, out := tree(t), t.TempDir()
	yes := true
	rules := []config.Rule{{
		Match:    "bootstrap/**",
		Override: config.Override{Recursive: &yes, Outputs: &[]string{config.OutputJSON}},
	}}
	run(t, conf(rules), root, out)

	b, err := os.ReadFile(filepath.Join(out, "bootstrap/tree.json"))
	if err != nil {
		t.Fatalf("tree.json not written: %v", err)
	}
	var l model.Listing
	if err := json.Unmarshal(b, &l); err != nil {
		t.Fatal(err)
	}
	var maxDepth int
	for _, e := range l.Entries {
		if e.Depth > maxDepth {
			maxDepth = e.Depth
		}
	}
	if maxDepth < 2 {
		t.Errorf("max depth = %d, want at least 2 in a recursive listing", maxDepth)
	}
	if _, err := os.Stat(filepath.Join(out, "docs/tree.json")); err == nil {
		t.Error("docs/ is not recursive and must not get tree.json")
	}
	// SHA256SUMS describes a directory, never a recursive tree.
	if _, err := os.Stat(filepath.Join(out, "bootstrap/tree.SHA256SUMS")); err == nil {
		t.Error("a tree listing must not produce its own sums file")
	}
}

func TestRunPropagatesProtectFailure(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	c.Protect = []string{"bootstrap/**"}
	if _, err := Run(c, root, out, obs.Discard()); err == nil {
		t.Fatal("expected the build to fail when an output lands on a protected path")
	}
}

func TestRunUnknownOutputIsAnError(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	bogus := []string{"pdf"}
	c.Defaults = config.Override{Outputs: &bogus}
	if _, err := Run(c, root, out, obs.Discard()); err == nil {
		t.Fatal("expected an error for an unknown output format")
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

func TestRunBareHTMLWritten(t *testing.T) {
	root, out := tree(t), t.TempDir()
	bare := config.PresentBare
	rules := []config.Rule{{
		Match:    "bootstrap/**",
		Override: config.Override{Present: &bare, Outputs: &[]string{config.OutputHTML}},
	}}
	run(t, conf(rules), root, out)

	b, err := os.ReadFile(filepath.Join(out, "bootstrap/index.html"))
	if err != nil {
		t.Fatalf("index.html not written: %v", err)
	}
	if strings.Contains(string(b), "<script") {
		t.Error("bare HTML must contain no script tag")
	}
	if !strings.Contains(string(b), "bootstrap.sh") {
		t.Error("listing entry missing from HTML")
	}
}

// The styled presenter is Hugo's job; the direct path must not half-render it.
func TestRunStyledHTMLIsNotWrittenDirectly(t *testing.T) {
	root, out := tree(t), t.TempDir()
	styled := config.PresentStyled
	rules := []config.Rule{{
		Match:    "bootstrap/**",
		Override: config.Override{Present: &styled, Outputs: &[]string{config.OutputHTML}},
	}}
	run(t, conf(rules), root, out)

	if _, err := os.Stat(filepath.Join(out, "bootstrap/index.html")); err == nil {
		t.Error("styled HTML must be left to the Hugo layer")
	}
}
