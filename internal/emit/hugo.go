// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

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

// cairnParam is the listing as a template sees it. The keys match the JSON
// contract exactly: a Hugo template and a jq pipeline read the same names.
type cairnParam struct {
	Present    string        `yaml:"present"`
	Path       string        `yaml:"path"`
	Source     string        `yaml:"source,omitempty"`
	SourceText string        `yaml:"source_text,omitempty"`
	Generated  string        `yaml:"generated"`
	Count      int           `yaml:"count"`
	Entries    []model.Entry `yaml:"entries"`
}

// HugoContent renders one directory as a Hugo branch-bundle _index.md carrying
// the listing in frontmatter.
//
// Hugo's output formats then produce HTML, JSON and CSV from this single
// source, so the three cannot drift from each other. cairn writes one file per
// directory in this mode and lets Hugo publish the rest.
func HugoContent(l model.Listing, prose, present, source, sourceText string) ([]byte, error) {
	fm := hugoFrontmatter{
		Title:  titleFor(l.Path),
		Layout: HugoLayout,
		Cairn: cairnParam{
			Present:    present,
			Path:       l.Path,
			Source:     source,
			SourceText: sourceText,
			Generated:  l.Generated.Format(time.RFC3339),
			Count:      l.Count,
			Entries:    l.Entries,
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
	buf.WriteString(prose)
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
