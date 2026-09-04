// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

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

func TestRunHugoModeWritesOneFilePerDir(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	c.Mode = config.ModeHugo
	run(t, c, root, out)

	for _, rel := range []string{"_index.md", "bootstrap/_index.md", "bootstrap/linux/_index.md"} {
		if _, err := os.Stat(filepath.Join(out, rel)); err != nil {
			t.Errorf("missing %s", rel)
		}
	}
	// Hugo renders these from the page; a second source could disagree.
	if _, err := os.Stat(filepath.Join(out, "bootstrap/index.json")); err == nil {
		t.Error("hugo mode must not also write index.json")
	}
	b, err := os.ReadFile(filepath.Join(out, "bootstrap/linux/_index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "APT sources") {
		t.Error("metadata did not reach the frontmatter")
	}
}

func TestRunHugoModeSkipsRecursivePage(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	c.Mode = config.ModeHugo
	yes := true
	c.Rules = []config.Rule{{Match: "bootstrap/**", Override: config.Override{Recursive: &yes}}}
	run(t, c, root, out)

	// Hugo renders tree.json from the same page; a second bundle would be a
	// second source for one URL.
	if _, err := os.Stat(filepath.Join(out, "bootstrap/tree.md")); err == nil {
		t.Error("a recursive listing must not get its own Hugo page")
	}
	if _, err := os.Stat(filepath.Join(out, "bootstrap/_index.md")); err != nil {
		t.Errorf("the directory page is still required: %v", err)
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

func TestRunEmitsPEP503(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	outs := []string{config.OutputJSON, config.OutputPEP503}
	c.Defaults = config.Override{Outputs: &outs}
	run(t, c, root, out)

	b, err := os.ReadFile(filepath.Join(out, "bootstrap/index.html"))
	if err != nil {
		t.Fatalf("simple index not written: %v", err)
	}
	if !strings.Contains(string(b), "pypi:repository-version") {
		t.Errorf("not a PEP 503 page:\n%s", b)
	}
}

// html and pep503 are alternative renderings of one URL, so asking for both is
// a configuration error the write guard surfaces rather than a silent
// last-one-wins.
func TestRunPEP503AndHTMLCollide(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	bare := config.PresentBare
	outs := []string{config.OutputHTML, config.OutputPEP503}
	c.Defaults = config.Override{Outputs: &outs, Present: &bare}
	if _, err := Run(c, root, out, obs.Discard()); err == nil {
		t.Fatal("expected a conflict when both html and pep503 target index.html")
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

	// out == root: cairn writes into the tree it is indexing.
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
			t.Errorf("listing includes cairn's own output %q", generated)
		}
	}
	if !strings.Contains(firstList, "bootstrap.sh") {
		t.Errorf("listing lost a real file: %q", firstList)
	}
}

// Hugo mode writes _index.md into the tree in the same arrangement, and must
// not list it either.
func TestRunInPlaceHugoExcludesItsPage(t *testing.T) {
	root := tree(t)
	c := conf(nil)
	c.Mode = config.ModeHugo
	outs := []string{config.OutputText}
	c.Defaults = config.Override{Outputs: &outs}
	run(t, c, root, root)
	run(t, c, root, root)

	b, err := os.ReadFile(filepath.Join(root, "bootstrap", "index.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "_index.md") {
		t.Errorf("listing includes the page cairn generated: %q", b)
	}
}
