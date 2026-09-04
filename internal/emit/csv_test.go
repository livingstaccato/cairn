// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package emit

import (
	"encoding/csv"
	"strings"
	"testing"
)

func TestCSVHeaderAndRows(t *testing.T) {
	b, err := CSV(sample())
	if err != nil {
		t.Fatal(err)
	}
	recs, err := csv.NewReader(strings.NewReader(string(b))).ReadAll()
	if err != nil {
		t.Fatalf("emitted CSV does not parse: %v", err)
	}
	if len(recs) != 3 {
		t.Fatalf("got %d records, want header + 2 rows", len(recs))
	}
	for i, h := range CSVHeader {
		if recs[0][i] != h {
			t.Errorf("header[%d] = %q, want %q", i, recs[0][i], h)
		}
	}
	if recs[1][2] != "dir" {
		t.Errorf("type = %q, want dir", recs[1][2])
	}
	if recs[2][2] != "file" {
		t.Errorf("type = %q, want file", recs[2][2])
	}
	if recs[2][3] != "64" {
		t.Errorf("size = %q, want the exact byte count 64", recs[2][3])
	}
	if recs[2][4] != "2026-09-02T09:00:00Z" {
		t.Errorf("modified = %q, want RFC3339 UTC", recs[2][4])
	}
	if recs[2][7] != "APT sources" {
		t.Errorf("title = %q", recs[2][7])
	}
}

func TestCSVNeutralizesFormulaInjection(t *testing.T) {
	l := sample()
	l.Entries[1].Name = `=cmd|'/c calc'!A1`
	l.Entries[1].Title = `@SUM(1+1)`
	b, err := CSV(l)
	if err != nil {
		t.Fatal(err)
	}
	recs, err := csv.NewReader(strings.NewReader(string(b))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(recs[2][0], "'=") {
		t.Errorf("name = %q, want an apostrophe prefix so a spreadsheet treats it as text", recs[2][0])
	}
	if !strings.HasPrefix(recs[2][7], "'@") {
		t.Errorf("title = %q, want an apostrophe prefix", recs[2][7])
	}
}

func TestCSVSafeCoversEveryTrigger(t *testing.T) {
	for _, s := range []string{"=x", "+x", "-x", "@x", "\tx", "\rx"} {
		if got := csvSafe(s); !strings.HasPrefix(got, "'") {
			t.Errorf("csvSafe(%q) = %q, want an apostrophe prefix", s, got)
		}
	}
	for _, s := range []string{"", "plain.txt", "1.txt", "a=b"} {
		if got := csvSafe(s); got != s {
			t.Errorf("csvSafe(%q) = %q, want it unchanged", s, got)
		}
	}
}

func TestCSVQuotesInjectedCommas(t *testing.T) {
	l := sample()
	l.Entries[1].Title = `Comma, "quote" and more`
	b, err := CSV(l)
	if err != nil {
		t.Fatal(err)
	}
	recs, err := csv.NewReader(strings.NewReader(string(b))).ReadAll()
	if err != nil {
		t.Fatalf("a title containing a comma broke the CSV: %v", err)
	}
	if recs[2][7] != `Comma, "quote" and more` {
		t.Errorf("title round trip = %q", recs[2][7])
	}
}
