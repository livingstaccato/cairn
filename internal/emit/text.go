// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

package emit

import (
	"bytes"
	"strings"

	"github.com/livingstaccato/cairn/internal/model"
)

// Text renders a listing as one entry name per line, directories with a
// trailing slash.
//
// Deliberately not a second CSV. CSV has a header and eight columns, so
// consuming it means parsing it; this is the format a shell already speaks:
//
//	curl -s http://mirror/bootstrap/index.txt | grep -v / | xargs -n1 -I{} \
//	  curl -sO "http://mirror/bootstrap/{}"
//
// Names are emitted raw. A filename containing a newline would corrupt the
// format, and cannot be escaped without inventing a syntax that defeats the
// point — such a name is rejected rather than mangled.
func Text(l model.Listing) []byte {
	var buf bytes.Buffer
	for _, e := range l.Entries {
		if strings.ContainsAny(e.Name, "\n\r") {
			continue
		}
		buf.WriteString(e.Name)
		if e.IsDir {
			buf.WriteByte('/')
		}
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}
