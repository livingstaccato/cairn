// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package verify

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/livingstaccato/cairn/internal/build"
	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/emit"
	"github.com/livingstaccato/cairn/internal/obs"
)

// fixture is one tree under verification. root and out are separate fields
// because the two deployments differ in exactly that: a mirror sets them to the
// same directory, a split build does not, and every path question in this
// package turns on which of the two a name is relative to.
type fixture struct {
	t    *testing.T
	root string
	out  string
	cfg  *config.Config
	log  *slog.Logger
	logs *bytes.Buffer
}

// mirror is the deployment cairn exists for: indexes written into the artifact
// tree itself.
func mirror(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	return newFixture(t, dir, dir)
}

// split writes indexes to a separate directory, so SHA256SUMS under out names
// files that live under root and nowhere else.
func split(t *testing.T) *fixture {
	t.Helper()
	return newFixture(t, t.TempDir(), t.TempDir())
}

func newFixture(t *testing.T, root, out string) *fixture {
	t.Helper()
	buf := &bytes.Buffer{}
	return &fixture{
		t:    t,
		root: root,
		out:  out,
		cfg:  conf(),
		log:  slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
		logs: buf,
	}
}

func conf() *config.Config {
	return &config.Config{
		Version:       config.SupportedVersion,
		Mode:          config.ModeDirect,
		IndexBasename: config.DefaultIndexBasename,
		OnConflict:    config.ConflictError,
	}
}

// file writes an artifact into the indexed root.
func (f *fixture) file(rel, body string) {
	f.t.Helper()
	f.writeAt(f.root, rel, body)
}

// outFile writes generated output into the output tree.
func (f *fixture) outFile(rel, body string) {
	f.t.Helper()
	f.writeAt(f.out, rel, body)
}

func (f *fixture) writeAt(base, rel, body string) {
	f.t.Helper()
	p := filepath.Join(base, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// manifest records the paths cairn claims, exactly as emit.Writer would.
func (f *fixture) manifest(paths ...string) {
	f.t.Helper()
	b, err := json.Marshal(paths)
	if err != nil {
		f.t.Fatal(err)
	}
	f.outFile(emit.ManifestFile, string(b))
}

// sums writes a SHA256SUMS under out/dir covering names that live under
// root/dir, with the digests the files actually have.
func (f *fixture) sums(dir string, names ...string) {
	f.t.Helper()
	var b strings.Builder
	for _, n := range names {
		b.WriteString(f.digestOf(dir, n) + "  " + n + "\n")
	}
	f.outFile(join(dir, emit.SumsFile), b.String())
}

// rawSums writes a SHA256SUMS verbatim, for lines no emitter would produce.
func (f *fixture) rawSums(dir, body string) {
	f.t.Helper()
	f.outFile(join(dir, emit.SumsFile), body)
}

func (f *fixture) digestOf(dir, name string) string {
	f.t.Helper()
	b, err := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(join(dir, name))))
	if err != nil {
		f.t.Fatal(err)
	}
	return digest(string(b))
}

func digest(body string) string {
	s := sha256.Sum256([]byte(body))
	return hex.EncodeToString(s[:])
}

func join(dir, name string) string {
	if dir == "" || dir == "." {
		return name
	}
	return dir + "/" + name
}

func (f *fixture) run() *Report {
	f.t.Helper()
	r, err := Run(f.cfg, f.root, f.out, f.log)
	if err != nil {
		f.t.Fatalf("Run: %v", err)
	}
	return r
}

func (f *fixture) runErr() error {
	f.t.Helper()
	_, err := Run(f.cfg, f.root, f.out, f.log)
	return err
}

func eq(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s = %v, want %v", label, got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s = %v, want %v", label, got, want)
			return
		}
	}
}

// An intact mirror is the case that must stay quiet: any false positive here
// trains an operator to ignore the command.
func TestIntactMirrorIsOK(t *testing.T) {
	f := mirror(t)
	f.file("pool/nginx.deb", "deb payload\n")
	f.file("pool/curl.deb", "curl payload\n")
	f.outFile("index.json", `{"path":"/"}`)
	f.outFile("pool/index.json", `{"path":"/pool"}`)
	f.sums("pool", "curl.deb", "nginx.deb")
	f.manifest("index.json", "pool/index.json", "pool/SHA256SUMS")

	r := f.run()
	if !r.OK() {
		t.Fatalf("intact mirror reported dirty: %+v", r)
	}
	if r.Checked != 2 {
		t.Errorf("Checked = %d, want 2", r.Checked)
	}
}

func TestOKRequiresEveryListEmpty(t *testing.T) {
	cases := []struct {
		name string
		r    Report
		want bool
	}{
		{"clean", Report{}, true},
		{"counted but clean", Report{Checked: 9}, true},
		{"missing", Report{Missing: []string{"a"}}, false},
		{"modified", Report{Modified: []string{"a"}}, false},
		{"orphaned", Report{Orphaned: []string{"a"}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.r.OK(); got != c.want {
				t.Errorf("OK() = %v, want %v", got, c.want)
			}
		})
	}
}

