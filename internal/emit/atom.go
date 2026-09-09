// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package emit

import (
	"encoding/xml"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/livingstaccato/cairndex/internal/model"
)

// atomMaxEntries caps a feed to its most recently modified entries.
// index.json, index.csv and index.txt carry a listing's full contents by
// design; a feed's purpose is different — "what changed recently" — and an
// uncapped feed of a fifty-thousand-entry pool would be a multi-megabyte file
// no feed reader is built to hold, almost all of it years-old entries that
// never change. 100 matches the default most feed generators and readers
// already assume.
const atomMaxEntries = 100

type atomFeed struct {
	XMLName   xml.Name       `xml:"http://www.w3.org/2005/Atom feed"`
	ID        string         `xml:"id"`
	Title     string         `xml:"title"`
	Updated   string         `xml:"updated"`
	Links     []atomLink     `xml:"link"`
	Generator string         `xml:"generator"`
	Entries   []atomXMLEntry `xml:"entry"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr,omitempty"`
}

type atomXMLEntry struct {
	ID      string   `xml:"id"`
	Title   string   `xml:"title"`
	Updated string   `xml:"updated"`
	Link    atomLink `xml:"link"`
	Summary string   `xml:"summary,omitempty"`
}

// xmlSafe strips characters XML 1.0 cannot represent, escaped or not: the C0
// control characters other than tab, LF and CR, and the UTF-16 surrogate and
// noncharacter ranges. Unlike an HTML entity or a percent-encoded URL byte,
// there is no valid escape for these — encoding/xml would otherwise emit the
// raw byte and produce a file no XML parser can read back, and names in a
// mirrored tree are attacker-influenced.
func xmlSafe(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return r
		case r < 0x20:
			return -1
		case r >= 0xD800 && r <= 0xDFFF:
			return -1
		case r == 0xFFFE || r == 0xFFFF:
			return -1
		default:
			return r
		}
	}, s)
}

// atomTime formats t per RFC 3339, the timestamp form RFC 4287 requires.
func atomTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// Atom renders a listing as an Atom 1.0 feed (RFC 4287) of its most recently
// modified entries — files and directories alike, since a new subdirectory
// (a package's new version, say) is exactly the kind of change worth
// subscribing to.
//
// Every link is site-root-relative, the same as every other emitter: cairndex
// has no base_url setting to build an absolute one from, and Atom permits a
// relative IRI, resolved against the feed's own fetch URL (RFC 3986 §5.1.1).
// <id> is likewise not a dereferenceable address — RFC 4287 §4.2.6 requires
// only that it be stable and "unique within the scope of the document", for
// a reader's own deduplication, which a path-derived urn: value already is.
func Atom(l model.Listing) ([]byte, error) {
	entries := make([]model.Entry, len(l.Entries))
	copy(entries, l.Entries)
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].ModTime.After(entries[j].ModTime)
	})
	if len(entries) > atomMaxEntries {
		entries = entries[:atomMaxEntries]
	}

	feed := atomFeed{
		ID:        "urn:cairndex:path:" + l.Path,
		Title:     xmlSafe(l.Path),
		Updated:   atomTime(l.Generated),
		Generator: model.Generator,
		Links: []atomLink{
			{Href: "atom.xml", Rel: "self"},
			{Href: "./", Rel: "alternate"},
		},
	}
	for _, e := range entries {
		title := e.Title
		if title == "" {
			title = e.Name
		}
		feed.Entries = append(feed.Entries, atomXMLEntry{
			ID:      "urn:cairndex:path:" + e.Path,
			Title:   xmlSafe(title),
			Updated: atomTime(e.ModTime),
			Link:    atomLink{Href: e.Path},
			Summary: xmlSafe(e.Summary),
		})
	}

	b, err := xml.MarshalIndent(feed, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode atom feed %s: %w", l.Path, err)
	}
	out := append([]byte(xml.Header), b...)
	out = append(out, '\n')
	return out, nil
}
