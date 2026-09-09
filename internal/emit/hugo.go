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

	"github.com/livingstaccato/cairndex/internal/model"
)

const (
	// HugoLayout is the layout name cairndex stamps on every generated page, so a
	// theme can bind templates to it without guessing at section names.
	HugoLayout = "cairndex"
	// HugoPEP503Layout is the layout name a pep503 page gets instead. Its
	// template has no {{ define "main" }} block, so Hugo renders it as a
	// complete document rather than wrapping it in the theme's baseof.html —
	// PEP 503 defines a minimal page of anchors with no reader but pip, and
	// no chrome belongs on it, the same reason present:/the format switcher
	// do not either.
	HugoPEP503Layout = "cairndex-pep503"
	// HugoSearchLayout is the layout the search page's own branch bundle
	// gets, one level under the listing it searches. No {{ define "main" }}
	// block, same reason as HugoPEP503Layout: Hugo also refuses to publish a
	// raw .html bundle resource verbatim, so the search box needs a real
	// page rather than a plain file mode: direct alone could get away with.
	HugoSearchLayout = "cairndex-search"
	// HugoContentFile is the branch-bundle filename cairndex writes per directory.
	HugoContentFile = "_index.md"
	// rootTitle names the top of the tree, which has no directory name.
	rootTitle = "Index"
)

// hugoFrontmatter is what a Hugo template reads back out of .Params. Field
// names are lowercase because Hugo exposes frontmatter keys verbatim.
type hugoFrontmatter struct {
	Title    string        `yaml:"title"`
	Layout   string        `yaml:"layout"`
	Cairndex cairndexParam `yaml:"cairndex"`
}

