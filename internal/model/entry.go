// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Package model defines the normalized record that every cairn producer fills
// and every emitter reads. Its JSON field names are a public contract:
// index.json consumers and Hugo templates both depend on them.
package model

import "time"

// Entry is one item in a directory listing — a file, a directory, or a page.
type Entry struct {
	Name    string    `json:"name"    yaml:"name"`
	Path    string    `json:"path"    yaml:"path"` // site-root-relative, URL-safe
	IsDir   bool      `json:"is_dir"  yaml:"is_dir"`
	Size    int64     `json:"size"    yaml:"size"` // exact bytes; formatting is presentation
	ModTime time.Time `json:"modified" yaml:"modified"`
	Kind    string    `json:"kind"    yaml:"kind"` // dir|script|archive|image|page|config|data|doc|other
	MIME    string    `json:"mime"    yaml:"mime"`
	SHA256  string    `json:"sha256,omitempty"  yaml:"sha256,omitempty"`
	Title   string    `json:"title,omitempty"   yaml:"title,omitempty"`
	Summary string    `json:"summary,omitempty" yaml:"summary,omitempty"`
	Tags    []string  `json:"tags,omitempty"    yaml:"tags,omitempty"`
	Count   int       `json:"count,omitempty"   yaml:"count,omitempty"` // children, for directories
	// Weight orders a listing ahead of its sort key, the way Hugo's does. Zero
	// means unweighted and sorts last, so authoring one entry does not disturb
	// the rest.
	Weight int            `json:"weight,omitempty"  yaml:"weight,omitempty"`
	Depth  int            `json:"depth"   yaml:"depth"` // 0 = the listed directory itself
	Extra  map[string]any `json:"extra,omitempty"   yaml:"extra,omitempty"`
}

// Listing is the envelope written to index.json and tree.json.
type Listing struct {
	Path string `json:"path"      yaml:"path"`
	// Generated is the most recent modification time among Entries — when this
	// listing became accurate, not when the build ran. Derived from the content
	// so that two builds of an unchanged tree produce identical bytes.
	Generated time.Time `json:"generated" yaml:"generated"`
	Count     int       `json:"count"     yaml:"count"`
	Entries   []Entry   `json:"entries"   yaml:"entries"`
}
