// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

// Package meta attaches authored context to entries. Files that cannot carry
// frontmatter — an ISO, a package, a binary — get their metadata from beside
// them instead.
package meta

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/livingstaccato/cairn/internal/model"
	"github.com/livingstaccato/cairn/internal/walk"
)

const (
	// SidecarSuffix marks a single file's metadata: ubuntu.iso.meta.yaml.
	SidecarSuffix = ".meta.yaml"
	// DirFile holds a whole directory's metadata, keyed by filename.
	DirFile = "_meta.yaml"
	// DirConfigFile holds per-directory settings, and a manifest source's
	// entry list.
	DirConfigFile = ".cairn.yaml"
)

// FileMeta is authored metadata for one file.
type FileMeta struct {
	Title   string         `yaml:"title"`
	Summary string         `yaml:"summary"`
	Kind    string         `yaml:"kind"`
	Tags    []string       `yaml:"tags"`
	Weight  int            `yaml:"weight"`
	Hidden  bool           `yaml:"hidden"`
	Extra   map[string]any `yaml:"extra"`
}

// merge returns base with every set field of over replacing it. Emptiness is
// the signal for "unset" here, unlike config.Override's pointers: a title of ""
// and an absent title mean the same thing to a reader, so the extra ceremony
// would buy nothing.
func (base FileMeta) merge(over FileMeta) FileMeta {
	if over.Title != "" {
		base.Title = over.Title
	}
	if over.Summary != "" {
		base.Summary = over.Summary
	}
	if over.Kind != "" {
		base.Kind = over.Kind
	}
	if over.Tags != nil {
		base.Tags = over.Tags
	}
	if over.Weight != 0 {
		base.Weight = over.Weight
	}
	if over.Hidden {
		base.Hidden = true
	}
	if over.Extra != nil {
		base.Extra = over.Extra
	}
	return base
}

// Load reads _meta.yaml and every <file>.meta.yaml sidecar in absDir. Sidecars
// take precedence. A key naming an absent file warns; malformed YAML fails,
// because silently dropping authored metadata produces a wrong index.
func Load(absDir string) (map[string]FileMeta, []walk.Warning, error) {
	des, err := os.ReadDir(absDir)
	if err != nil {
		return nil, nil, fmt.Errorf("read dir %s: %w", absDir, err)
	}

	out, warns, err := loadDirFile(absDir)
	if err != nil {
		return nil, nil, err
	}
	if err := loadSidecars(absDir, des, out); err != nil {
		return nil, nil, err
	}
	return out, warns, nil
}

// loadDirFile reads the directory-wide _meta.yaml, if present.
func loadDirFile(absDir string) (map[string]FileMeta, []walk.Warning, error) {
	out := map[string]FileMeta{}
	p := filepath.Join(absDir, DirFile)

	b, err := readIfPresent(p)
	if err != nil {
		return nil, nil, err
	}
	if b == nil {
		return out, nil, nil
	}

	var m map[string]FileMeta
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", p, err)
	}

	var warns []walk.Warning
	for name, fm := range m {
		if _, err := os.Lstat(filepath.Join(absDir, name)); err != nil {
			warns = append(warns, walk.Warning{
				Path: filepath.Join(absDir, name),
				Err:  fmt.Errorf("%s names a file that is not present", DirFile),
			})
			continue
		}
		out[name] = fm
	}
	return out, warns, nil
}

// loadSidecars merges each <file>.meta.yaml over the directory-wide entry.
func loadSidecars(absDir string, des []os.DirEntry, out map[string]FileMeta) error {
	for _, de := range des {
		n := de.Name()
		if !strings.HasSuffix(n, SidecarSuffix) {
			continue
		}
		p := filepath.Join(absDir, n)
		b, err := readIfPresent(p)
		if err != nil {
			return err
		}
		var fm FileMeta
		if err := yaml.Unmarshal(b, &fm); err != nil {
			return fmt.Errorf("parse %s: %w", p, err)
		}
		target := strings.TrimSuffix(n, SidecarSuffix)
		out[target] = out[target].merge(fm)
	}
	return nil
}

// readIfPresent returns nil bytes and no error when the file does not exist.
func readIfPresent(p string) ([]byte, error) {
	// #nosec G304 -- p is built from a directory cairn was configured to scan
	// plus a fixed filename, never from user-supplied text.
	b, err := os.ReadFile(p)
	if err == nil {
		return b, nil
	}
	if os.IsNotExist(err) {
		return nil, nil
	}
	return nil, fmt.Errorf("read %s: %w", p, err)
}

// Apply copies metadata onto matching entries, returning a new slice.
func Apply(entries []model.Entry, m map[string]FileMeta) []model.Entry {
	out := make([]model.Entry, len(entries))
	copy(out, entries)
	for i := range out {
		fm, ok := m[out[i].Name]
		if !ok {
			continue
		}
		applyOne(&out[i], fm)
	}
	return out
}

// applyOne writes one file's authored metadata onto its entry.
func applyOne(e *model.Entry, fm FileMeta) {
	if fm.Title != "" {
		e.Title = fm.Title
	}
	if fm.Summary != "" {
		e.Summary = fm.Summary
	}
	if fm.Kind != "" {
		e.Kind = fm.Kind
	}
	if fm.Tags != nil {
		e.Tags = fm.Tags
	}
	if fm.Extra != nil {
		e.Extra = fm.Extra
	}
}

// SourceMaxBytes caps how much authored metadata is inlined into a page. A
// directory description that runs past this is a file to fetch, not something
// to read inline.
const SourceMaxBytes = 8 << 10

// SourceFile returns the name of the authored file describing this directory,
// or "" when there is none.
func SourceFile(absDir string) string {
	for _, name := range []string{DirFile, DirConfigFile} {
		if _, err := os.Lstat(filepath.Join(absDir, name)); err == nil {
			return name
		}
	}
	return ""
}

// Source returns the name and contents of the authored file describing this
// directory, for the listing to show inline.
//
// Inline rather than linked: a link to a .yaml downloads it on any host that
// does not declare a MIME type for the extension, which includes the default
// nginx config and every static host where you cannot set one. A reader asking
// why an entry is titled the way it is should see the answer, not a file in
// their downloads folder.
func Source(absDir string) (FileSource, error) {
	name := SourceFile(absDir)
	if name == "" {
		return FileSource{}, nil
	}
	b, err := readIfPresent(filepath.Join(absDir, name))
	if err != nil {
		return FileSource{}, err
	}
	if len(b) > SourceMaxBytes {
		b = b[:SourceMaxBytes]
	}
	return FileSource{Name: name, Text: string(b)}, nil
}

// FileSource is the authored file describing a directory, shown inline.
type FileSource struct {
	Name string
	Text string
}

// proseFiles are the directory-introduction filenames, in precedence order.
var proseFiles = []string{"README.md", "_index.md"}

// Prose returns the directory's authored introduction, rendered above the
// listing. README.md wins over _index.md.
func Prose(absDir string) (string, error) {
	for _, name := range proseFiles {
		b, err := readIfPresent(filepath.Join(absDir, name))
		if err != nil {
			return "", err
		}
		if b != nil {
			return string(b), nil
		}
	}
	return "", nil
}
