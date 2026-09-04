// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package walk

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/model"
)

// DirConfigFile holds a directory's cairn overrides, and for a manifest source
// its entry list too.
const DirConfigFile = ".cairn.yaml"

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

// Manifest reads an authored entry list from a directory's .cairn.yaml.
//
// An entry missing name or path warns and is skipped; a directory with no
// .cairn.yaml simply lists nothing. Malformed YAML fails, for the same reason
// _meta.yaml does: silently dropping authored content produces an index that
// looks complete and is not.
func Manifest(absDir string, s config.Settings) ([]model.Entry, []Warning, error) {
	p := filepath.Join(absDir, DirConfigFile)
	// #nosec G304 -- composed from a directory cairn was configured to scan and
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
	return model.Entry{
		Name: me.Name, Path: me.Path, IsDir: kind == KindDir,
		Size: me.Size, ModTime: modTime(me.Modified),
		Kind: kind, MIME: mimeType, SHA256: me.SHA256,
		Title: me.Title, Summary: me.Summary, Tags: me.Tags, Depth: 1,
	}
}
