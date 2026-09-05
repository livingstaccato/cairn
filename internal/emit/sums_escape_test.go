// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for filenames SHA256SUMS cannot hold literally.

package emit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/livingstaccato/cairn/internal/model"
)

// A newline in a filename splits the line, so `sha256sum -c` reads one entry as
// two: a digest naming a file that does not exist, and a malformed remainder.
// The real file is then never verified and nothing says so — a silent hole in
// the one output whose whole purpose is integrity.
//
// coreutils answers this by prefixing the line with a backslash and escaping
// the name. The digests are meant to be checked by `sha256sum -c` with no
// bespoke verifier, so cairn writes what that program writes.
func TestSumsEscapesNamesThatWouldBreakTheFormat(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"a\nb.txt", "\\DIGEST  a\\nb.txt"},
		{"a\rb.txt", "\\DIGEST  a\\rb.txt"},
		{"a\\b.txt", "\\DIGEST  a\\\\b.txt"},
		{"a\\\nb.txt", "\\DIGEST  a\\\\\\nb.txt"},
		// A space needs nothing: everything after the separator is the name.
		{"a b.txt", "DIGEST  a b.txt"},
		{"ok.txt", "DIGEST  ok.txt"},
	}
	const digest = "2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db0225871792 1a4881"
	sum := strings.ReplaceAll(digest, " ", "")

	for _, tc := range cases {
		got := string(Sums(model.Listing{Entries: []model.Entry{
			{Name: tc.name, SHA256: sum},
		}}))
		want := strings.ReplaceAll(tc.want, "DIGEST", sum) + "\n"
		if got != want {
			t.Errorf("name %q\n got %q\nwant %q", tc.name, got, want)
		}
	}
}

// Every line has to stay one line, whatever the name holds.
func TestSumsAlwaysWritesOneLinePerEntry(t *testing.T) {
	sum := strings.Repeat("a", 64)
	l := model.Listing{Entries: []model.Entry{
		{Name: "one\ntwo\nthree.txt", SHA256: sum},
		{Name: "plain.txt", SHA256: sum},
	}}
	body := strings.TrimRight(string(Sums(l)), "\n")
	if n := strings.Count(body, "\n") + 1; n != 2 {
		t.Errorf("two entries produced %d lines:\n%q", n, body)
	}
}

// The claim tested against the tool rather than asserted, with the names that
// break the format. A file whose name holds a newline must verify, and must be
// reported by name — not silently skipped while a phantom entry fails beside it.
func TestSumsWithHostileNamesVerifiesWithCoreutils(t *testing.T) {
	bin := gnuSha256sum(t)

	names := []string{
		"ok.txt",
		"a b.txt",
		"a\nb.txt",
		"a\rb.txt",
		"a\\b.txt",
		"a\\\nb.txt",
		"-leading-dash.txt",
		"a\tb.txt",
	}

	dir := t.TempDir()
	var entries []model.Entry
	for i, name := range names {
		body := fmt.Sprintf("body %d\n", i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Skipf("this filesystem will not hold %q: %v", name, err)
		}
		sum := sha256.Sum256([]byte(body))
		entries = append(entries, model.Entry{Name: name, SHA256: hex.EncodeToString(sum[:])})
	}

	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"),
		Sums(model.Listing{Entries: entries}), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "-c", "SHA256SUMS")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s -c failed: %v\n%s", bin, err, out)
	}
	// Every file accounted for, and no line the tool could not read. A warning
	// about a malformed line is the exact failure this escaping exists to stop,
	// and coreutils reports it without failing the run.
	if n := strings.Count(string(out), ": OK"); n != len(names) {
		t.Errorf("%d files verified, want %d:\n%s", n, len(names), out)
	}
	if strings.Contains(string(out), "improperly formatted") {
		t.Errorf("coreutils could not read a line cairn wrote:\n%s", out)
	}
}

// gnuSha256sum finds an implementation that speaks the escaping convention.
//
// Apple ships its own sha256sum, which reads a plain line and reports a
// backslash-prefixed one as improperly formatted. Escaping is still what cairn
// must write — a mirror is verified by GNU coreutils on the client side, and
// without it a newline in a filename corrupts the file for every
// implementation, Apple's included. This test just needs the tool that can read
// what it produces.
func gnuSha256sum(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"gsha256sum", "sha256sum"} {
		bin, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		out, err := exec.Command(bin, "--version").CombinedOutput()
		if err == nil && strings.Contains(string(out), "GNU coreutils") {
			return bin
		}
	}
	t.Skip("no GNU sha256sum on PATH")
	return ""
}
