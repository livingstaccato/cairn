// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package build

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/livingstaccato/cairndex/internal/config"
	"github.com/livingstaccato/cairndex/internal/model"
	"github.com/livingstaccato/cairndex/internal/obs"
)

// recursing returns a config that writes the recursive listing beside the
// per-directory one.
func recursing() *config.Config {
	c := conf(nil)
	rec := true
	outs := []string{config.OutputJSON}
	c.Defaults = config.Override{Recursive: &rec, Outputs: &outs}
	return c
}

// treeEntries reads a recursive listing and returns its entries.
func treeEntries(t *testing.T, path string) []struct{ Name, Path string } {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var l struct {
		Entries []struct{ Name, Path string } `json:"entries"`
	}
	if err := json.Unmarshal(b, &l); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return l.Entries
}

// The recursive listing has to leave out exactly what the per-directory listing
// leaves out.
//
// The two are produced by different paths — collect runs the per-directory walk
// through dropGenerated, while emitTree reaches walk.Tree directly — so the
// exclusion had to be stated twice or stated once and shared. Stated only on the
// collect side, tree.json published the output directory and cairndex's own
// generated files as though they were content, and the recursive search index
// built from that listing inherited it.
func TestTreeListingExcludesTheOutputDirectory(t *testing.T) {
	root, out := nested(t)

	// Twice: the output directory does not exist to be walked until the first
	// build has written it.
	run(t, recursing(), root, out)
	run(t, recursing(), root, out)

	for _, e := range treeEntries(t, filepath.Join(out, "tree.json")) {
		if e.Name == "site" {
			t.Errorf("the recursive listing names the output directory: %+v", e)
		}
		if strings.Contains(e.Path, "/site/") {
			t.Errorf("the recursive listing descended into the output directory: %+v", e)
		}
	}
}

// cairndex's own generated filenames are dropped from the recursive listing for the
// same reason they are dropped from the per-directory one: in a mirror, root and
// out are one directory, so there is no subtree to skip and the names are the
// only thing keeping the build from listing what it just wrote.
func TestTreeListingExcludesGeneratedNames(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pool"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pool", "a.txt"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	run(t, recursing(), root, root)
	run(t, recursing(), root, root)

	for _, e := range treeEntries(t, filepath.Join(root, "tree.json")) {
		if e.Name == "index.json" || e.Name == "tree.json" {
			t.Errorf("the recursive listing names cairndex's own output: %+v", e)
		}
	}
}

// treeEntries' symlink-loop guard has to catch an ancestor reached again
// through a link without also refusing two unrelated links that merely
// point at the same target — sibling directories a real tree can hold, not
// a cycle. A seen set spanning the whole recursive walk instead of just the
// current descent's own path cannot tell the two apart: whichever sibling
// is visited second gets falsely flagged and its subtree dropped from the
// recursive listing.
func TestTreeListingDoesNotFlagTwoLinksToTheSameTarget(t *testing.T) {
	root, out := t.TempDir(), t.TempDir()
	// The target has to resolve inside root: a followed symlink pointing
	// outside it is refused as escaping the root, a different check from
	// the one this test is after.
	target := filepath.Join(root, "real")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "shared.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link1")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link2")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	c := recursing()
	follow := true
	c.Defaults.FollowSymlinks = &follow

	run(t, c, root, out)

	var sawReal, sawLink1, sawLink2 bool
	for _, e := range treeEntries(t, filepath.Join(out, "tree.json")) {
		switch e.Path {
		case "/real/shared.txt":
			sawReal = true
		case "/link1/shared.txt":
			sawLink1 = true
		case "/link2/shared.txt":
			sawLink2 = true
		}
	}
	if !sawReal || !sawLink1 || !sawLink2 {
		t.Errorf("three siblings resolving to the same real directory should each be "+
			"descended, got real=%v link1=%v link2=%v", sawReal, sawLink1, sawLink2)
	}
}

// A symlink pointing back to an ancestor is the real cycle the seen set has
// to catch, unwinding it per-branch must not have traded that away for the
// two-siblings fix above.
func TestTreeListingStopsAtAnAncestorLoop(t *testing.T) {
	root, out := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, filepath.Join(root, "a", "loop")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	c := recursing()
	follow := true
	c.Defaults.FollowSymlinks = &follow

	done := make(chan error, 1)
	go func() { _, err := Run(context.Background(), c, root, out, obs.Discard()); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("treeEntries never returned; an ancestor symlink loop was not caught")
	}

	// The root's own identity has to be seeded into seen before the first
	// descent, or a.loop's link back to root is not caught until root's own
	// subtree has already been walked a second time through it: "a" and
	// "a/loop" listed once each is the whole tree, seeded; unseeded, "a"
	// under the second, wasted pass appears again as "a/loop/a", and its
	// own copy of the loop as "a/loop/a/loop" — four entries instead of two
	// for a cycle exactly one link deep.
	got := treeEntries(t, filepath.Join(out, "tree.json"))
	if len(got) != 2 {
		names := make([]string, len(got))
		for i, e := range got {
			names[i] = e.Path
		}
		t.Errorf("got %d entries %v, want exactly 2 (a, a/loop): the cycle was "+
			"caught only after re-walking root's own subtree once already", len(got), names)
	}
}

