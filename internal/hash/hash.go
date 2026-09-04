// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

// Package hash computes SHA-256 digests with a (path, size, mtime) cache, so a
// mirror holding multi-gigabyte images re-hashes only what changed.
package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// CacheFile is the conventional filename for a cache, written under the output
// directory.
const CacheFile = ".cairn-cache.json"

// record is one cached digest plus the stat fields that validate it.
type record struct {
	Size    int64  `json:"size"`
	ModUnix int64  `json:"mod_unix"`
	Sum     string `json:"sum"`
}

// Cache memoizes digests across runs.
type Cache struct {
	path    string
	entries map[string]record
	hits    int
	dirty   bool
}

// NewCache loads path if it is readable and parseable. A missing or corrupt
// cache yields an empty one: it is an optimization, never a source of truth, so
// there is nothing a bad cache can make wrong except the time a run takes.
func NewCache(path string) *Cache {
	c := &Cache{path: path, entries: map[string]record{}}
	// #nosec G304 -- path is composed by cairn from the configured output
	// directory and a fixed filename.
	b, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	var loaded map[string]record
	if err := json.Unmarshal(b, &loaded); err != nil {
		return c
	}
	c.entries = loaded
	return c
}

// Sum returns the hex digest of absPath, reusing the cached value when size and
// mtime are both unchanged.
func (c *Cache) Sum(absPath string, size, modUnix int64) (string, error) {
	if r, ok := c.entries[absPath]; ok && r.Size == size && r.ModUnix == modUnix {
		c.hits++
		return r.Sum, nil
	}
	sum, err := digest(absPath)
	if err != nil {
		return "", err
	}
	c.entries[absPath] = record{Size: size, ModUnix: modUnix, Sum: sum}
	c.dirty = true
	return sum, nil
}

// digest streams a file through SHA-256. Streaming rather than reading whole:
// the files this exists for are disk images.
func digest(absPath string) (string, error) {
	// #nosec G304 -- absPath comes from a directory walk of the tree cairn was
	// configured to index. Containment of *written* paths is emit.Write's job;
	// this only reads what the walk already listed.
	f, err := os.Open(absPath)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", absPath, err)
	}
	// Close's error is discarded deliberately: this handle is read-only, so a
	// failure to close cannot have lost data, and the digest above is already
	// computed. Discarding it explicitly says that, where a bare defer reads as
	// an oversight.
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash %s: %w", absPath, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Hits reports cache hits since load.
func (c *Cache) Hits() int { return c.hits }

// Save writes the cache back, if anything changed.
func (c *Cache) Save() error {
	if !c.dirty {
		return nil
	}
	b, err := json.Marshal(c.entries)
	if err != nil {
		return fmt.Errorf("marshal cache: %w", err)
	}
	if err := os.WriteFile(c.path, b, 0o600); err != nil {
		return fmt.Errorf("write cache %s: %w", c.path, err)
	}
	c.dirty = false
	return nil
}