// A tree cairn has never built is a legitimate thing to point verify at, so it
// must not error — but it is also not clean, and saying so is the whole point.
func TestMissingManifestClaimsNothing(t *testing.T) {
	f := mirror(t)
	f.file("pool/nginx.deb", "deb payload\n")
	f.outFile("pool/index.json", `{"path":"/pool"}`)

	r := f.run()
	eq(t, "Missing", r.Missing, nil)
	eq(t, "Orphaned", r.Orphaned, []string{"pool/index.json"})
}

// A manifest that exists but does not parse is not an empty manifest. Treating
// it as one would report every generated file as an orphan, which is a lie
// about the tree rather than a finding.
func TestUnreadableManifestIsAnError(t *testing.T) {
	f := mirror(t)
	f.outFile(emit.ManifestFile, "{not json")

	err := f.runErr()
	if err == nil {
		t.Fatal("a corrupt manifest must not verify as clean")
	}
	if !strings.Contains(err.Error(), emit.ManifestFile) {
		t.Errorf("error must name the file: %v", err)
	}
}

func TestMissingOutputTreeIsAnError(t *testing.T) {
	f := mirror(t)
	f.out = filepath.Join(f.out, "nope")
	if err := f.runErr(); err == nil {
		t.Fatal("verifying a directory that is not there must report why")
	}
}

// Deterministic output is a reporting requirement: a diff of two verify runs
// should show what changed in the tree, not what changed in map iteration.
func TestReportSlicesAreSorted(t *testing.T) {
	f := mirror(t)
	f.manifest("z.json", "a.json", "m.json", "q.json", "b.json", "n.json")
	for _, d := range []string{"d", "a", "c", "b"} {
		f.outFile(d+"/index.json", "{}")
	}

	r := f.run()
	eq(t, "Missing", r.Missing,
		[]string{"a.json", "b.json", "m.json", "n.json", "q.json", "z.json"})
	eq(t, "Orphaned", r.Orphaned,
		[]string{"a/index.json", "b/index.json", "c/index.json", "d/index.json"})
}

// Map iteration is randomized, but for a small map the randomization is a
// rotation: one unsorted set can come out in order by chance often enough that
// a single assertion proves nothing. Repeating it is what makes the claim.
func TestSortedIsOrderedEveryTime(t *testing.T) {
	set := map[string]bool{}
	for _, k := range []string{"h", "c", "a", "f", "b", "g", "d", "e"} {
		set[k] = true
	}
	want := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	for i := 0; i < 32; i++ {
		eq(t, "sorted", sorted(set), want)
	}
	if sorted(map[string]bool{}) != nil {
		t.Error("an empty set must produce no slice, not an empty one")
	}
}

func ptr[T any](v T) *T { return &v }

// The claim this package makes is that a tree cairn has just built verifies
// clean. It is asserted against a real build rather than a hand-made fixture
// because every path decision here is a claim about what build.Run writes —
// which root a digest line resolves against, which basenames no manifest ever
// claims — and only build.Run can settle those.
func TestFreshBuildVerifiesClean(t *testing.T) {
	f := freshBuild(t)

	r := f.run()
	if !r.OK() {
		t.Fatalf("a tree cairn just built reported dirty: %+v", r)
	}
	if r.Checked < 3 {
		t.Errorf("Checked = %d, want every hashed artifact re-read", r.Checked)
	}
}

// The same tree with one artifact rewritten underneath it: the finding an
// operator runs this command to get.
func TestFreshBuildCatchesATamperedArtifact(t *testing.T) {
	f := freshBuild(t)
	f.file("pool/nginx.deb", "tampered payload\n")

	eq(t, "Modified", f.run().Modified, []string{"pool/nginx.deb"})
}

// A generated file removed from a built tree is the other half: the manifest
// still claims it and it is not there.
func TestFreshBuildCatchesADeletedIndex(t *testing.T) {
	f := freshBuild(t)
	if err := os.Remove(filepath.Join(f.out, "pool", "index.json")); err != nil {
		t.Fatal(err)
	}

	eq(t, "Missing", f.run().Missing, []string{"pool/index.json"})
}

// freshBuild runs cairn over a small mirror the way a mirror is configured:
// every output format, digests on, indexes written into the artifact tree.
func freshBuild(t *testing.T) *fixture {
	t.Helper()
	f := mirror(t)
	f.cfg.Defaults = config.Override{
		Present:  ptr(config.PresentBare),
		Checksum: ptr(config.ChecksumSHA256),
		Outputs: ptr([]string{
			config.OutputHTML, config.OutputJSON, config.OutputCSV,
			config.OutputText, config.OutputSums,
		}),
	}
	f.file("pool/nginx.deb", "deb payload\n")
	f.file("pool/curl.deb", "curl payload\n")
	f.file("docs/readme.txt", "prose\n")

	if _, err := build.Run(f.cfg, f.root, f.out, obs.Discard()); err != nil {
		t.Fatalf("build: %v", err)
	}
	return f
}
