// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Fuzz targets for the guards that stand between a scanned tree and what cairn
// writes. Every input here is a filename, and filenames in a mirror are
// attacker-influenced — which is why these are properties over all inputs
// rather than a table of the ones somebody thought of.

package emit

import (
	"encoding/csv"
	"path/filepath"
	"strings"
	"testing"

	"github.com/livingstaccato/cairn/internal/model"
)

// hostileNames seed every target. A corpus that starts from real breakage finds
// the neighbouring cases quickly.
var hostileNames = []string{
	"", "ok.txt", "a b.txt", "a\nb.txt", "a\rb.txt", "a\\b.txt", "a\\\nb.txt",
	"=cmd()", "+1", "-2", "@SUM", "\tlead", "\rlead",
	"../escape", "../../escape", "/absolute", "C:\\win", `a"b`, "a'b",
	"<script>alert(1)</script>", "javascript:alert(1)", "a&b", "%2e%2e",
	"\x00null", "\xff\xfe", "üñïçødé", strings.Repeat("x", 300),
}

// One entry is one line. A name holding a newline otherwise splits its entry in
// two: a digest naming a file that does not exist, and a malformed remainder —
// and the real file goes unverified with nothing to say so.
func FuzzSumsIsOneLinePerEntry(f *testing.F) {
	for _, n := range hostileNames {
		f.Add(n)
	}
	sum := strings.Repeat("a", 64)

	f.Fuzz(func(t *testing.T, name string) {
		out := string(Sums(model.Listing{Entries: []model.Entry{
			{Name: name, SHA256: sum},
		}}))
		if out == "" {
			return // no name to write is no line to check
		}
		if !strings.HasSuffix(out, "\n") {
			t.Fatalf("name %q produced a line with no terminator: %q", name, out)
		}
		if n := strings.Count(out, "\n"); n != 1 {
			t.Fatalf("name %q produced %d lines: %q", name, n, out)
		}
	})
}

// containedPath is the single guard on every write cairn makes. If any input
// resolves outside the output root, cairn can be made to write anywhere the
// process can reach.
func FuzzContainedPath(f *testing.F) {
	for _, n := range hostileNames {
		f.Add(n)
	}

	// Made and resolved once, outside the loop. This was a t.TempDir() per
	// execution, which put a mkdir and an rmdir on the hot path and held the
	// target to tens of executions a second where every other one manages tens
	// of thousands — slow enough that a loaded CI runner tripped the fuzzing
	// deadline and the gate reported it as a failing input. The root does not
	// have to vary: the property is about the paths, not the directory.
	root := f.TempDir()
	resolved, err := filepath.Abs(root)
	if err != nil {
		f.Fatal(err)
	}
	if resolved, err = filepath.EvalSymlinks(resolved); err != nil {
		f.Fatal(err)
	}

	f.Fuzz(func(t *testing.T, rel string) {
		got, err := containedPath(root, rel)
		if err != nil {
			return // refusing is always a correct answer
		}
		if got != resolved && !strings.HasPrefix(got, resolved+string(filepath.Separator)) {
			t.Fatalf("relPath %q escaped the root:\n got %q\nroot %q", rel, got, resolved)
		}
	})
}

// A spreadsheet executes a field that begins =, +, -, @, tab or CR. Whatever
// the name, the field cairn writes must not begin with one.
func FuzzCSVSafeNeutralizesFormulas(f *testing.F) {
	for _, n := range hostileNames {
		f.Add(n)
	}

	f.Fuzz(func(t *testing.T, name string) {
		got := csvSafe(name)
		if got == "" {
			return
		}
		switch got[0] {
		case '=', '+', '-', '@', '\t', '\r':
			t.Fatalf("name %q produced a field a spreadsheet executes: %q", name, got)
		}
	})
}

