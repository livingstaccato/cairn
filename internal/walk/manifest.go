// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package walk

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/livingstaccato/cairndex/internal/config"
	"github.com/livingstaccato/cairndex/internal/model"
)

// DirConfigFile holds a directory's cairndex overrides, and for a manifest source
// its entry list too.
const DirConfigFile = ".cairndex.yaml"

// ManifestEntry is one authored listing row.
//
// It exists for directories whose contents are not on disk at build time: a
// mirror populated after deploy, or an artifact fetched from elsewhere during
// provisioning.
type ManifestEntry struct {
	Name     string    `yaml:"name"`
	Path     string    `yaml:"path"`
	Title    string    `yaml:"title"`
	Summary  string    `yaml:"summary"`
	Kind     string    `yaml:"kind"`
	Size     int64     `yaml:"size"`
	Tags     []string  `yaml:"tags"`
	SHA256   string    `yaml:"sha256"`
	Modified time.Time `yaml:"modified"`
}

type manifestFile struct {
	Entries []ManifestEntry `yaml:"entries"`
}

// Manifest reads an authored entry list from a directory's .cairndex.yaml.
//
// An entry missing name or path warns and is skipped; a directory with no
// .cairndex.yaml simply lists nothing. Malformed YAML fails, for the same reason
// _meta.yaml does: silently dropping authored content produces an index that
// looks complete and is not.
func Manifest(absDir string, s config.Settings) ([]model.Entry, []Warning, error) {
	p := filepath.Join(absDir, DirConfigFile)
	// #nosec G304 -- composed from a directory cairndex was configured to scan and
	// a fixed filename.
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read %s: %w", p, err)
	}
	var mf manifestFile
	if err := yaml.Unmarshal(b, &mf); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", p, err)
	}

	var out []model.Entry
	var warns []Warning
	for i, me := range mf.Entries {
		if me.Name == "" || me.Path == "" {
			warns = append(warns, Warning{
				Path: p,
				Err:  fmt.Errorf("entries[%d] needs both name and path, skipped", i),
			})
			continue
		}
		out = append(out, me.entry())
	}
	sortEntries(out, s)
	return out, warns, nil
}

// entry converts an authored row into the normalized record.
func (me ManifestEntry) entry() model.Entry {
	kind, mimeType := KindOf(me.Name, me.Kind == KindDir)
	if me.Kind != "" {
		kind = me.Kind
	}
	isDir := kind == KindDir
	size := me.Size
	if isDir {
		// Matches fs.go's own entry(): Entry.Size is exact bytes, and a
		// directory does not have a meaningful one. An authored size: on a
		// dir row — plausibly copied from a filesystem's own directory
		// inode size — must not reach a client sorting by size as though it
		// were a real file.
		size = 0
	}
	return model.Entry{
		Name: me.Name, Path: me.Path, IsDir: isDir,
		Size: size, ModTime: modTime(me.Modified),
		Kind: kind, MIME: mimeType, SHA256: me.SHA256,
		Title: me.Title, Summary: me.Summary, Tags: me.Tags, Depth: 1,
	}
}