// cairndexParam is what a template needs *about* the listing. The entries
// themselves are not here: they travel as an index.json page resource beside the
// page, which the template unmarshals.
//
// Inline in frontmatter, Hugo refuses any directory past roughly ten thousand
// entries — "too many YAML aliases for non-scalar nodes", a decoder limit rather
// than a memory or time one, and a normal package pool exceeds it. A
// 50,000-entry directory renders from a JSON resource in 0.36s.
type cairndexParam struct {
	Present    string   `yaml:"present"`
	Path       string   `yaml:"path"`
	Source     string   `yaml:"source,omitempty"`
	SourceText string   `yaml:"source_text,omitempty"`
	Formats    []string `yaml:"formats,omitempty"`
	Generated  string   `yaml:"generated"`
	Count      int      `yaml:"count"`
	// TotalSize is the sum of Size across the listing's own files —
	// immediate children only, or every descendant for tree.json — see
	// model.Listing.TotalSize. No omitempty: an empty directory's 0 is as
	// meaningful as Count's own unconditional field.
	TotalSize int64 `yaml:"total_size"`
	Recursive bool  `yaml:"recursive,omitempty"`
	// BasePath is where the indexed tree starts inside the site, so a template
	// can build a link to the top of it. The breadcrumb's root anchor is the
	// case: hardcoded to "/" it points above the mirror under base_path, and at
	// the filesystem root from file://.
	BasePath string `yaml:"base_path,omitempty"`
	// AtRoot suppresses the parent row, the same switch BarePage carries. The
	// Hugo templates render the bare presenter themselves — emitHugo never calls
	// BareHTML — so without this the two renderers disagree about the top of the
	// tree, and the Hugo one offers a link above it.
	//
	// It travels as data because a template cannot work it out: under base_path
	// every listing path carries the prefix, so the top of the tree is not "/".
	AtRoot bool `yaml:"at_root,omitempty"`
	// MaxRendered is the row cap the template applies. It travels in the
	// frontmatter rather than being decided in the template so one config key
	// governs both presenters and both modes.
	MaxRendered int `yaml:"max_rendered,omitempty"`
	// PEP503 tells the template to render the PEP 503 simple-index page
	// instead of the normal listing, bypassing present: entirely — pep503
	// is requested through outputs:, an independent axis a template cannot
	// otherwise see, the same reason BasePath and AtRoot travel as data.
	PEP503 bool `yaml:"pep503,omitempty"`
	// BuildInfoVersion and BuildInfoGenerated are the small footer banner a
	// listing shows by default: what built this and when. Absent rather
	// than a separate Show flag alongside them — a template checks
	// build_info_version's truthiness, the same pattern BasePath's own
	// omitempty already uses, so there is one field to check instead of
	// two that could disagree.
	BuildInfoVersion   string `yaml:"build_info_version,omitempty"`
	BuildInfoGenerated string `yaml:"build_info_generated,omitempty"`
	// ShowOwner adds owner, group and permission columns. Travels as data
	// for the same reason MaxRendered does: the directory's show_owner
	// setting is not something either template can otherwise see.
	ShowOwner bool `yaml:"show_owner,omitempty"`
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
	// AtRoot is the top of the indexed tree, where a parent link would point at
	// something cairndex never wrote. The same switch BarePage carries, for the
	// renderer that is not BareHTML.
	AtRoot bool
	// BasePath is the site-absolute prefix the tree is published under, "" when
	// it is published at the site root.
	BasePath string
	// PEP503 is whether this directory's outputs: include pep503. See
	// cairndexParam.PEP503.
	PEP503 bool
	// BuildInfo is the small footer banner every listing shows by default:
	// what built it and when. The zero value carries nothing to the
	// template.
	BuildInfo BuildInfo
	// ShowOwner is whether this directory's show_owner setting is on. See
	// cairndexParam.ShowOwner.
	ShowOwner bool
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
	layout := HugoLayout
	if p.PEP503 {
		layout = HugoPEP503Layout
	}
	var buildInfoGenerated string
	if p.BuildInfo.Version != "" {
		buildInfoGenerated = p.BuildInfo.Generated.Format(time.RFC3339)
	}
	fm := hugoFrontmatter{
		Title:  titleFor(l.Path, p.AtRoot),
		Layout: layout,
		Cairndex: cairndexParam{
			Present:            p.Present,
			Path:               l.Path,
			Source:             p.Source,
			Formats:            p.Formats,
			SourceText:         p.SourceText,
			Generated:          l.Generated.Format(time.RFC3339),
			Count:              l.Count,
			TotalSize:          l.TotalSize,
			Recursive:          p.Recursive,
			MaxRendered:        p.MaxRendered,
			AtRoot:             p.AtRoot,
			BasePath:           p.BasePath,
			PEP503:             p.PEP503,
			BuildInfoVersion:   p.BuildInfo.Version,
			BuildInfoGenerated: buildInfoGenerated,
			ShowOwner:          p.ShowOwner,
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

// HugoSearchContent renders the _index.md for the search page's own branch
// bundle. It carries no cairndex param block: the layout is fully static,
// unlike the listing's own page, so there is nothing for a template to read
// beside the title.
func HugoSearchContent(dirPath string) ([]byte, error) {
	fm := struct {
		Title  string `yaml:"title"`
		Layout string `yaml:"layout"`
	}{Title: "Search " + dirPath, Layout: HugoSearchLayout}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(fm); err != nil {
		return nil, fmt.Errorf("encode search frontmatter for %s: %w", dirPath, err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("close search frontmatter encoder for %s: %w", dirPath, err)
	}
	buf.WriteString("---\n")
	return buf.Bytes(), nil
}

// titleFor names a directory page from its path.
//
// atRoot decides it, not a string match against p: BasePath prepends a prefix
// to l.Path before this ever runs, so a configured base_path leaves the root
// directory's path looking like any other and a match against "", ".", "/"
// misses it.
func titleFor(p string, atRoot bool) string {
	if atRoot {
		return rootTitle
	}
	return path.Base(strings.TrimSuffix(p, "/"))
}