// The recursive listing is an output like any other, so a rebuild that changed
// nothing must not rewrite it. Listing the output directory made tree.json
// differ on every run, which is the churn Result.Changed exists to rule out.
func TestTreeListingReachesAFixedPoint(t *testing.T) {
	root, out := nested(t)

	run(t, recursing(), root, out)
	run(t, recursing(), root, out)
	third := run(t, recursing(), root, out)

	if len(third.Changed) != 0 {
		t.Errorf("a settled rebuild rewrote %v, want nothing", third.Changed)
	}
}

// A sidecar's hidden: has to be honoured by the recursive listing exactly as
// it is by the per-directory one — collect runs meta.Load/meta.Apply, and
// emitTree used to reach the tree through walk.Tree directly, which never
// merged a sidecar at all.
func TestTreeListingHonoursSidecarHidden(t *testing.T) {
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
	mk("docs/public.txt", "keep\n")
	mk("docs/secret.txt", "hide me\n")
	mk("docs/_meta.yaml", "secret.txt:\n  hidden: true\n")

	out := t.TempDir()
	run(t, recursing(), root, out)

	for _, e := range treeEntries(t, filepath.Join(out, "tree.json")) {
		if e.Name == "secret.txt" {
			t.Errorf("tree.json lists a file its own directory's sidecar hides: %+v", e)
		}
	}
}

// The recursive listing has to dispatch on each directory's own configured
// Source, exactly as the per-directory listing does — emitTree used to reach
// every directory through a raw filesystem walk regardless of source:,
// showing source: pages content as raw disk files and failing outright on
// source: manifest content that does not exist on disk.
func TestTreeListingDispatchesConfiguredSource(t *testing.T) {
	root, out := tree(t), t.TempDir()
	pages, manifest := config.SourcePages, config.SourceManifest

	if err := os.MkdirAll(filepath.Join(root, "external"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "external", ".cairndex.yaml"),
		[]byte("entries:\n  - name: upstream.iso\n    path: https://example.invalid/upstream.iso\n    title: Fetched elsewhere\n"),
		0o644); err != nil {
		t.Fatal(err)
	}

	yes := true
	outs := []string{config.OutputJSON}
	c := conf([]config.Rule{
		{Match: "docs/**", Override: config.Override{Source: &pages}},
		{Match: "external/**", Override: config.Override{Source: &manifest}},
	})
	c.Defaults.Recursive = &yes
	c.Defaults.Outputs = &outs
	run(t, c, root, out)

	var foundPage, foundManifestEntry bool
	for _, e := range treeEntries(t, filepath.Join(out, "tree.json")) {
		if strings.Contains(e.Path, "docs/intro.md") {
			t.Errorf("tree.json read docs/ off the filesystem, ignoring source: pages: %+v", e)
		}
		if e.Name == "intro" && e.Path == "/docs/intro/" {
			foundPage = true
		}
		if e.Name == "upstream.iso" {
			foundManifestEntry = true
		}
	}
	if !foundPage {
		t.Error("tree.json did not dispatch docs/ through the pages producer")
	}
	if !foundManifestEntry {
		t.Error("tree.json did not dispatch external/ through the manifest producer")
	}
}

