// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

// Package config resolves per-directory cairn behavior from root defaults,
// path-glob rules, and per-directory override files.
package config

// Enum values for the Settings fields. Exported because every other package
// switches on them — walk on Source and Hidden, emit on Outputs, the templates
// on Present — and a typo in a bare string literal is a silent misconfiguration
// rather than a compile error.
const (
	SourceFS       = "fs"
	SourcePages    = "pages"
	SourceManifest = "manifest"

	PresentStyled = "styled"
	PresentBare   = "bare"

	SortName    = "name"
	SortSize    = "size"
	SortModTime = "modtime"
	SortKind    = "kind"

	OrderAsc  = "asc"
	OrderDesc = "desc"

	HiddenSkip = "skip"
	HiddenShow = "show"

	ChecksumNone   = "none"
	ChecksumSHA256 = "sha256"

	OutputHTML   = "html"
	OutputJSON   = "json"
	OutputCSV    = "csv"
	OutputText   = "txt"
	OutputSums   = "sums"
	OutputPEP503 = "pep503"
)

// Settings is fully-resolved behavior for one directory. No pointers: every
// field has an answer by the time a directory is processed.
type Settings struct {
	Source         string   `yaml:"source"`
	Present        string   `yaml:"present"`
	Outputs        []string `yaml:"outputs"`
	Sort           string   `yaml:"sort"`
	Order          string   `yaml:"order"`
	DirsFirst      bool     `yaml:"dirs_first"`
	Hidden         string   `yaml:"hidden"`
	Checksum       string   `yaml:"checksum"`
	Recursive      bool     `yaml:"recursive"`
	FollowSymlinks bool     `yaml:"follow_symlinks"`
	// MaxRendered bounds how many entries reach the HTML page. 0 renders every
	// one. The machine formats are never capped: the page is what has a reader
	// waiting on it, and index.json is what has a script waiting on it.
	MaxRendered int `yaml:"max_rendered"`
}

// Defaults returns the built-in settings, used when a root config omits them.
// The Outputs slice is constructed per call, so a caller mutating the result
// cannot change what the next caller sees.
func Defaults() Settings {
	return Settings{
		Source:    SourceFS,
		Present:   PresentStyled,
		Outputs:   []string{OutputHTML, OutputJSON, OutputCSV},
		Sort:      SortName,
		Order:     OrderAsc,
		DirsFirst: true,
		Hidden:    HiddenSkip,
		Checksum:  ChecksumNone,
		// A flat package pool renders at roughly 180 bytes a row bare and 660
		// styled, so ten thousand entries is a 1.8 MB page bare and 6.6 MB
		// styled — measured, not estimated. Nobody reads ten thousand rows; they
		// filter, or they fetch index.json. A thousand keeps the page under a
		// megabyte in the worst case and leaves every byte of the directory
		// reachable through the machine formats.
		MaxRendered: 1000,
	}
}

// Override is a partial statement about Settings. Every field is a pointer so
// an explicitly-configured false is distinguishable from an absent key — the
// difference between "dirs_first: false" and saying nothing about it.
type Override struct {
	Source         *string   `yaml:"source"`
	Present        *string   `yaml:"present"`
	Outputs        *[]string `yaml:"outputs"`
	Sort           *string   `yaml:"sort"`
	Order          *string   `yaml:"order"`
	DirsFirst      *bool     `yaml:"dirs_first"`
	Hidden         *string   `yaml:"hidden"`
	Checksum       *string   `yaml:"checksum"`
	Recursive      *bool     `yaml:"recursive"`
	MaxRendered    *int      `yaml:"max_rendered"`
	FollowSymlinks *bool     `yaml:"follow_symlinks"`
}

// deref returns *p when p is set, and fallback otherwise. It exists so Apply
// reads as ten straight-line assignments instead of ten branches — the branchy
// form pushed the function past the complexity budget while saying no more.
func deref[T any](p *T, fallback T) T {
	if p == nil {
		return fallback
	}
	return *p
}

// Apply returns s with every set field of o replacing the corresponding field.
// s is taken by value and never mutated, and Outputs is copied rather than
// aliased so two directories resolving from one override cannot share an array.
func (o Override) Apply(s Settings) Settings {
	s.Source = deref(o.Source, s.Source)
	s.Present = deref(o.Present, s.Present)
	s.Sort = deref(o.Sort, s.Sort)
	s.Order = deref(o.Order, s.Order)
	s.DirsFirst = deref(o.DirsFirst, s.DirsFirst)
	s.Hidden = deref(o.Hidden, s.Hidden)
	s.Checksum = deref(o.Checksum, s.Checksum)
	s.Recursive = deref(o.Recursive, s.Recursive)
	s.MaxRendered = deref(o.MaxRendered, s.MaxRendered)
	s.FollowSymlinks = deref(o.FollowSymlinks, s.FollowSymlinks)
	if o.Outputs != nil {
		s.Outputs = append([]string(nil), *o.Outputs...)
	}
	return s
}
