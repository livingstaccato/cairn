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
	"github.com/livingstaccato/cairn/internal/emit"
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
	// index.json is written whether or not it was requested: the page reads its
	// entries from it, and Hugo publishes the resource verbatim so the bytes a
	// reader fetches are cairn's own.
	if _, err := os.Stat(filepath.Join(out, "bootstrap/index.json")); err != nil {
		t.Errorf("hugo mode must write the listing resource: %v", err)
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
	if res.Pruned == 0 {
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

// Pruning may only ever touch paths cairn recorded writing. An artifact sharing
// the output root is not cairn's to delete.
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
		t.Errorf("pruning deleted a file cairn did not create: %v", err)
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

	// Without the manifest cairn no longer knows it owns its own output, so the
	// conflict policy has to be relaxed for the build to proceed at all — see
	// TestRunWithoutManifestConflicts.
	c.OnConflict = config.ConflictSkip
	res := run(t, c, root, out)

	if res.Pruned != 0 {
		t.Errorf("Pruned = %d, want 0 without a manifest", res.Pruned)
	}
	if _, err := os.Stat(filepath.Join(out, "docs/index.json")); err != nil {
		t.Errorf("stale output should survive a missing manifest: %v", err)
	}
}

// Losing the manifest is recoverable but not silent. cairn stops rather than
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
	_, err := Run(c, root, out, obs.Discard())
	if err == nil {
		t.Fatal("expected a conflict once cairn cannot prove it wrote its output")
	}
	if !strings.Contains(err.Error(), config.ConflictSkip) {
		t.Errorf("error should name the way out, got: %v", err)
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

// In hugo mode every non-HTML output is a bundle resource Hugo publishes
// verbatim, so cairn writes them all rather than declaring output formats.
func TestRunHugoWritesEveryResource(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	c.Mode = config.ModeHugo
	sha := config.ChecksumSHA256
	outs := []string{config.OutputHTML, config.OutputJSON, config.OutputCSV, config.OutputText, config.OutputSums}
	c.Defaults = config.Override{Checksum: &sha, Outputs: &outs}
	run(t, c, root, out)

	for _, rel := range []string{"_index.md", "index.json", "index.csv", "index.txt", "SHA256SUMS"} {
		if _, err := os.Stat(filepath.Join(out, "bootstrap", rel)); err != nil {
			t.Errorf("missing bundle resource %s: %v", rel, err)
		}
	}
	// Hugo renders the HTML; cairn must not also write it.
	if _, err := os.Stat(filepath.Join(out, "bootstrap", "index.html")); err == nil {
		t.Error("cairn wrote index.html in hugo mode; Hugo renders that")
	}
}

// A directory holding only subdirectories has nothing to hash, so no SHA256SUMS
// is written and the footer must not offer one.
func TestRunNoSumsWhenNothingHashed(t *testing.T) {
	root, out := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "outer", "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outer", "inner", "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := conf(nil)
	sha := config.ChecksumSHA256
	outs := []string{config.OutputJSON, config.OutputSums}
	c.Defaults = config.Override{Checksum: &sha, Outputs: &outs}
	run(t, c, root, out)

	if _, err := os.Stat(filepath.Join(out, "outer", "SHA256SUMS")); !os.IsNotExist(err) {
		t.Errorf("outer/ holds only a directory; SHA256SUMS should not exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "outer", "inner", "SHA256SUMS")); err != nil {
		t.Errorf("inner/ has a file and should have digests: %v", err)
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
			if _, err := Run(c, root, out, obs.Discard()); err == nil {
				t.Fatal("expected an error writing into a read-only output root")
			}
		})
	}
}

// pep503 and the recursive listing do not mix: a simple index describes one
// directory's files, so the tree pass has nothing to write.
func TestRunPEP503SkipsRecursivePass(t *testing.T) {
	root, out := tree(t), t.TempDir()
	yes := true
	outs := []string{config.OutputJSON, config.OutputPEP503}
	c := conf([]config.Rule{{
		Match:    "bootstrap/**",
		Override: config.Override{Recursive: &yes, Outputs: &outs},
	}})
	run(t, c, root, out)

	if _, err := os.Stat(filepath.Join(out, "bootstrap", "index.html")); err != nil {
		t.Errorf("the directory's simple index is missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "bootstrap", "tree.html")); err == nil {
		t.Error("a recursive pass must not emit a second simple index")
	}
}

// TestHugoModeWritesRecursiveListing covers a silent no-op: emitHugo returned
// early for any basename but the index one, so recursive: true produced nothing
// at all in hugo mode. The comment claimed Hugo rendered tree.json from
// frontmatter, which stopped being true when the entries moved out of it.
//
// The recursive listing is machine data — one fetch instead of a walk — so it
// gets resources and no page of its own.
func TestHugoModeWritesRecursiveListing(t *testing.T) {
	root, out := tree(t), t.TempDir()
	yes := true
	c := conf([]config.Rule{{
		Match:    "bootstrap/**",
		Override: config.Override{Recursive: &yes},
	}})
	c.Mode = config.ModeHugo
	run(t, c, root, out)

	b, err := os.ReadFile(filepath.Join(out, "bootstrap", "tree.json"))
	if err != nil {
		t.Fatalf("hugo mode must write the recursive listing: %v", err)
	}
	var l model.Listing
	if err := json.Unmarshal(b, &l); err != nil {
		t.Fatal(err)
	}
	// One fetch has to reach past the immediate children, or it saves nobody a
	// request.
	deep := false
	for _, e := range l.Entries {
		if e.Depth > 1 {
			deep = true
		}
	}
	if !deep {
		t.Errorf("tree.json holds only immediate children; entries = %d", len(l.Entries))
	}

	// A recursive listing is data, not a page. A second _index.md in one
	// directory is not a thing Hugo can render.
	if _, err := os.Stat(filepath.Join(out, "bootstrap", "tree.md")); err == nil {
		t.Error("the recursive listing must not get a page of its own")
	}
}
