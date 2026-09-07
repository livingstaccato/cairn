// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package emit

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/livingstaccato/cairndex/internal/model"
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
// file is opened — and both trim leading spaces and tabs before making that
// decision, so "  =cmd()" is a formula to the spreadsheet even though its
// first byte is a space. Names in a mirrored tree are attacker-influenced,
// so such fields get an apostrophe prefix, which those programs read as
// "this is text".
//
// encoding/csv handles quoting; it does not handle this, because it is a
// spreadsheet behavior rather than a CSV one.
func csvSafe(s string) string {
	if s == "" {
		return s
	}
	// The literal first byte, so a leading tab or CR is still caught in its
	// own right — trimming it away below to look past it would stop
	// treating it as a trigger at all.
	if isFormulaTrigger(s[0]) {
		return "'" + s
	}
	// The first byte after leading spaces and tabs, so "  =cmd()" is caught
	// even though its own first byte is a space.
	if trimmed := strings.TrimLeft(s, " \t"); trimmed != "" && isFormulaTrigger(trimmed[0]) {
		return "'" + s
	}
	return s
}

// isFormulaTrigger reports whether b is one of the bytes Excel and
// LibreOffice treat as opening a formula.
func isFormulaTrigger(b byte) bool {
	switch b {
	case '=', '+', '-', '@', '\t', '\r':
		return true
	}
	return false
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
