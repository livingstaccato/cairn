// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEntryJSONKeys(t *testing.T) {
	e := Entry{
		Name:    "bootstrap.sh",
		Path:    "/bootstrap/bootstrap.sh",
		Size:    512,
		ModTime: time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC),
		Kind:    "script",
		MIME:    "text/x-shellscript",
	}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Present unconditionally — templates and jq consumers rely on these.
	for _, k := range []string{"name", "path", "is_dir", "size", "modified", "kind", "mime", "depth"} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing required key %q in %s", k, b)
		}
	}
	// Absent when empty — keeps index.json small on large mirrors.
	for _, k := range []string{"sha256", "title", "summary", "tags", "count", "extra"} {
		if _, ok := got[k]; ok {
			t.Errorf("key %q should be omitted when empty", k)
		}
	}
	if got["size"] != float64(512) {
		t.Errorf("size = %v, want 512 (exact bytes, never formatted)", got["size"])
	}
}

func TestListingJSONShape(t *testing.T) {
	l := Listing{
		Path:      "/bootstrap/linux/",
		Generated: time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC),
		Entries:   []Entry{{Name: "a"}, {Name: "b"}},
	}
	l.Count = len(l.Entries)
	b, err := json.Marshal(l)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"path":"/bootstrap/linux/","generated":"2026-09-03T12:00:00Z","count":2,` +
		`"entries":[` +
		`{"name":"a","path":"","is_dir":false,"size":0,"modified":"0001-01-01T00:00:00Z","kind":"","mime":"","depth":0},` +
		`{"name":"b","path":"","is_dir":false,"size":0,"modified":"0001-01-01T00:00:00Z","kind":"","mime":"","depth":0}` +
		`]}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
}
