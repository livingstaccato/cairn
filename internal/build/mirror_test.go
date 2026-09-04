// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for the deployment cairn was written for: indexes written in place,
// beside artifacts a package manager owns and verifies.

package build

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/livingstaccato/cairn/internal/emit"
	"github.com/livingstaccato/cairn/internal/model"
	"github.com/livingstaccato/cairn/internal/obs"
)

func TestRunSkipsProtectedOutputs(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	c.Protect = []string{"bootstrap/**"}
	res := run(t, c, root, out)

	if _, err := os.Stat(filepath.Join(out, "bootstrap/index.json")); err == nil {
		t.Error("wrote into a protected subtree")
	}
	if _, err := os.Stat(filepath.Join(out, "docs/index.json")); err != nil {
		t.Errorf("an unprotected sibling must still be indexed: %v", err)
	}
	if res.Protected == 0 {
		t.Error("Result.Protected = 0; the operator has no way to see what was skipped")
	}
}

// repoTree builds a mirror shaped like a real APT and YUM repository: signed
// dists/ metadata, a package pool, and repodata/. This is the deployment cairn
// exists for — indexes written beside artifacts that another tool owns.
func repoTree(t *testing.T) string {
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
	mk("dists/stable/Release", "Origin: example\nSuite: stable\nSHA256:\n ab12 9 main/binary-amd64/Packages\n")
	mk("dists/stable/InRelease", "Origin: example\nSuite: stable\n")
	mk("dists/stable/Release.gpg", "-----BEGIN PGP SIGNATURE-----\n")
	mk("dists/stable/main/binary-amd64/Packages", "Package: nginx\nVersion: 1.24.0-1\n")
	mk("dists/stable/main/binary-amd64/Release", "Component: main\n")
	mk("repodata/repomd.xml", `<repomd><data type="primary"/></repomd>`)
	mk("repodata/aa-primary.xml.gz", "\x1f\x8bfake")
	mk("pool/main/n/nginx/nginx_1.24.0-1_amd64.deb", "deb payload\n")
	mk("pool/main/c/curl/curl_8.5.0-1_amd64.deb", "deb payload\n")
	mk("Packages/n/nginx-1.24.0-1.x86_64.rpm", "rpm payload\n")
	return root
}

