// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package emit

import (
	"bytes"
	"sort"

	"github.com/livingstaccato/cairn/internal/model"
)

// SumsFile is the conventional filename for a directory's checksums.
const SumsFile = "SHA256SUMS"

// sumsSeparator is the two spaces coreutils writes between digest and filename.
// One space means "text mode" to some implementations and a different byte
// count to others; two is what `sha256sum` emits and what `-c` expects.
const sumsSeparator = "  "

// Sums renders SHA256SUMS in coreutils format: the hex digest, exactly two
// spaces, then the filename.
//
// That exact shape is the point. It lets a client run `sha256sum -c SHA256SUMS`
// with no bespoke verifier, which is the whole integrity story on a plain-HTTP
// mirror where the transport guarantees nothing.
//
// Directories and unhashed entries are omitted rather than written with an
// empty digest, which `-c` would reject as a malformed line.
func Sums(l model.Listing) []byte {
	type row struct{ name, sum string }
	var rows []row
	for _, e := range l.Entries {
		if e.IsDir || e.SHA256 == "" {
			continue
		}
		rows = append(rows, row{name: e.Name, sum: e.SHA256})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })

	var buf bytes.Buffer
	for _, r := range rows {
		buf.WriteString(r.sum)
		buf.WriteString(sumsSeparator)
		buf.WriteString(r.name)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}
