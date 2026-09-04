// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package emit

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/livingstaccato/cairn/internal/model"
)

// JSON renders a listing as index.json or tree.json.
//
// Indented, because these files are often committed alongside the tree they
// describe and a one-line change should read as a one-line diff. HTML escaping
// is off: these are filenames, and Go's default would turn "a&b<c>.txt" into
// escape sequences that no longer name the file on disk.
func JSON(l model.Listing) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(l); err != nil {
		return nil, fmt.Errorf("encode listing %s: %w", l.Path, err)
	}
	return buf.Bytes(), nil
}
