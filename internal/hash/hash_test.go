// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestSumMatchesCryptoSHA256(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	body := []byte("bootstrap payload\n")
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}

	c := NewCache(filepath.Join(dir, CacheFile))
	got, err := c.Sum(p, fi.Size(), fi.ModTime().Unix())
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(body)
	if got != hex.EncodeToString(want[:]) {
		t.Errorf("Sum = %s, want %s", got, hex.EncodeToString(want[:]))
	}
	if len(got) != 64 {
		t.Errorf("digest length = %d, want 64 hex chars", len(got))
	}
}

func TestCacheAvoidsRehash(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(p)
	cp := filepath.Join(dir, CacheFile)

	c1 := NewCache(cp)
	if _, err := c1.Sum(p, fi.Size(), fi.ModTime().Unix()); err != nil {
		t.Fatal(err)
	}
	if err := c1.Save(); err != nil {
		t.Fatal(err)
	}

	c2 := NewCache(cp)
	if _, err := c2.Sum(p, fi.Size(), fi.ModTime().Unix()); err != nil {
		t.Fatal(err)
	}
	if c2.Hits() != 1 {
		t.Errorf("Hits = %d, want 1 — the saved cache was not reused", c2.Hits())
	}
}

func TestCacheInvalidatedByChange(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	if err := os.WriteFile(p, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(p)
	cp := filepath.Join(dir, CacheFile)

	c1 := NewCache(cp)
	first, _ := c1.Sum(p, fi.Size(), fi.ModTime().Unix())
	if err := c1.Save(); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(p, []byte("two different"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi2, _ := os.Stat(p)

	c2 := NewCache(cp)
	second, err := c2.Sum(p, fi2.Size(), fi2.ModTime().Unix())
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Error("changed file returned the stale cached digest")
	}
	if c2.Hits() != 0 {
		t.Errorf("Hits = %d, want 0", c2.Hits())
	}
}

func TestCorruptCacheIsNotFatal(t *testing.T) {
	dir := t.TempDir()
	cp := filepath.Join(dir, CacheFile)
	if err := os.WriteFile(cp, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "f.bin")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(p)

	c := NewCache(cp) // must not panic or fail
	if _, err := c.Sum(p, fi.Size(), fi.ModTime().Unix()); err != nil {
		t.Fatalf("a corrupt cache must be discarded, not fatal: %v", err)
	}
}

func TestSumMissingFileErrors(t *testing.T) {
	c := NewCache(filepath.Join(t.TempDir(), CacheFile))
	if _, err := c.Sum(filepath.Join(t.TempDir(), "absent"), 1, 1); err == nil {
		t.Fatal("expected an error hashing a file that is not there")
	}
}

func TestSaveIsNoOpWhenClean(t *testing.T) {
	cp := filepath.Join(t.TempDir(), CacheFile)
	c := NewCache(cp)
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cp); !os.IsNotExist(err) {
		t.Error("Save wrote a cache file despite nothing having been hashed")
	}
}

func TestSaveUnwritablePathErrors(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(p)

	// A directory standing where the cache file should go.
	cp := filepath.Join(dir, CacheFile)
	if err := os.Mkdir(cp, 0o755); err != nil {
		t.Fatal(err)
	}
	c := NewCache(cp)
	if _, err := c.Sum(p, fi.Size(), fi.ModTime().Unix()); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err == nil {
		t.Fatal("expected an error writing the cache over a directory")
	}
}
