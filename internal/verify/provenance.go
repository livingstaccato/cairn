// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package verify

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"

	"github.com/livingstaccato/cairn/internal/emit"
)

// headBytes is how much of a file a header or marker test reads.
//
// Enough for the whole head of a page and far more than a CSV header line, and
// small enough that testing a file nobody should have named index.html costs
// nothing. A mirror can hold a 34 MB autoindex page; the marker is in the first
// few hundred bytes of it or it is not cairn's.
const headBytes = 8 << 10

// provenOurs reports whether a file's own bytes prove cairn wrote it.
//
// checkOrphan asks a deliberately weaker question — could cairn have written a
// file with this name — and that is the right question for a report. The
// basename test is a necessary condition and never a sufficient one, and the
// difference does not matter while a human reads the answer.
//
// It matters for deletion. In a mirror root and out are the same directory, so
// nearly every file is somebody's artifact, and the names cairn generates are
// the most ordinary names in a published tree: a PyPI simple index, an
// extracted documentation tarball and a generated API reference are all called
// index.html. Deleting on the name alone destroyed them, reported success, and
// exited zero — GeneratedNames covers all four extensions whatever outputs: is
// set, so cairn need never have written HTML into that tree at all, and
// emit.Writer's conflict check never fires on a path cairn does not write.
//
// So removal asks the stronger question and answers it from content. A format
// that cannot answer it is reported and kept, which is the safe direction: the
// cost of keeping a stale file is a line in a report, and the cost of deleting
// a live one is unrecoverable.
func provenOurs(abs string) bool {
	base := filepath.Base(abs)
	switch base {
	case emit.SearchFile:
		return isSearchIndex(read(abs, 0))
	case emit.HugoContentFile:
		return isHugoContent(read(abs, headBytes))
	case emit.SumsFile:
		// Coreutils format by design, and tested against the real sha256sum -c.
		// Every publisher's is the same shape as cairn's, so nothing here can
		// tell them apart and this must never be deleted on its name.
		return false
	}

	switch filepath.Ext(base) {
	case ".json":
		return isListing(read(abs, 0))
	case ".csv":
		return isCairnCSV(read(abs, headBytes))
	case ".html":
		return bytes.Contains(read(abs, headBytes), []byte(emit.GeneratorMarker))
	}
	// index.txt is one filename per line, which is what a listing of anything
	// looks like anywhere. Nothing in it is cairn's.
	return false
}

// read returns at most limit bytes of a file, or all of it when limit is 0. A
// file it cannot read comes back empty, so every recogniser refuses it.
func read(abs string, limit int) []byte {
	if limit == 0 {
		// #nosec G304 -- abs has already been through containedPath, which
		// resolves it and refuses anything outside the output root. Nothing
		// reaches here that removal is not already about to delete.
		b, err := os.ReadFile(abs)
		if err != nil {
			return nil
		}
		return b
	}
	// #nosec G304 -- see above: containedPath has resolved and bounded abs.
	f, err := os.Open(abs)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, limit)
	n, _ := f.Read(buf)
	return buf[:n]
}

// isListing reports whether these bytes are a listing cairn emitted.
//
// The whole of model.Listing's own shape, not one field of it: a foreign
// index.json in a package mirror is an object too, and one key in common is not
// evidence. Every key is required and entries must be an array, which is what
// separates a listing from a registry's metadata document.
func isListing(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	var l struct {
		Path      *string            `json:"path"`
		Generated *string            `json:"generated"`
		Count     *int               `json:"count"`
		Entries   *[]json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(b, &l); err != nil {
		return false
	}
	return l.Path != nil && l.Generated != nil && l.Count != nil && l.Entries != nil
}

// isSearchIndex reports whether these bytes are the standalone search index.
//
// A bare array of records, because that is what a browser search library takes
// directly. An empty array proves nothing: it is what an empty listing of
// anything looks like, search-index.json is not an unusual name — static-search
// plugins with no connection to cairn use exactly it — and there is no real case
// for the empty carve-out to protect. checkOrphan only ever reaches an unclaimed
// file, and emit.Writer claims every path it handles regardless of the bytes, so
// a real empty search index cairn wrote is never unclaimed and never arrives
// here in the first place.
func isSearchIndex(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	var records []struct {
		Name  *string `json:"name"`
		Path  *string `json:"path"`
		Kind  *string `json:"kind"`
		IsDir *bool   `json:"is_dir"`
	}
	if err := json.Unmarshal(b, &records); err != nil {
		return false
	}
	if len(records) == 0 {
		return false
	}
	for _, r := range records {
		if r.Name == nil || r.Path == nil || r.Kind == nil || r.IsDir == nil {
			return false
		}
	}
	return true
}

// isCairnCSV reports whether the first row is CSVHeader exactly.
//
// CSVHeader is the documented column contract that shell consumers index into,
// so a file carrying it either came from cairn or is deliberately impersonating
// cairn's output.
func isCairnCSV(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	// FieldsPerRecord off: only the header is read, and a truncated head can
	// leave the row after it short.
	r := csv.NewReader(bytes.NewReader(b))
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err != nil {
		return false
	}
	return slices.Equal(header, emit.CSVHeader)
}

// isHugoContent reports whether these bytes are the branch bundle cairn writes.
//
// Both keys, because a hand-written _index.md in a Hugo site is an ordinary
// thing to find and may well carry a layout of its own. The cairn: block is
// what no page cairn did not write would have.
func isHugoContent(b []byte) bool {
	if !bytes.HasPrefix(b, []byte("---\n")) {
		return false
	}
	head, _, ok := bytes.Cut(b[4:], []byte("\n---\n"))
	if !ok {
		// A frontmatter fence that does not close inside the bytes read is
		// still frontmatter; test what there is of it.
		head = b[4:]
	}
	return bytes.Contains(head, []byte("layout: "+emit.HugoLayout)) &&
		bytes.Contains(head, []byte("\ncairn:"))
}
