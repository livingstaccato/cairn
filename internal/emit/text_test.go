// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package emit

import (
	"strings"
	"testing"

	"github.com/livingstaccato/cairn/internal/model"
)

func TestText(t *testing.T) {
	got := string(Text(sample()))
	want := "deep/\napt.list\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTextIsShellSafe(t *testing.T) {
	// Every line is one name, so a shell needs no parser.
	out := string(Text(sample()))
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if strings.ContainsAny(line, ",\"") {
			t.Errorf("line %q carries CSV punctuation; this is not a second CSV", line)
		}
	}
}

// A newline in a filename would corrupt a line-per-name format, and escaping it
// would mean inventing a syntax that defeats the format's purpose.
func TestTextSkipsNamesWithNewlines(t *testing.T) {
	l := model.Listing{Entries: []model.Entry{
		{Name: "ok.txt"},
		{Name: "bad\nname.txt"},
		{Name: "also\rbad"},
	}}
	got := string(Text(l))
	if got != "ok.txt\n" {
		t.Errorf("got %q, want only the safe name", got)
	}
}
