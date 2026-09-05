// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package hash

import (
	"fmt"
	"path/filepath"
	"testing"
)

// A cold directory of the shape this is for: a few thousand release artifacts,
// each big enough that the read is real work rather than one page.
const (
	benchFiles = 2048
	benchSize  = 24 << 10
)

// coldTree lays down the fixture once; b.Loop starts the clock after it.
func coldTree(b *testing.B) (string, []Job) {
	b.Helper()
	dir := b.TempDir()
	return dir, writeFiles(b, dir, benchFiles, benchSize)
}

// BenchmarkSum is the baseline: the sequential loop the caller does today.
func BenchmarkSum(b *testing.B) {
	dir, jobs := coldTree(b)
	b.SetBytes(int64(benchFiles) * benchSize)
	for b.Loop() {
		c := NewCache(filepath.Join(dir, CacheFile))
		for i := range jobs {
			if _, err := c.Sum(jobs[i].Path, jobs[i].Size, jobs[i].ModTime); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkSumAll is the same work through the batch API.
func BenchmarkSumAll(b *testing.B) {
	dir, jobs := coldTree(b)
	b.SetBytes(int64(benchFiles) * benchSize)
	for b.Loop() {
		c := NewCache(filepath.Join(dir, CacheFile))
		for _, r := range c.SumAll(jobs) {
			if r.Err != nil {
				b.Fatal(r.Err)
			}
		}
	}
}

// BenchmarkSumAllWorkers sweeps the bound, which is how workersPerProc and
// MaxWorkers were chosen rather than guessed.
func BenchmarkSumAllWorkers(b *testing.B) {
	for _, n := range []int{1, 2, 4, 8, 16, 32, 64} {
		b.Run(fmt.Sprintf("workers=%d", n), func(b *testing.B) {
			dir, jobs := coldTree(b)
			b.SetBytes(int64(benchFiles) * benchSize)
			for b.Loop() {
				c := NewCache(filepath.Join(dir, CacheFile))
				c.workers = n
				for _, r := range c.SumAll(jobs) {
					if r.Err != nil {
						b.Fatal(r.Err)
					}
				}
			}
		})
	}
}

// BenchmarkSumAllWarm measures the case a rebuild actually hits: every digest
// already cached, so the batch is two locked passes and no I/O at all.
func BenchmarkSumAllWarm(b *testing.B) {
	dir, jobs := coldTree(b)
	c := NewCache(filepath.Join(dir, CacheFile))
	for _, r := range c.SumAll(jobs) {
		if r.Err != nil {
			b.Fatal(r.Err)
		}
	}
	b.ResetTimer()
	for b.Loop() {
		c.SumAll(jobs)
	}
}
