// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package emit

import (
	"encoding/xml"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/livingstaccato/cairndex/internal/model"
)

func TestAtomWellFormedAndOrderedNewestFirst(t *testing.T) {
	b, err := Atom(sample())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), xml.Header) {
		t.Errorf("output does not start with the XML declaration: %q", string(b)[:40])
	}

	var feed atomFeed
	if err := xml.Unmarshal(b, &feed); err != nil {
		t.Fatalf("emitted feed does not parse: %v", err)
	}
	if feed.Title != "/bootstrap/linux/" {
		t.Errorf("Title = %q", feed.Title)
	}
	if feed.Updated != "2026-09-03T12:00:00Z" {
		t.Errorf("Updated = %q, want RFC3339 UTC", feed.Updated)
	}
	if len(feed.Entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(feed.Entries))
	}
	// apt.list (2026-09-02) modified more recently than deep/ (2026-09-01), so
	// it must lead the feed even though the listing itself orders directories
	// first.
	if feed.Entries[0].Title != "APT sources" {
		t.Errorf("Entries[0].Title = %q, want the newer entry first", feed.Entries[0].Title)
	}
	if feed.Entries[1].Title != "deep" {
		t.Errorf("Entries[1].Title = %q, want the older entry second", feed.Entries[1].Title)
	}
	if feed.Entries[0].Link.Href != "/bootstrap/linux/apt.list" {
		t.Errorf("Link.Href = %q", feed.Entries[0].Link.Href)
	}
}

func TestAtomEntryIDsAreStableAndUnique(t *testing.T) {
	b, err := Atom(sample())
	if err != nil {
		t.Fatal(err)
	}
	var feed atomFeed
	if err := xml.Unmarshal(b, &feed); err != nil {
		t.Fatal(err)
	}
	if feed.ID == "" {
		t.Error("feed ID is empty")
	}
	seen := map[string]bool{}
	for _, e := range feed.Entries {
		if e.ID == "" {
			t.Errorf("entry %q has an empty ID", e.Title)
		}
		if seen[e.ID] {
			t.Errorf("duplicate entry ID %q", e.ID)
		}
		seen[e.ID] = true
	}
}

func TestAtomStripsInvalidXMLCharsButEscapesTheRest(t *testing.T) {
	l := sample()
	l.Entries[1].Name = "evil\x00\x01<script>&\"'"
	l.Entries[1].Title = ""
	b, err := Atom(l)
	if err != nil {
		t.Fatal(err)
	}
	var feed atomFeed
	if err := xml.Unmarshal(b, &feed); err != nil {
		t.Fatalf("a hostile filename produced unparsable XML: %v\n%s", err, b)
	}
	got := feed.Entries[0].Title
	if strings.ContainsAny(got, "\x00\x01") {
		t.Errorf("Title %q: control characters must be stripped, not escaped", got)
	}
	if got != "evil<script>&\"'" {
		t.Errorf("Title = %q, want the control bytes gone and the rest intact", got)
	}
}

func TestAtomCapsToMostRecentEntries(t *testing.T) {
	l := model.Listing{Path: "/pool/", Generated: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	for i := 0; i < atomMaxEntries+10; i++ {
		l.Entries = append(l.Entries, model.Entry{
			Name:    "x" + strconv.Itoa(i),
			Path:    "/pool/" + strconv.Itoa(i),
			ModTime: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Hour),
		})
	}
	b, err := Atom(l)
	if err != nil {
		t.Fatal(err)
	}
	var feed atomFeed
	if err := xml.Unmarshal(b, &feed); err != nil {
		t.Fatal(err)
	}
	if len(feed.Entries) != atomMaxEntries {
		t.Fatalf("got %d entries, want the cap of %d", len(feed.Entries), atomMaxEntries)
	}
	// The highest index has the latest ModTime, so it must survive the cap.
	want := "x" + strconv.Itoa(atomMaxEntries+9)
	if feed.Entries[0].Title != want {
		t.Errorf("Entries[0].Title = %q, want %q (the most recently modified)", feed.Entries[0].Title, want)
	}
}

func TestAtomEmptyListingIsStillValid(t *testing.T) {
	l := model.Listing{Path: "/empty/", Generated: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	b, err := Atom(l)
	if err != nil {
		t.Fatal(err)
	}
	var feed atomFeed
	if err := xml.Unmarshal(b, &feed); err != nil {
		t.Fatalf("empty listing produced unparsable XML: %v", err)
	}
	if len(feed.Entries) != 0 {
		t.Errorf("got %d entries, want 0", len(feed.Entries))
	}
	if feed.Title == "" || feed.Updated == "" {
		t.Error("feed-level metadata must still be present with no entries")
	}
}