// index.csv has to parse as CSV for every name, or a consumer reading it gets a
// different number of rows than the directory holds.
func FuzzCSVStaysParseable(f *testing.F) {
	for _, n := range hostileNames {
		f.Add(n)
	}

	f.Fuzz(func(t *testing.T, name string) {
		body, err := CSV(model.Listing{Path: "/x", Entries: []model.Entry{
			{Name: name, Path: "/x/" + name},
		}})
		if err != nil {
			return
		}
		rows, err := csv.NewReader(strings.NewReader(string(body))).ReadAll()
		if err != nil {
			t.Fatalf("name %q produced CSV that does not parse: %v\n%s", name, err, body)
		}
		if len(rows) != 2 { // header plus the one entry
			t.Fatalf("name %q produced %d rows, want 2:\n%s", name, len(rows), body)
		}
	})
}

// The bare page renders in a browser. A name is content, never markup: no input
// may close a tag or introduce a script.
func FuzzBareHTMLEscapesNames(f *testing.F) {
	for _, n := range hostileNames {
		f.Add(n)
	}

	f.Fuzz(func(t *testing.T, name string) {
		body, err := BareHTML(model.Listing{Path: "/x", Entries: []model.Entry{
			{Name: name, Path: "/x/" + name},
		}}, "", 100)
		if err != nil {
			return
		}
		assertNoInjection(t, name, string(body))
	})
}

// PEP503 hands its href to template.URL, which suppresses contextual escaping.
// That is the one place in this package where escaping is opted out of, so it
// is the one that most needs proving over all inputs.
func FuzzPEP503EscapesNames(f *testing.F) {
	for _, n := range hostileNames {
		f.Add(n)
	}
	sum := strings.Repeat("a", 64)

	f.Fuzz(func(t *testing.T, name string) {
		// Path is the bare name as well as a rooted one: url.URL reads a
		// colon in the first segment as a scheme unless something stops it, and
		// that is exactly what template.URL would then hand to a browser.
		for _, p := range []string{"/x/" + name, name} {
			body, err := PEP503(model.Listing{Path: "/x", Entries: []model.Entry{
				{Name: name, Path: p, SHA256: sum},
			}})
			if err != nil {
				continue
			}
			assertNoInjection(t, name, string(body))
		}
	})
}

// assertNoInjection fails when rendered output carries markup the input smuggled
// in.
//
// Three checks, because each alone is weak. No script may appear at all. Every
// href must stay a link to a file — a name is not allowed to become a scheme,
// which is the specific risk of PEP503 handing template.URL a value and
// suppressing contextual escaping. And a name holding an HTML metacharacter has
// to show it escaped, which is what proves the other two passed because
// escaping happened rather than because the input was harmless.
func assertNoInjection(t *testing.T, name, body string) {
	t.Helper()
	lower := strings.ToLower(body)
	for _, bad := range []string{"<script", "onerror=", "onload="} {
		if strings.Contains(lower, bad) {
			t.Fatalf("name %q put %q into the page:\n%s", name, bad, body)
		}
	}
	for _, href := range hrefs(body) {
		trimmed := strings.ToLower(strings.TrimLeft(href, " \t\r\n"))
		for _, scheme := range []string{"javascript:", "data:", "vbscript:"} {
			if strings.HasPrefix(trimmed, scheme) {
				t.Fatalf("name %q produced href %q, which is not a link to a file", name, href)
			}
		}
	}
	for raw, escaped := range map[string]string{"<": "&lt;", ">": "&gt;"} {
		if strings.Contains(name, raw) && !strings.Contains(body, escaped) {
			t.Fatalf("name %q holds %q and the page shows no %q:\n%s", name, raw, escaped, body)
		}
	}
}

// hrefs pulls every href value out of rendered markup.
func hrefs(body string) []string {
	var out []string
	for rest := body; ; {
		i := strings.Index(rest, `href="`)
		if i < 0 {
			return out
		}
		rest = rest[i+len(`href="`):]
		j := strings.IndexByte(rest, '"')
		if j < 0 {
			return out
		}
		out = append(out, rest[:j])
		rest = rest[j:]
	}
}
