// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package verify

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/livingstaccato/cairn/internal/emit"
	"github.com/livingstaccato/cairn/internal/model"
)

// sampleListing is what cairn actually writes, so a recogniser is tested
// against the emitter's real output rather than against a hand-typed guess at
// it. A recogniser that only accepts what a test author remembered is a
// recogniser that refuses to delete real cairn output.
func sampleListing() model.Listing {
	return model.Listing{
		Path:      "/pool",
		Generated: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
		Count:     1,
		Entries: []model.Entry{{
			Name: "nginx.deb", Path: "/pool/nginx.deb", Size: 3,
			ModTime: time.Date(2026, 9, 5, 11, 0, 0, 0, time.UTC), Kind: "archive",
		}},
		Generator: model.Generator,
	}
}

func writeTemp(t *testing.T, name string, body []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// mustBytes unwraps an emitter. It takes no *testing.T because a multi-value
// call has to be the sole argument, and panicking in a test fails it anyway.
func mustBytes(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}

// An empty JSON array is not evidence of authorship, because it is what an
// empty listing of anything looks like — and search-index.json is not an
// unusual name to have chosen: static-search plugins with no connection to
// cairn use exactly this filename. An orphan reaching provenOurs is by
// definition unclaimed, and cairn's own writes are always claimed regardless
// of content, so a real empty search index cairn wrote never reaches this
// function in the first place; there was nothing for the carve-out to protect.
func TestProvenOursRejectsAnEmptySearchIndex(t *testing.T) {
	if provenOurs(writeTemp(t, emit.SearchFile, []byte("[]\n"))) {
		t.Error("an empty array was accepted as proof of authorship")
	}
}

// The formats whose bytes identify themselves. These are what --remove-orphaned
// is allowed to delete.
func TestProvenOursAcceptsCairnsOwnOutput(t *testing.T) {
	l := sampleListing()
	cases := []struct {
		name string
		body []byte
	}{
		{"index.json", mustBytes(emit.JSON(l))},
		{"tree.json", mustBytes(emit.JSON(l))},
		{"index.csv", mustBytes(emit.CSV(l))},
		{"tree.csv", mustBytes(emit.CSV(l))},
		{emit.SearchFile, mustBytes(emit.Search(l))},
		{"index.html", mustBytes(emit.BareHTML(emit.BarePage{Listing: l}))},
		{emit.HugoContentFile, mustBytes(emit.HugoContent(emit.HugoPage{Listing: l, Present: "bare"}))},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !provenOurs(writeTemp(t, c.name, c.body)) {
				t.Errorf("cairn's own %s was not recognised, so a real orphan can never be removed", c.name)
			}
		})
	}
}

// The four keys isListing checked were also a plausible shape for another
// tool's directory listing to have chosen — path, a timestamp, a count, an
// array of entries is a generic description of "a directory", not something
// unique to cairn. A generator marker closes that: the shape can be imitated,
// a specific value in a specific field is what actually identifies the writer.
func TestProvenOursRejectsAListingShapeWithNoGenerator(t *testing.T) {
	foreign := `{"path":"/pool","generated":"2026-09-05T12:00:00Z","count":0,"entries":[]}` + "\n"
	if provenOurs(writeTemp(t, "index.json", []byte(foreign))) {
		t.Error("a coincidentally cairn-shaped listing with no generator was accepted")
	}
}

// The mirror is the deployment cairn exists for, and in a mirror nearly every
// file is somebody's artifact. A foreign file wearing a generated name is the
// data-loss case: --remove-orphaned deleted mirrored package indexes and
// extracted documentation because the classification was the basename alone.
func TestProvenOursRejectsForeignFiles(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"index.html", "<html><body><a href='requests-2.0.tar.gz'>requests</a></body></html>\n"},
		{"index.json", `{"name":"requests","versions":["2.0"]}` + "\n"},
		{"index.json", `[1,2,3]` + "\n"},
		{"index.csv", "package,version\nrequests,2.0\n"},
		{"tree.csv", "a,b\n1,2\n"},
		{emit.SearchFile, `{"index":"lunr"}` + "\n"},
		{emit.HugoContentFile, "---\ntitle: Hand written\n---\n\nProse.\n"},
	}
	for _, c := range cases {
		t.Run(c.name+"/"+c.body[:min(12, len(c.body))], func(t *testing.T) {
			if provenOurs(writeTemp(t, c.name, []byte(c.body))) {
				t.Errorf("a foreign %s was accepted as cairn's, so --remove-orphaned would delete it", c.name)
			}
		})
	}
}

// Two formats cannot prove authorship and are never deleted on the strength of
// their name.
//
// index.txt is one filename per line, which is what any listing anywhere looks
// like. SHA256SUMS is coreutils format by design and tested against the real
// sha256sum -c, so cairn's is byte-for-byte the shape every other publisher's
// is — a Debian or release mirror ships one that is not cairn's and not
// distinguishable from cairn's.
func TestProvenOursRefusesTheUnprovableFormats(t *testing.T) {
	l := sampleListing()
	unprovable := map[string][]byte{
		"index.txt":   emit.Text(l),
		"tree.txt":    emit.Text(l),
		emit.SumsFile: emit.Sums(l),
	}
	for name, body := range unprovable {
		t.Run(name, func(t *testing.T) {
			if provenOurs(writeTemp(t, name, body)) {
				t.Errorf("%s cannot be told from anyone else's and must never be deleted on its name", name)
			}
		})
	}
}

// A file that cannot be read is not proven. Refusing is the safe direction: the
// alternative is deleting something on the strength of an error.
func TestProvenOursRefusesWhatItCannotRead(t *testing.T) {
	if provenOurs(filepath.Join(t.TempDir(), "index.json")) {
		t.Error("a missing file was treated as proven")
	}
}
