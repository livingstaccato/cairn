// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

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
	"sync"
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
//
// Every field after path is guarded by mu. SumAll hashes a batch on several
// goroutines and a caller may run batches concurrently, so the map is shared
// mutable state; the lock is never held across a read of a file, only across
// the map operations at either end of a batch.
type Cache struct {
	path    string
	mu      sync.Mutex
	entries map[string]record
	hits    int
	dirty   bool
	// workers overrides the computed bound in SumAll. Zero means "decide from
	// the machine"; it exists so a test can pin the bound it is asserting.
	workers int
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
	if sum, ok := c.cached(absPath, size, modUnix); ok {
		return sum, nil
	}
	// Deliberately not under the lock: the read is the slow part, and holding
	// the lock across it would serialize concurrent callers on the one thing
	// that is worth doing at the same time.
	sum, err := digestFile(absPath)
	if err != nil {
		return "", err
	}
	c.store(absPath, size, modUnix, sum)
	return sum, nil
}

// matches reports whether this record still describes a file of that size and
// mtime — the whole of the cache's validity rule, in one place, because Sum and
// SumAll must not drift on what counts as a hit.
func (r record) matches(size, modUnix int64) bool {
	return r.Size == size && r.ModUnix == modUnix
}

// cached returns the recorded digest for absPath if it is still valid, counting
// the hit.
func (c *Cache) cached(absPath string, size, modUnix int64) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.entries[absPath]
	if !ok || !r.matches(size, modUnix) {
		return "", false
	}
	c.hits++
	return r.Sum, true
}

// store records a freshly computed digest.
func (c *Cache) store(absPath string, size, modUnix int64, sum string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[absPath] = record{Size: size, ModUnix: modUnix, Sum: sum}
	c.dirty = true
}

// digestFile is the file-reading step, indirected so a test can watch how many
// run at once. The descriptor bound is a promise about behaviour, and a promise
// no test can observe is not one.
var digestFile = digest

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
func (c *Cache) Hits() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits
}

// Save writes the cache back, if anything changed.
//
// The lock is held across the write: what lands on disk has to be one moment's
// snapshot of the map, and Save runs once at the end of a build rather than on
// the hot path, so the cost of holding it is a run's worth of nothing.
func (c *Cache) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
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