// The parent row, end to end: the top of the tree must not offer a link above
// itself, and every directory below it must. The unit test in internal/emit
// covers the switch; this covers the build actually setting it.
func TestOnlyTheTopListingOmitsTheParentRow(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	bare := config.PresentBare
	outs := []string{config.OutputHTML}
	c.Defaults = config.Override{Present: &bare, Outputs: &outs}
	run(t, c, root, out)

	read := func(rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(out, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if strings.Contains(read("index.html"), `href="../"`) {
		t.Error("the top listing links above the indexed tree")
	}
	for _, rel := range []string{"bootstrap/index.html", "bootstrap/linux/index.html"} {
		if !strings.Contains(read(rel), `href="../"`) {
			t.Errorf("%s has no way back up", rel)
		}
	}
}

// Which files a directory gets and who renders each: the question emit.go
// answers. Driven through Run, because the dispatch and the writing are only
// correct together.

func TestRunUnknownOutputIsAnError(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	bogus := []string{"pdf"}
	c.Defaults = config.Override{Outputs: &bogus}
	if _, err := Run(context.Background(), c, root, out, obs.Discard()); err == nil {
		t.Fatal("expected an error for an unknown output format")
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
	// reader fetches are cairndex's own.
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
	if _, err := Run(context.Background(), c, root, out, obs.Discard()); err == nil {
		t.Fatal("expected a conflict when both html and pep503 target index.html")
	}
}

// In hugo mode every non-HTML output is a bundle resource Hugo publishes
// verbatim, so cairndex writes them all rather than declaring output formats.
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
	// Hugo renders the HTML; cairndex must not also write it.
	if _, err := os.Stat(filepath.Join(out, "bootstrap", "index.html")); err == nil {
		t.Error("cairndex wrote index.html in hugo mode; Hugo renders that")
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

// Where the standalone search index lands, which is the whole of the decision
// emitSearch makes: the emitter in internal/emit is a projection, the placement
// is the logic.

func readSearch(t *testing.T, path string) []map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no search index at %s: %v", path, err)
	}
	var records []map[string]any
	if err := json.Unmarshal(b, &records); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return records
}

func names(records []map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, r := range records {
		out[r["name"].(string)] = true
	}
	return out
}

// Without recursion the index describes the directory it sits in, the way
// index.json does.
func TestSearchIndexScopedToItsDirectory(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	c.Defaults = config.Override{Outputs: &[]string{config.OutputJSON, config.OutputSearch}}
	run(t, c, root, out)

	got := names(readSearch(t, filepath.Join(out, "bootstrap", "search-index.json")))
	if !got["bootstrap.sh"] || !got["linux"] {
		t.Errorf("bootstrap index = %v, want its own children", got)
	}
	if got["apt.list"] {
		t.Error("bootstrap index reached into linux/ without recursion")
	}
}

// With recursion the subtree is what is worth searching: an index covering one
// directory of a deep tree finds almost nothing.
func TestSearchIndexCoversTheSubtree(t *testing.T) {
	root, out := tree(t), t.TempDir()
	yes := true
	c := conf([]config.Rule{{
		Match: "bootstrap/**",
		Override: config.Override{
			Recursive: &yes,
			Outputs:   &[]string{config.OutputJSON, config.OutputSearch},
		},
	}})
	run(t, c, root, out)

	got := names(readSearch(t, filepath.Join(out, "bootstrap", "search-index.json")))
	if !got["apt.list"] {
		t.Errorf("recursive index = %v, want the nested file in it", got)
	}
}

// The filename is fixed, so both listings would write it under recursion and
// the second would overwrite the first with less. The subtree must win.
func TestSearchIndexWrittenOncePerDirectory(t *testing.T) {
	root, out := tree(t), t.TempDir()
	yes := true
	c := conf([]config.Rule{{
		Match: "bootstrap/**",
		Override: config.Override{
			Recursive: &yes,
			Outputs:   &[]string{config.OutputJSON, config.OutputSearch},
		},
	}})
	run(t, c, root, out)

	// tree.json exists, so both listings were emitted; the search index must
	// still hold the recursive one rather than the directory-only one.
	if _, err := os.Stat(filepath.Join(out, "bootstrap", "tree.json")); err != nil {
		t.Fatalf("expected a recursive listing to have been written: %v", err)
	}
	if got := names(readSearch(t, filepath.Join(out, "bootstrap", "search-index.json"))); !got["apt.list"] {
		t.Errorf("index = %v, want the subtree; the directory-only pass overwrote it", got)
	}
}

// cairndex excludes its own output from the listings it writes. A search index
// that offers itself as a result is noise on every query.
//
// Root and out are the same directory here, which is both the arrangement the
// deployment guide describes and the only one that can catch this: with output
// written elsewhere cairndex never walks over what it wrote, and the test passes
// whether or not the exclusion exists.
func TestSearchIndexDoesNotListItself(t *testing.T) {
	root := tree(t)
	c := conf(nil)
	c.Defaults = config.Override{Outputs: &[]string{config.OutputJSON, config.OutputSearch}}
	run(t, c, root, root)
	// Twice: the first run leaves the file on disk for the second to find.
	run(t, c, root, root)

	got := names(readSearch(t, filepath.Join(root, "search-index.json")))
	if got["search-index.json"] {
		t.Error("the search index lists itself")
	}
	if got["index.json"] {
		t.Error("the search index lists cairndex's own listing")
	}
}
