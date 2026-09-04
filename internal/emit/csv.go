// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package emit

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strconv"
	"time"

	"github.com/livingstaccato/cairn/internal/model"
)

// CSVHeader is the stable documented column order. Shell consumers index into
// these positions, so columns may be appended but never reordered or removed.
var CSVHeader = []string{"name", "path", "type", "size", "modified", "sha256", "kind", "title"}

// Coarse type column values. `type` says file-or-directory for a shell
// consumer; `kind` carries the finer classification for a UI.
const (
	typeDir  = "dir"
	typeFile = "file"
)

// csvSafe neutralizes spreadsheet formula injection. Excel and LibreOffice
// execute a field beginning with =, +, -, @, tab or CR as a formula when the
// file is opened. Names in a mirrored tree are attacker-influenced, so such
// fields get an apostrophe prefix, which those programs read as "this is text".
//
// encoding/csv handles quoting; it does not handle this, because it is a
// spreadsheet behavior rather than a CSV one.
func csvSafe(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

// CSV renders a listing as index.csv or tree.csv.
func CSV(l model.Listing) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(CSVHeader); err != nil {
		return nil, fmt.Errorf("write csv header: %w", err)
	}
	for _, e := range l.Entries {
		if err := w.Write(csvRow(e)); err != nil {
			return nil, fmt.Errorf("write csv row %s: %w", e.Name, err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("flush csv: %w", err)
	}
	return buf.Bytes(), nil
}

// csvRow renders one entry in CSVHeader's column order.
func csvRow(e model.Entry) []string {
	typ := typeFile
	if e.IsDir {
		typ = typeDir
	}
	return []string{
		csvSafe(e.Name),
		csvSafe(e.Path),
		typ,
		strconv.FormatInt(e.Size, 10),
		e.ModTime.UTC().Format(time.RFC3339),
		e.SHA256,
		e.Kind,
		csvSafe(e.Title),
	}
}
