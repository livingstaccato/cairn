// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

// Package walk turns a directory on disk into normalized entries.
package walk

import (
	"mime"
	"path/filepath"
	"strings"
	"time"
)

// Entry kinds. These are the values Entry.Kind takes, and they drive icon
// selection, filtering and search facets.
const (
	KindDir     = "dir"
	KindScript  = "script"
	KindImage   = "image"
	KindArchive = "archive"
	KindDoc     = "doc"
	KindPage    = "page"
	KindConfig  = "config"
	KindData    = "data"
	KindOther   = "other"

	// MIMEDirectory is what a directory reports; there is no registered type.
	MIMEDirectory = "inode/directory"
	// MIMEFallback is used when the extension maps to nothing known.
	MIMEFallback = "application/octet-stream"
)

var kindByExt = map[string]string{
	".sh": KindScript, ".bash": KindScript, ".zsh": KindScript,
	".ps1": KindScript, ".py": KindScript,

	".iso": KindImage, ".img": KindImage, ".qcow2": KindImage,
	".vmdk": KindImage, ".raw": KindImage,

	".tar": KindArchive, ".tar.gz": KindArchive, ".tgz": KindArchive,
	".tar.xz": KindArchive, ".tar.zst": KindArchive, ".tar.bz2": KindArchive,
	".zip": KindArchive, ".deb": KindArchive, ".rpm": KindArchive,
	".whl": KindArchive, ".gz": KindArchive, ".xz": KindArchive, ".zst": KindArchive,

	".md": KindDoc, ".txt": KindDoc, ".rst": KindDoc, ".pdf": KindDoc,

	".html": KindPage, ".htm": KindPage,

	".yaml": KindConfig, ".yml": KindConfig, ".toml": KindConfig,
	".ini": KindConfig, ".conf": KindConfig, ".cfg": KindConfig,

	".json": KindData, ".csv": KindData, ".tsv": KindData, ".xml": KindData,
}

// compoundExts are checked before the final extension, longest first. Without
// this "archive.tar.gz" reads as ".gz", which says how it is compressed rather
// than what it is.
var compoundExts = []string{".tar.gz", ".tar.xz", ".tar.zst", ".tar.bz2"}

// extensionOf returns the longest known compound extension, else the final one.
func extensionOf(name string) string {
	lower := strings.ToLower(name)
	for _, compound := range compoundExts {
		if strings.HasSuffix(lower, compound) {
			return compound
		}
	}
	return filepath.Ext(lower)
}

// KindOf classifies an entry for display, filtering and search facets.
func KindOf(name string, isDir bool) (kind, mimeType string) {
	if isDir {
		return KindDir, MIMEDirectory
	}
	ext := extensionOf(name)
	kind = kindByExt[ext]
	if kind == "" {
		kind = KindOther
	}
	mimeType = mime.TypeByExtension(ext)
	if mimeType == "" {
		mimeType = MIMEFallback
	}
	return kind, mimeType
}

// modTime normalizes a modification time for output.
//
// Whole seconds in UTC, because the same timestamp is rendered by three paths —
// JSON from Go, CSV from Go, and YAML that Hugo re-renders verbatim — and
// sub-second precision survives some of them and not others. A file listing has
// no use for nanoseconds, and a value that differs by path is worse than a
// coarse one.
func modTime(t time.Time) time.Time {
	return t.UTC().Truncate(time.Second)
}

// Metadata filenames cairn reads. They live here rather than in meta because
// walk decides what a listing contains, and meta imports walk.
const (
	// DirFile describes every entry in one directory.
	DirFile = "_meta.yaml"
	// SidecarSuffix marks a single file's metadata: ubuntu.iso.meta.yaml.
	SidecarSuffix = ".meta.yaml"
)

// IsSidecar reports whether name is metadata cairn reads rather than content it
// publishes.
//
// These were excluded only as a side effect of the underscore rule, so
// hidden: dotfiles — which exists precisely to show underscore-prefixed names —
// put them in listings, in SHA256SUMS, and on the page as though a reader were
// meant to download them. What describes a listing is not part of it, under any
// hidden policy.
func IsSidecar(name string) bool {
	return name == DirFile || strings.HasSuffix(name, SidecarSuffix)
}
