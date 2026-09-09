// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Fuzz targets for reading back what a build wrote.

package verify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/livingstaccato/cairndex/internal/emit"
	"github.com/livingstaccato/cairndex/internal/model"
)

var hostileNames = []string{
	"", "ok.txt", "a b.txt", "a\nb.txt", "a\rb.txt", "a\\b.txt", "a\\\nb.txt",
	"a\\\\b.txt", "\\leading", "trailing\\", "a\tb.txt", "..", ".", "../up",
	"\x00null", "üñïçødé", strings.Repeat("x", 300),
}

// The two halves have to agree. emit escapes a name that cannot be written
// literally; verify undoes it. If they disagree, cairndex reports its own intact
// artifacts as missing — a verification tool crying wolf about files it wrote
// itself, which is worse than not having one.
func FuzzSumsRoundTrip(f *testing.F) {
	for _, n := range hostileNames {
		f.Add(n)
	}
	sum := strings.Repeat("a", 64)

	f.Fuzz(func(t *testing.T, name string) {
		body := string(emit.Sums(model.Listing{Entries: []model.Entry{
			{Name: name, SHA256: sum},
		}}))
		if body == "" {
			return
		}
		line := strings.TrimSuffix(body, "\n")

		gotSum, gotName, ok := parseSumsLine(line)
		if !ok {
			// Refusing is only correct for a name no line can carry back.
			// Anything emit wrote, verify has to be able to read.
			t.Fatalf("emit wrote a line verify rejects\nname %q\nline %q", name, line)
		}
		if gotSum != sum {
			t.Fatalf("digest came back as %q, want %q", gotSum, sum)
		}
		if gotName != name {
			t.Fatalf("name did not survive the round trip\n sent %q\n got  %q\n line %q",
				name, gotName, line)
		}
	})
}

