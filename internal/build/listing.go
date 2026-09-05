// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Turning a directory's entries into the listing that gets emitted: the path it
// publishes under, the timestamp it carries, and where base_path puts both.
package build

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/livingstaccato/cairn/internal/model"
)

// listing wraps entries in the envelope the emitters write.
func (r *runner) listing(relDir string, entries []model.Entry) model.Listing {
	p := "/" + relDir
	if relDir == "." {
		p = "/"
	}
	p = strings.TrimSuffix(r.cfg.BasePath+p, "/")
	if p == "" {
		p = "/"
	}
	return model.Listing{
		Path: p, Generated: r.newest(relDir, entries), Count: len(entries),
		Entries: r.rebase(entries), Generator: model.Generator,
	}
}

// newest is the most recent modification time in a listing.
//
// Not the build clock. A wall-clock stamp makes every index.json differ from
// the last one even when nothing in the directory moved, so no build ever
// settles — and in a mirror, where cairn writes into the tree it indexes, that
// rewrite moves the file's own mtime, which is itself a change the parent
// listing records. Derived from the content, two builds of one tree produce the
// same bytes, and the field answers the more useful question anyway: as of when
// is this listing accurate.
//
// Entry times arrive already in UTC and truncated to the second, so a listing
// built in two timezones is byte-identical.
func (r *runner) newest(relDir string, entries []model.Entry) time.Time {
	var t time.Time
	for _, e := range entries {
		if e.ModTime.After(t) {
			t = e.ModTime
		}
	}
	if !t.IsZero() {
		return t
	}
	// An empty directory, or a source carrying no times of its own. The
	// directory's own stamp is still derived from the tree rather than the run.
	fi, err := os.Stat(filepath.Join(r.root, filepath.FromSlash(relDir)))
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime().UTC().Truncate(time.Second)
}

// rebase moves every entry path under base_path.
//
// Entry.Path is rooted at cairn's root, which is only the site root when the
// two happen to coincide. A tree indexed from static/_odds and served at /_odds
// emitted /mockups/x.html for a file the site serves at /_odds/mockups/x.html,
// so every link was a fresh 404 and nothing in cairn mentioned it. This is the
// one funnel all three producers pass through.
//
// An authored manifest path that is already a full URL is left alone: it names
// somewhere else entirely, which is what the manifest source is for.
func (r *runner) rebase(entries []model.Entry) []model.Entry {
	if r.cfg.BasePath == "" {
		return entries
	}
	out := make([]model.Entry, len(entries))
	copy(out, entries)
	for i := range out {
		if strings.HasPrefix(out[i].Path, "/") {
			out[i].Path = r.cfg.BasePath + out[i].Path
		}
	}
	return out
}
