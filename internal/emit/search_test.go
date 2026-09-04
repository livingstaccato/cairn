// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package emit

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/livingstaccato/cairn/internal/model"
)

func searchListing() model.Listing {
	when := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	return model.Listing{
		Path: "/bootstrap/",
		Entries: []model.Entry{
			{Name: "nginx_1.24.0-1_amd64.deb", Path: "/bootstrap/nginx_1.24.0-1_amd64.deb",
				Kind: "archive", MIME: "application/vnd.debian.binary-package",
				Size: 2048, ModTime: when, SHA256: strings.Repeat("a", 64),
				Title: "nginx", Summary: "web server", Tags: []string{"debian", "http"},
				Weight: 5, Depth: 1},
			{Name: "linux", Path: "/bootstrap/linux/", IsDir: true, Kind: "dir", Count: 3, ModTime: when},
		},
	}
}

// A browser search library is constructed from an array of records. An object
// with the array nested inside would have to be unwrapped by every consumer.
func TestSearchIsABareArray(t *testing.T) {
	b, err := Search(searchListing())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(b))[:1]; got != "[" {
		t.Errorf("search index starts with %q, want a JSON array", got)
	}
	var records []map[string]any
	if err := json.Unmarshal(b, &records); err != nil {
		t.Fatalf("search index is not an array of records: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
}

// The fields are asserted as literal strings rather than against the struct
// tags: this file is a contract with whatever indexes it, and comparing a tag
// against itself would pass even after the name on the wire changed.
func TestSearchRecordFields(t *testing.T) {
	b, err := Search(searchListing())
	if err != nil {
		t.Fatal(err)
	}
	var records []map[string]any
	if err := json.Unmarshal(b, &records); err != nil {
		t.Fatal(err)
	}
	got := records[0]

	for field, want := range map[string]any{
		"name":    "nginx_1.24.0-1_amd64.deb",
		"title":   "nginx",
		"path":    "/bootstrap/nginx_1.24.0-1_amd64.deb",
		"summary": "web server",
		"kind":    "archive",
		"is_dir":  false,
	} {
		if got[field] != want {
			t.Errorf("%s = %v, want %v", field, got[field], want)
		}
	}
	if got["size"] != float64(2048) {
		t.Errorf("size = %v, want 2048", got["size"])
	}
	if tags, _ := got["tags"].([]any); len(tags) != 2 || tags[0] != "debian" {
		t.Errorf("tags = %v, want [debian http]", got["tags"])
	}
}

// Nobody searches for a digest or a MIME type, and every byte here is
// downloaded by every visitor who searches.
func TestSearchOmitsWhatNobodySearchesFor(t *testing.T) {
	b, err := Search(searchListing())
	if err != nil {
		t.Fatal(err)
	}
	var records []map[string]any
	if err := json.Unmarshal(b, &records); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"sha256", "mime", "weight", "depth", "count", "extra"} {
		if _, present := records[0][field]; present {
			t.Errorf("%s is in the search index; it is not searchable", field)
		}
	}
}

// A filename is not HTML. Go's default encoder would turn "a&b<c>.txt" into
// escape sequences that no longer name the file.
func TestSearchDoesNotEscapeFilenames(t *testing.T) {
	l := model.Listing{
		Path:    "/odd/",
		Entries: []model.Entry{{Name: "a&b<c>.txt", Path: "/odd/a&b<c>.txt", Kind: "doc"}},
	}
	b, err := Search(l)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "a&b<c>.txt") {
		t.Errorf("filename was escaped: %s", b)
	}
}

// An empty directory still publishes an index, so a consumer fetching it gets
// an empty array rather than null, which no search library accepts.
func TestSearchEmptyListingIsAnEmptyArray(t *testing.T) {
	b, err := Search(model.Listing{Path: "/empty/"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(b)); got != "[]" {
		t.Errorf("empty listing rendered %q, want []", got)
	}
}
