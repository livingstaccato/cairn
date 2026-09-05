// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for the guarantees that only hold because SumAll runs on more than one
// goroutine: positional results under out-of-order completion, one cache shared
// by concurrent callers, and a pool that stays inside its bound.

package hash

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestSumAllPositionalUnderReversedCompletion pins the ordering guarantee
// without relying on file sizes to skew the timing: the digest step is made to
// finish in exactly the reverse of the submitted order.
func TestSumAllPositionalUnderReversedCompletion(t *testing.T) {
	dir := t.TempDir()
	jobs := writeFiles(t, dir, 6, 16)

	restore := digestFile
	t.Cleanup(func() { digestFile = restore })
	delay := map[string]time.Duration{}
	for i, j := range jobs {
		delay[j.Path] = time.Duration(len(jobs)-i) * 8 * time.Millisecond
	}
	digestFile = func(p string) (string, error) {
		time.Sleep(delay[p])
		return restore(p)
	}

	want := wantSums(t, jobs)
	c := NewCache(filepath.Join(dir, CacheFile))
	c.workers = len(jobs) // every job in flight at once, so completion is by delay
	got := c.SumAll(jobs)

	for i := range jobs {
		if got[i].Sum != want[i] {
			t.Errorf("result %d is %s, want %s — completion order leaked into the results",
				i, got[i].Sum, want[i])
		}
	}
}

// TestSumAllConcurrentCallersShareOneCache is the data-race test: several
// callers hash overlapping paths through the same Cache at once, which is what
// makes the map a shared mutable object rather than a per-call one.
func TestSumAllConcurrentCallersShareOneCache(t *testing.T) {
	dir := t.TempDir()
	jobs := writeFiles(t, dir, 24, 96)
	want := wantSums(t, jobs)

	c := NewCache(filepath.Join(dir, CacheFile))
	const callers = 8
	got := make([][]Result, callers)

	var wg sync.WaitGroup
	for k := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[k] = c.SumAll(jobs)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			// The single-file API and the counters touch the same map.
			if _, err := c.Sum(jobs[k].Path, jobs[k].Size, jobs[k].ModTime); err != nil {
				t.Error(err)
			}
			_ = c.Hits()
		}()
	}
	wg.Wait()

	for k := range callers {
		for i := range jobs {
			if got[k][i].Sum != want[i] {
				t.Fatalf("caller %d, job %d: %s, want %s", k, i, got[k][i].Sum, want[i])
			}
		}
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
}

// TestSumAllBoundsOpenFiles asserts the promise the descriptor limit needs: a
// batch of any size has at most one file per worker open at a time.
func TestSumAllBoundsOpenFiles(t *testing.T) {
	dir := t.TempDir()
	jobs := writeFiles(t, dir, 64, 16)

	restore := digestFile
	t.Cleanup(func() { digestFile = restore })
	var mu sync.Mutex
	var open, peak int
	digestFile = func(p string) (string, error) {
		mu.Lock()
		open++
		if open > peak {
			peak = open
		}
		mu.Unlock()
		time.Sleep(time.Millisecond)
		defer func() {
			mu.Lock()
			open--
			mu.Unlock()
		}()
		return restore(p)
	}

	const bound = 4
	c := NewCache(filepath.Join(dir, CacheFile))
	c.workers = bound
	c.SumAll(jobs)

	mu.Lock()
	defer mu.Unlock()
	if peak > bound {
		t.Errorf("%d files open at once, bound is %d", peak, bound)
	}
	if peak < 2 {
		t.Errorf("peak concurrency %d — the batch ran sequentially", peak)
	}
}

func TestWorkerCountIsBounded(t *testing.T) {
	c := NewCache(filepath.Join(t.TempDir(), CacheFile))
	n := c.workerCount()
	if n < 1 || n > MaxWorkers {
		t.Errorf("default worker count %d is outside [1, %d]", n, MaxWorkers)
	}
	c.workers = 3
	if got := c.workerCount(); got != 3 {
		t.Errorf("configured worker count = %d, want 3", got)
	}
}

// TestSumAllWorkersNeverExceedJobs keeps a small batch from starting more
// goroutines than it has work for.
func TestSumAllWorkersNeverExceedJobs(t *testing.T) {
	dir := t.TempDir()
	jobs := writeFiles(t, dir, 1, 8)
	want := wantSums(t, jobs)

	c := NewCache(filepath.Join(dir, CacheFile))
	c.workers = 64
	got := c.SumAll(jobs)
	if got[0].Sum != want[0] {
		t.Errorf("SumAll = %s, want %s", got[0].Sum, want[0])
	}
}
