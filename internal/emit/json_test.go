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

func sample() model.Listing {
	return model.Listing{
		Path:      "/bootstrap/linux/",
		Generated: time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC),
		Count:     2,
		Entries: []model.Entry{
			{Name: "deep", Path: "/bootstrap/linux/deep/", IsDir: true, Kind: "dir",
				MIME: "inode/directory", Count: 3, Depth: 1,
				ModTime: time.Date(2026, 9, 1, 8, 30, 0, 0, time.UTC)},
			{Name: "apt.list", Path: "/bootstrap/linux/apt.list", Size: 64, Kind: "config",
				MIME: "text/plain", Depth: 1, SHA256: strings.Repeat("a", 64),
				Title: "APT sources", ModTime: time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC)},
		},
	}
}

func TestJSONRoundTrips(t *testing.T) {
	b, err := JSON(sample())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(b), "\n") {
		t.Error("output must end with a newline")
	}
	var back model.Listing
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("emitted JSON does not parse: %v", err)
	}
	if back.Count != 2 || len(back.Entries) != 2 {
		t.Errorf("round trip lost entries: %+v", back)
	}
	if back.Entries[1].Size != 64 {
		t.Errorf("Size = %d, want exactly 64", back.Entries[1].Size)
	}
}

func TestJSONIsIndented(t *testing.T) {
	b, _ := JSON(sample())
	if !strings.Contains(string(b), "\n  ") {
		t.Error("expected indented output for human-diffable committed indexes")
	}
}

// A filename containing < or & must survive verbatim: these are file paths, not
// HTML, and Go's default HTML escaping would corrupt them.
func TestJSONDoesNotHTMLEscape(t *testing.T) {
	l := sample()
	l.Entries[1].Name = "a&b<c>.txt"
	b, err := JSON(l)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "a&b<c>.txt") {
		t.Errorf("name was escaped:\n%s", b)
	}
}
