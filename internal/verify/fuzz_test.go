// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Fuzz targets for reading back what a build wrote.

package verify

import (
	"strings"
	"testing"

	"github.com/livingstaccato/cairn/internal/emit"
	"github.com/livingstaccato/cairn/internal/model"
)

var hostileNames = []string{
	"", "ok.txt", "a b.txt", "a\nb.txt", "a\rb.txt", "a\\b.txt", "a\\\nb.txt",
	"a\\\\b.txt", "\\leading", "trailing\\", "a\tb.txt", "..", ".", "../up",
	"\x00null", "üñïçødé", strings.Repeat("x", 300),
}

// The two halves have to agree. emit escapes a name that cannot be written
// literally; verify undoes it. If they disagree, cairn reports its own intact
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
