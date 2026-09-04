// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package emit

import (
	"bytes"
	"fmt"
	"path"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/livingstaccato/cairn/internal/model"
)

const (
	// HugoLayout is the layout name cairn stamps on every generated page, so a
	// theme can bind templates to it without guessing at section names.
	HugoLayout = "cairn"
	// HugoContentFile is the branch-bundle filename cairn writes per directory.
	HugoContentFile = "_index.md"
	// rootTitle names the top of the tree, which has no directory name.
	rootTitle = "Index"
)

// hugoFrontmatter is what a Hugo template reads back out of .Params. Field
// names are lowercase because Hugo exposes frontmatter keys verbatim.
type hugoFrontmatter struct {
	Title  string     `yaml:"title"`
	Layout string     `yaml:"layout"`
	Cairn  cairnParam `yaml:"cairn"`
}

// cairnParam is what a template needs *about* the listing. The entries
// themselves are not here: they travel as an index.json page resource beside the
// page, which the template unmarshals.
//
// Inline in frontmatter, Hugo refuses any directory past roughly ten thousand
// entries — "too many YAML aliases for non-scalar nodes", a decoder limit rather
// than a memory or time one, and a normal package pool exceeds it. A
// 50,000-entry directory renders from a JSON resource in 0.36s.
type cairnParam struct {
	Present    string   `yaml:"present"`
	Path       string   `yaml:"path"`
	Source     string   `yaml:"source,omitempty"`
	SourceText string   `yaml:"source_text,omitempty"`
	Formats    []string `yaml:"formats,omitempty"`
	Generated  string   `yaml:"generated"`
	Count      int      `yaml:"count"`
	Recursive  bool     `yaml:"recursive,omitempty"`
	// MaxRendered is the row cap the template applies. It travels in the
	// frontmatter rather than being decided in the template so one config key
	// governs both presenters and both modes.
	MaxRendered int `yaml:"max_rendered,omitempty"`
}

// HugoPage is everything a directory's page needs to know about itself.
//
// A struct rather than a parameter list: five of these are strings, and a call
// site passing five positional strings is one transposition away from a bug no
// compiler catches.
type HugoPage struct {
	Listing    model.Listing
	Prose      string
	Present    string
	Source     string
	SourceText string
	Formats    []string
	// MaxRendered bounds the rows the template renders; 0 renders every one.
	MaxRendered int
	// Recursive tells the template this directory also publishes tree.json, so
	// the format switcher can offer it. Without it the recursive listing is
	// written and nothing ever links to it.
	Recursive bool
}

// HugoContent renders one directory as a Hugo branch-bundle _index.md.
//
// It carries what a template needs *about* the listing; the entries arrive as an
// index.json resource in the same bundle.
//
// Hugo renders this into index.html; every other output is written beside it as
// a bundle resource and published verbatim, so nothing is produced twice.
func HugoContent(p HugoPage) ([]byte, error) {
	l := p.Listing
	fm := hugoFrontmatter{
		Title:  titleFor(l.Path),
		Layout: HugoLayout,
		Cairn: cairnParam{
			Present:     p.Present,
			Path:        l.Path,
			Source:      p.Source,
			Formats:     p.Formats,
			SourceText:  p.SourceText,
			Generated:   l.Generated.Format(time.RFC3339),
			Count:       l.Count,
			Recursive:   p.Recursive,
			MaxRendered: p.MaxRendered,
		},
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(fm); err != nil {
		return nil, fmt.Errorf("encode frontmatter for %s: %w", l.Path, err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("close frontmatter encoder for %s: %w", l.Path, err)
	}
	buf.WriteString("---\n")
	buf.WriteString(p.Prose)
	return buf.Bytes(), nil
}

// titleFor names a directory page from its path.
func titleFor(p string) string {
	base := path.Base(strings.TrimSuffix(p, "/"))
	switch base {
	case "", ".", "/":
		return rootTitle
	}
	return base
}
