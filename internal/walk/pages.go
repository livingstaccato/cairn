// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/model"
)

const (
	markdownExt = ".md"
	// pageMIME is what a rendered content page is served as, not what the
	// source file is.
	pageMIME = "text/html"
)

// sectionPages are a section's own page rather than an entry within it.
var sectionPages = map[string]bool{"_index.md": true, "index.md": true}

// pageFrontmatter is the subset of a Hugo page's frontmatter cairn reads.
type pageFrontmatter struct {
	Title   string   `yaml:"title"`
	Summary string   `yaml:"summary"`
	Tags    []string `yaml:"tags"`
	Draft   bool     `yaml:"draft"`
}

// Pages lists an existing Hugo content section: its Markdown pages and its
// child sections.
//
// A page whose frontmatter does not parse warns and is omitted. One broken page
// must not take a whole index down with it, and the warning names the file.
func Pages(contentRoot, relDir string, s config.Settings) ([]model.Entry, []Warning, error) {
	absDir := filepath.Join(contentRoot, filepath.FromSlash(relDir))
	des, err := os.ReadDir(absDir)
	if err != nil {
		return nil, nil, fmt.Errorf("read content dir %s: %w", relDir, err)
	}

	var out []model.Entry
	var warns []Warning
	for _, de := range des {
		e, w, ok := pageEntry(absDir, relDir, de, s)
		warns = append(warns, w...)
		if ok {
			out = append(out, e)
		}
	}
	sortEntries(out, s)
	return out, warns, nil
}

// pageEntry builds one content entry, reporting whether it belongs in the list.
func pageEntry(absDir, relDir string, de os.DirEntry, s config.Settings) (model.Entry, []Warning, bool) {
	name := de.Name()
	if s.Hidden != config.HiddenShow && isHidden(name) {
		return model.Entry{}, nil, false
	}
	info, err := de.Info()
	if err != nil {
		return model.Entry{}, []Warning{{Path: path.Join(relDir, name), Err: err}}, false
	}

	if de.IsDir() {
		return model.Entry{
			Name: name, Path: "/" + path.Join(relDir, name) + "/",
			IsDir: true, Kind: KindDir, MIME: MIMEDirectory,
			ModTime: modTime(info.ModTime()), Depth: 1,
		}, nil, true
	}
	if !strings.HasSuffix(name, markdownExt) || sectionPages[name] {
		return model.Entry{}, nil, false
	}

	fm, err := readFrontmatter(filepath.Join(absDir, name))
	if err != nil {
		return model.Entry{}, []Warning{{Path: path.Join(relDir, name), Err: err}}, false
	}
	if fm.Draft {
		return model.Entry{}, nil, false
	}

	slug := strings.TrimSuffix(name, markdownExt)
	title := fm.Title
	if title == "" {
		title = slug
	}
	return model.Entry{
		Name: slug, Path: "/" + path.Join(relDir, slug) + "/",
		Size: info.Size(), ModTime: modTime(info.ModTime()),
		Kind: KindPage, MIME: pageMIME,
		Title: title, Summary: fm.Summary, Tags: fm.Tags, Depth: 1,
	}, nil, true
}

// readFrontmatter parses a page's leading YAML block. A page without one is not
// an error: Hugo renders it, so cairn lists it.
func readFrontmatter(p string) (pageFrontmatter, error) {
	var fm pageFrontmatter
	// #nosec G304 -- p comes from a directory walk of the content root cairn
	// was configured to read.
	b, err := os.ReadFile(p)
	if err != nil {
		return fm, err
	}
	const fence = "---\n"
	if !bytes.HasPrefix(b, []byte(fence)) {
		return fm, nil
	}
	rest := b[len(fence):]
	end := bytes.Index(rest, []byte("\n---"))
	if end < 0 {
		return fm, fmt.Errorf("unterminated frontmatter")
	}
	if err := yaml.Unmarshal(rest[:end], &fm); err != nil {
		return fm, fmt.Errorf("parse frontmatter: %w", err)
	}
	return fm, nil
}
