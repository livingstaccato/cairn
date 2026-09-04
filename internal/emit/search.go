// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package emit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/livingstaccato/cairn/internal/model"
)

// SearchFile is the fixed name of the standalone search index. Fixed rather
// than derived from the index basename: one directory publishes one index, and
// a recursive listing would otherwise write the file twice under two stems.
const SearchFile = "search-index.json"

// SearchRecord is one searchable thing, in the shape a browser search library
// consumes directly.
//
// Every field is one a person would type. Name is first because on a file
// mirror people search for filenames — "nginx_1.24.0-1_amd64.deb" — far more
// often than for titles, and a title is frequently absent. Digests, MIME types
// and layout fields like weight and depth are left out: nobody searches for
// them, and each one makes the file bigger for every visitor who downloads it.
type SearchRecord struct {
	Name     string    `json:"name"`
	Title    string    `json:"title,omitempty"`
	Path     string    `json:"path"`
	Summary  string    `json:"summary,omitempty"`
	Tags     []string  `json:"tags,omitempty"`
	Kind     string    `json:"kind"`
	IsDir    bool      `json:"is_dir"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

// Search renders a listing as a standalone search index.
//
// A bare JSON array, not an object with the array inside it, because that is
// what a browser search library takes: Fuse.js, MiniSearch and FlexSearch are
// all constructed from an array of records plus the names of the fields to
// index, so this file is usable with no adapter and no unwrapping. Lunr builds
// its own index from the same array. cairn still dictates no record shape for
// a site that already has a search index — that site maps entries itself.
func Search(l model.Listing) ([]byte, error) {
	records := make([]SearchRecord, 0, len(l.Entries))
	for _, e := range l.Entries {
		records = append(records, SearchRecord{
			Name:     e.Name,
			Title:    e.Title,
			Path:     e.Path,
			Summary:  e.Summary,
			Tags:     e.Tags,
			Kind:     e.Kind,
			IsDir:    e.IsDir,
			Size:     e.Size,
			Modified: e.ModTime,
		})
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	// Filenames, like everywhere else cairn writes JSON: escaping would leave
	// "a&b<c>.txt" naming nothing on disk.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(records); err != nil {
		return nil, fmt.Errorf("encode search index %s: %w", l.Path, err)
	}
	return buf.Bytes(), nil
}
