// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package emit

import (
	"strings"
	"testing"
)

// The parent row was written for every listing, including the top of the tree,
// where it points at something cairn did not index. Served under base_path it
// leaves the mirror entirely, and from file:// it walks out of the tree.
//
// The same rule the footer already follows: machineFormats exists so a page
// never links to a file that was not written. A link out of the tree is the
// same mistake pointing the other way.
func TestBareHTMLOmitsTheParentRowAtTheTop(t *testing.T) {
	b, err := BareHTML(BarePage{Listing: sample(), AtRoot: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `href="../"`) {
		t.Errorf("the top listing offers a link above the tree:\n%s", b)
	}
}

// Everywhere else it is the only way back up, so it has to stay.
func TestBareHTMLKeepsTheParentRowBelowTheTop(t *testing.T) {
	b, err := BareHTML(BarePage{Listing: sample()})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `href="../"`) {
		t.Errorf("a listing below the top has no way back up:\n%s", b)
	}
}
