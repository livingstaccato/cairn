// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for the ownership record: what cairn claims, and what it says those
// files should hold.

package emit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/livingstaccato/cairn/internal/config"
)

// The manifest records what stands at each path, not what a particular run
// moved. A run that skipped a write because the bytes already matched still has
// to record the digest, or the next check calls every unchanged file unknown.
func TestManifestRecordsDigestsForSkippedWrites(t *testing.T) {
	out := t.TempDir()
	c := cfg(t, config.ConflictError)
	body := []byte(`{"entries":[]}`)

	w := NewWriter(c, out)
	if err := w.Write("index.json", body); err != nil {
		t.Fatal(err)
	}
	if err := w.Save(); err != nil {
		t.Fatal(err)
	}
	first := manifestOf(t, out)

	w2 := NewWriter(c, out)
	if err := w2.Write("index.json", body); err != nil { // identical: skipped
		t.Fatal(err)
	}
	if w2.Unchanged() != 1 {
		t.Fatalf("setup: the write was not skipped")
	}
	if err := w2.Save(); err != nil {
		t.Fatal(err)
	}
	if got := manifestOf(t, out); got["index.json"] != first["index.json"] {
		t.Errorf("digest changed across a skipped write: %q then %q",
			first["index.json"], got["index.json"])
	}
	if !isHex64(first["index.json"]) {
		t.Errorf("digest %q is not a SHA-256", first["index.json"])
	}
}

// A scoped rebuild carries the rest of the tree forward. The digests have to
// travel with the paths, or every file outside the scope loses the only record
// of what it should hold.
func TestScopedSaveCarriesDigestsForward(t *testing.T) {
	out := t.TempDir()
	c := cfg(t, config.ConflictError)

	w := NewWriter(c, out)
	for _, p := range []string{"docs/index.json", "bootstrap/index.json"} {
		if err := w.Write(p, []byte("body of "+p)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Save(); err != nil {
		t.Fatal(err)
	}
	before := manifestOf(t, out)

	w2 := NewWriter(c, out)
	if err := w2.Write("bootstrap/index.json", []byte("body of bootstrap/index.json")); err != nil {
		t.Fatal(err)
	}
	if err := w2.SaveScoped("bootstrap"); err != nil {
		t.Fatal(err)
	}
	after := manifestOf(t, out)
	if after["docs/index.json"] != before["docs/index.json"] {
		t.Errorf("the unscoped path lost its digest: %q -> %q",
			before["docs/index.json"], after["docs/index.json"])
	}
}

func TestParseManifestRejectsWhatIsNotOne(t *testing.T) {
	cases := map[string]string{
		"an object that is not a manifest": `{"paths":["a"]}`,
		"the shape cairn never wrote":      `["a","b"]`,
		"a digest that is not one":         `{"version":1,"outputs":{"a":"nope"}}`,
		"a truncated digest":               `{"version":1,"outputs":{"a":"abc123"}}`,
		"not json at all":                  `{`,
	}
	for why, body := range cases {
		if _, err := ParseManifest([]byte(body)); err == nil {
			t.Errorf("%s parsed as a manifest: %s", why, body)
		}
	}
	if got, err := ParseManifest([]byte(`{"version":1,"outputs":{}}`)); err != nil || len(got) != 0 {
		t.Errorf("a completed run that wrote nothing must parse: %v, %v", got, err)
	}
}

func manifestOf(t *testing.T, out string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseManifest(b)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