// A checksum file on a published mirror is fetched over plain HTTP by
// definition — that is what it exists to compensate for. Every line is
// therefore untrusted input, and no line may panic the reader or come back
// carrying a path that walks out of the directory it describes.
func FuzzParseSumsLine(f *testing.F) {
	seeds := []string{
		"", "  ", strings.Repeat("a", 64) + "  name.txt",
		strings.Repeat("a", 64) + " *binary.txt",
		"\\" + strings.Repeat("a", 64) + `  a\nb.txt`,
		"\\" + strings.Repeat("a", 64) + `  a\qb.txt`,
		"\\" + strings.Repeat("a", 64) + `  trailing\`,
		// Found by fuzzing: an unescaped line naming a file called literally
		// backslash-n. Read as written, because nothing announced an escape.
		strings.Repeat("0", 64) + `  \n`,
		strings.Repeat("z", 64) + "  bad-hex.txt",
		strings.Repeat("a", 63) + "  short.txt",
		strings.Repeat("a", 64) + "  ../escape",
		strings.Repeat("a", 64) + "  .",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, line string) {
		sum, name, ok := parseSumsLine(line)
		if !ok {
			return
		}
		if len(sum) != digestLen || !isHex(sum) {
			t.Fatalf("accepted line %q with digest %q", line, sum)
		}
		if name == "" || name == "." || name == ".." {
			t.Fatalf("accepted line %q naming %q", line, name)
		}
		// Deliberately no assertion about backslashes in the result. A line with
		// no leading backslash is not escaped, so it names a file called
		// literally \n if that is what it says; and an escaped `\\n` decodes to
		// a name that still holds a backslash followed by an n. Both are real
		// filenames. Whether emit and verify agree is FuzzSumsRoundTrip's
		// question, not this one.
	})
}

// The manifest is read from the output directory, which on a mirror is served
// and writable by whatever publishes it. It has to survive arbitrary bytes
// without panicking, and must never hand back a digest that is not one.
func FuzzParseManifest(f *testing.F) {
	seeds := []string{
		"", "{}", "null", "[]", `["a"]`, "{not json",
		`{"version":1,"outputs":{}}`,
		`{"version":1,"outputs":{"a":"` + strings.Repeat("a", 64) + `"}}`,
		`{"version":1,"outputs":{"a":"short"}}`,
		`{"version":1,"outputs":{"../escape":"` + strings.Repeat("a", 64) + `"}}`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, body string) {
		got, err := emit.ParseManifest([]byte(body))
		if err != nil {
			return
		}
		for claim, digest := range got {
			if len(digest) != digestLen || !isHex(digest) {
				t.Fatalf("accepted %q claiming %q with digest %q", body, claim, digest)
			}
		}
	})
}

// containedIn is the read side's guard on every path a manifest, a SHA256SUMS or
// a removal names — the same property emit.FuzzContainedPath proves for the
// write side, reimplemented independently here and never itself fuzzed. If any
// input resolves outside the output root, cairndex can be made to read or delete
// anywhere the process can reach.
func FuzzContainedIn(f *testing.F) {
	for _, n := range hostileNames {
		f.Add(n)
	}

	// Resolved once, outside the loop, for the reason FuzzContainedPath's own
	// comment gives: the root does not vary, and a filesystem call per execution
	// holds a native-speed fuzz target to a syscall-bound crawl.
	root := f.TempDir()
	resolved, err := resolveRoot(root)
	if err != nil {
		f.Fatal(err)
	}

	f.Fuzz(func(t *testing.T, rel string) {
		got, err := containedIn(resolved, rel)
		if err != nil {
			return // refusing is always a correct answer
		}
		if got != resolved && !strings.HasPrefix(got, resolved+string(filepath.Separator)) {
			t.Fatalf("rel %q escaped the root:\n got %q\nroot %q", rel, got, resolved)
		}
	})
}

// verifiedAncestors is called on a rel that has already been through
// containedIn in every real caller, but its own contract — no panic, whatever
// string it is handed — should not depend on that. A path it cannot classify is
// a path it refuses, never one that crashes the process running a destructive
// command.
func FuzzVerifiedAncestorsNeverPanics(f *testing.F) {
	for _, n := range hostileNames {
		f.Add(n)
	}
	f.Add("../../../../etc/passwd")
	f.Add(strings.Repeat("a/", 200) + "x")

	root := f.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a", "b"), 0o755); err != nil {
		f.Fatal(err)
	}
	resolved, err := resolveRoot(root)
	if err != nil {
		f.Fatal(err)
	}

	f.Fuzz(func(t *testing.T, rel string) {
		_ = verifiedAncestors(resolved, rel) // must not panic
	})
}

// provenOursNames are every basename provenOurs branches on. A selector index
// rather than a fuzzed string: the property is "no panic for any of these
// names given arbitrary content", and a free-form name would spend the whole
// budget on strings that hit no branch at all.
var provenOursNames = []string{
	"index.json", "tree.json", "index.csv", "tree.csv",
	emit.SearchFile, emit.HugoContentFile, emit.SumsFile,
	"index.html", "index.txt", "tree.txt",
}

// provenOurs decides deletion from bytes read off disk — a file cairndex did not
// necessarily write, sitting in a tree it is about to act on. Every other
// guard in this codebase fuzzed for crash-safety stands between a scanned tree
// and what cairndex writes; this is the one that stands between a foreign tree
// and what cairndex destroys, and it had no fuzz coverage at all. isSearchIndex
// accepting a bare "[]" was found by reading the code, not by fuzzing it, and a
// fuzzer seeded from real output would plausibly have found it: an empty array
// is one truncation away from anything in the corpus.
func FuzzProvenOursNeverPanics(f *testing.F) {
	seeds := [][]byte{
		nil, []byte(""), []byte("{}"), []byte("[]\n"), []byte("null"), []byte("["),
		[]byte(`{"path":"/x","generated":"2026-01-01T00:00:00Z","count":0,"entries":[],"generator":"cairndex"}`),
		[]byte(`{"path":"/x","generated":"2026-01-01T00:00:00Z","count":0,"entries":[]}`),
		[]byte(`[{"name":"a","path":"/a","kind":"other","is_dir":false}]`),
		[]byte("name,path,type,size,modified,sha256,kind,title\n"),
		[]byte("name,path\n"),
		[]byte("---\nlayout: cairndex\ncairndex:\n  present: bare\n---\n"),
		[]byte("---\ntitle: hand written\n---\n"),
		{0x00, 0xff, 0xfe, '\n'},
	}
	for i := range provenOursNames {
		for _, s := range seeds {
			f.Add(i, s)
		}
	}

	// One directory, made once, with one fixed path per name — reused and
	// overwritten every execution. A t.TempDir() per iteration held this to a few
	// hundred execs/sec against the hundreds of thousands every other target
	// here manages; FuzzContainedPath's own comment documents the same fix for
	// the same reason. Each fuzz worker is a separate process re-running this
	// setup on its own, so the directory is private to it and never shared.
	dir := f.TempDir()
	paths := make([]string, len(provenOursNames))
	for i, name := range provenOursNames {
		paths[i] = filepath.Join(dir, name)
	}

	f.Fuzz(func(t *testing.T, sel int, body []byte) {
		i := ((sel % len(provenOursNames)) + len(provenOursNames)) % len(provenOursNames)
		if err := os.WriteFile(paths[i], body, 0o644); err != nil {
			return
		}
		_ = provenOurs(paths[i]) // must not panic, whatever the bytes are
	})
}

// The two halves have to agree, the same principle FuzzSumsRoundTrip already
// holds emit and verify to: an entry name hostile enough to distort what an
// emitter produces must not also make provenOurs stop recognising cairndex's own
// real output. That failure is quiet and costs the opposite of a false
// positive — a file removal can never reclaim, forever, rather than one it
// should never have touched.
func FuzzProvenOursAcceptsItsOwnOutput(f *testing.F) {
	for _, n := range hostileNames {
		f.Add(n)
	}

	// One directory and one fixed path per format, made once and overwritten
	// every execution — t.TempDir() per format per iteration made this the
	// slowest target in the package by two orders of magnitude, for the same
	// reason FuzzProvenOursNeverPanics was. Private per fuzz worker, the same
	// as there.
	dir := f.TempDir()
	jsonPath := filepath.Join(dir, "index.json")
	csvPath := filepath.Join(dir, "index.csv")
	searchPath := filepath.Join(dir, emit.SearchFile)
	barePath := filepath.Join(dir, "index.html")
	hugoPath := filepath.Join(dir, emit.HugoContentFile)

	f.Fuzz(func(t *testing.T, name string) {
		l := model.Listing{
			Path: "/x", Generated: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Count: 1, Entries: []model.Entry{{Name: name, Path: "/x/" + name}},
			Generator: model.Generator,
		}

		check := func(rel, p string, body []byte, err error) {
			t.Helper()
			if err != nil {
				return // a name this hostile need not be encodable at all
			}
			if err := os.WriteFile(p, body, 0o644); err != nil {
				return
			}
			if !provenOurs(p) {
				t.Fatalf("name %q made %s unrecognisable as cairndex's own:\n%s", name, rel, body)
			}
		}

		jsonBody, err := emit.JSON(l)
		check("index.json", jsonPath, jsonBody, err)

		csvBody, err := emit.CSV(l, false)
		check("index.csv", csvPath, csvBody, err)

		searchBody, err := emit.Search(l)
		check(emit.SearchFile, searchPath, searchBody, err)

		bareBody, err := emit.BareHTML(emit.BarePage{Listing: l})
		check("index.html", barePath, bareBody, err)

		hugoBody, err := emit.HugoContent(emit.HugoPage{Listing: l, Present: "bare"})
		check(emit.HugoContentFile, hugoPath, hugoBody, err)
	})
}