// snapshot records every file in a tree with its contents, so a later
// comparison can prove cairn did not touch what it does not own.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	got := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p) // #nosec G304 -- test-owned tempdir
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		got[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// TestRunCoexistsWithPackageRepo is the deployment this tool was written for:
// cairn indexing an APT/YUM mirror in place, beside metadata that apt and dnf
// own and verify. It must leave every byte of that metadata alone, write no
// output inside it, still index the pool, and still list the protected
// directories so a browser can walk into them.
func TestRunCoexistsWithPackageRepo(t *testing.T) {
	root := repoTree(t)
	before := snapshot(t, root)

	c := conf(nil)
	c.Protect = []string{"dists/**", "repodata/**"}
	res := run(t, c, root, root) // in place: out == root

	// Nothing apt or dnf owns may change.
	for rel, body := range before {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))) // #nosec G304
		if err != nil {
			t.Fatalf("%s disappeared: %v", rel, err)
		}
		if string(b) != body {
			t.Errorf("%s changed; cairn must not rewrite content it did not create", rel)
		}
	}

	// No cairn output anywhere inside a protected subtree. An extra file will
	// not break apt, which fetches only what Release names, but it does put
	// cairn's output inside a directory whose checksums another tool signs.
	for rel := range snapshot(t, root) {
		if _, was := before[rel]; was {
			continue
		}
		if strings.HasPrefix(rel, "dists/") || strings.HasPrefix(rel, "repodata/") {
			t.Errorf("wrote %s into a protected subtree", rel)
		}
	}

	// The pool and the RPM directory are cairn's to index.
	for _, rel := range []string{
		"pool/index.json", "pool/main/n/nginx/index.json",
		"Packages/n/index.json", "index.json",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}

	// A protected directory is still content: it must appear in its parent's
	// listing, or a machine reading index.json cannot discover dists/ at all.
	b, err := os.ReadFile(filepath.Join(root, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var l model.Listing
	if err := json.Unmarshal(b, &l); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, e := range l.Entries {
		seen[e.Name] = true
	}
	for _, want := range []string{"dists", "repodata", "pool", "Packages"} {
		if !seen[want] {
			t.Errorf("root listing omits %s; entries = %v", want, seen)
		}
	}
	if res.Protected == 0 {
		t.Error("Result.Protected = 0; skipped writes must be reported")
	}
}

// TestRunOverPackageRepoIsIdempotent proves the in-place mirror reaches a fixed
// point. It must, or every rebuild changes files rsync then re-ships and
// SHA256SUMS never settles.
func TestRunOverPackageRepoIsIdempotent(t *testing.T) {
	root := repoTree(t)
	c := conf(nil)
	c.Protect = []string{"dists/**", "repodata/**"}

	run(t, c, root, root)
	first := snapshot(t, root)
	run(t, c, root, root)
	second := snapshot(t, root)

	for rel, body := range first {
		// The listing timestamp moves every run by design; the digests and the
		// entry set must not.
		if rel == emit.ManifestFile || strings.HasSuffix(rel, ".cairn-cache.json") {
			continue
		}
		got, ok := second[rel]
		if !ok {
			t.Errorf("%s vanished on the second run", rel)
			continue
		}
		if strings.HasSuffix(rel, "SHA256SUMS") && got != body {
			t.Errorf("%s is not stable across runs", rel)
		}
	}
	for rel := range second {
		if _, ok := first[rel]; !ok {
			t.Errorf("the second run invented %s", rel)
		}
	}
}

// TestRunFailureLeavesRecoverableState checks that a build dying partway can be
// retried. Without recording what it wrote before the error, that output belongs
// to nobody and on_conflict: error refuses every later run until an operator
// deletes the files by hand. A mirror that cannot be rebuilt without manual
// cleanup is not usable.
func TestRunFailureLeavesRecoverableState(t *testing.T) {
	root := tree(t)
	// A file cairn does not own, deep enough that the root listing is written
	// before the build reaches it. This is the ordinary way a real build dies.
	if err := os.WriteFile(filepath.Join(root, "docs", "index.json"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := conf(nil)
	if _, err := Run(c, root, root, obs.Discard()); err == nil {
		t.Fatal("expected the conflict in docs/ to fail the build")
	}
	if _, err := os.Stat(filepath.Join(root, "index.json")); err != nil {
		t.Fatalf("the test needs the root listing written before the failure: %v", err)
	}

	// Clear the conflict. The retry must succeed: cairn has to recognize the
	// output its own aborted run left behind.
	if err := os.Remove(filepath.Join(root, "docs", "index.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(c, root, root, obs.Discard()); err != nil {
		t.Fatalf("a retry after a failed run must succeed, got: %v", err)
	}
}

// TestStyledHTMLInDirectModeWarns covers a silent failure that was the default
// outcome: present: styled is the default, the styled presenter is a Hugo
// template, and direct mode simply wrote no index.html and said nothing.
func TestStyledHTMLInDirectModeWarns(t *testing.T) {
	root, out := tree(t), t.TempDir()
	var buf bytes.Buffer
	c := conf(nil)
	if _, err := Run(c, root, out, slog.New(slog.NewTextHandler(&buf, nil))); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "index.html")); err == nil {
		t.Fatal("direct mode cannot render the styled presenter; this test is stale")
	}
	if !strings.Contains(buf.String(), "no HTML written") {
		t.Errorf("no diagnostic for a request that produced nothing; log = %s", buf.String())
	}
	if n := strings.Count(buf.String(), "no HTML written"); n != 1 {
		t.Errorf("warned %d times; a config-level problem warrants one line per run", n)
	}
}
