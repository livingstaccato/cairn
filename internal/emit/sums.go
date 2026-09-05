// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package emit

import (
	"bytes"
	"sort"
	"strings"

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
//
// Names that cannot be written literally are escaped the way coreutils escapes
// them. Written raw, a newline in a filename splits one entry into two: a
// digest naming a file that does not exist, and a malformed remainder. The real
// file is then never verified and nothing reports it — a silent hole in the one
// output whose whole purpose is integrity. Filenames in a mirror are
// attacker-influenced, which is why this is a guard and not a nicety.
// nameableInSums reports whether a name can be written as a checksum line at
// all.
//
// A checksum line names a file relative to the directory it sits in, and three
// names cannot do that: the empty one, "." and "..", which name the directory
// or its parent. The filesystem walk never produces them, but an authored
// manifest source can, and writing one produces a line cairn's own verifier has
// to reject — a mirror reporting its own output as damaged.
func nameableInSums(name string) bool {
	return name != "" && name != "." && name != ".."
}

// sumsName renders a filename for a checksum line, reporting whether anything
// had to be escaped.
//
// The rules are coreutils', verified against GNU coreutils 9.11 rather than
// read off a manual: a backslash becomes two, a newline becomes \n, a carriage
// return becomes \r, and a line holding any of them is prefixed with a
// backslash so `-c` knows to undo it. A space needs nothing — everything after
// the separator is the name by definition.
func sumsName(name string) (string, bool) {
	if !strings.ContainsAny(name, "\\\n\r") {
		return name, false
	}
	var b strings.Builder
	b.Grow(len(name) + 8)
	for i := range len(name) {
		switch name[i] {
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteByte(name[i])
		}
	}
	return b.String(), true
}

func Sums(l model.Listing) []byte {
	type row struct{ name, sum string }
	var rows []row
	for _, e := range l.Entries {
		if e.IsDir || e.SHA256 == "" || !nameableInSums(e.Name) {
			continue
		}
		rows = append(rows, row{name: e.Name, sum: e.SHA256})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })

	var buf bytes.Buffer
	for _, r := range rows {
		name, escaped := sumsName(r.name)
		if escaped {
			buf.WriteByte('\\')
		}
		buf.WriteString(r.sum)
		buf.WriteString(sumsSeparator)
		buf.WriteString(name)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}
