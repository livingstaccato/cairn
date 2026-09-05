// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// writeFiles fills dir with n files of size bytes, each with distinct contents,
// and returns the jobs that digest them in the order they were written.
func writeFiles(t testing.TB, dir string, n, size int) []Job {
	t.Helper()
	jobs := make([]Job, 0, n)
	for i := range n {
		body := make([]byte, size)
		for j := range body {
			body[j] = byte(i + j)
		}
		p := filepath.Join(dir, fmt.Sprintf("f%04d.bin", i))
		if err := os.WriteFile(p, body, 0o600); err != nil {
			t.Fatal(err)
		}
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		jobs = append(jobs, Job{Path: p, Size: fi.Size(), ModTime: fi.ModTime().Unix()})
	}
	return jobs
}

// wantSums digests every job the slow, obvious way, for comparison.
func wantSums(t testing.TB, jobs []Job) []string {
	t.Helper()
	out := make([]string, len(jobs))
	for i, j := range jobs {
		b, err := os.ReadFile(j.Path) // #nosec G304 -- test fixture path
		if err != nil {
			t.Fatal(err)
		}
		s := sha256.Sum256(b)
		out[i] = hex.EncodeToString(s[:])
	}
	return out
}

func TestSumAllIsPositional(t *testing.T) {
	dir := t.TempDir()
	jobs := writeFiles(t, dir, 32, 64)
	// One file far larger than the rest, first in the batch: it finishes last,
	// so a result slice built in completion order puts the wrong digest at 0.
	big := make([]byte, 8<<20)
	for i := range big {
		big[i] = byte(i)
	}
	p := filepath.Join(dir, "big.bin")
	if err := os.WriteFile(p, big, 0o600); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	jobs = append([]Job{{Path: p, Size: fi.Size(), ModTime: fi.ModTime().Unix()}}, jobs...)

	want := wantSums(t, jobs)
	c := NewCache(filepath.Join(dir, CacheFile))
	got := c.SumAll(jobs)

	if len(got) != len(jobs) {
		t.Fatalf("SumAll returned %d results for %d jobs", len(got), len(jobs))
	}
	for i := range jobs {
		if got[i].Err != nil {
			t.Fatalf("job %d (%s): %v", i, jobs[i].Path, got[i].Err)
		}
		if got[i].Sum != want[i] {
			t.Errorf("result %d is %s, want %s — results are not positional",
				i, got[i].Sum, want[i])
		}
	}
}

func TestSumAllAgreesWithSum(t *testing.T) {
	dir := t.TempDir()
	jobs := writeFiles(t, dir, 16, 128)

	seq := NewCache(filepath.Join(dir, "seq"+CacheFile))
	want := make([]string, len(jobs))
	for i, j := range jobs {
		s, err := seq.Sum(j.Path, j.Size, j.ModTime)
		if err != nil {
			t.Fatal(err)
		}
		want[i] = s
	}

	batch := NewCache(filepath.Join(dir, CacheFile))
	got := batch.SumAll(jobs)
	for i := range jobs {
		if got[i].Err != nil {
			t.Fatal(got[i].Err)
		}
		if got[i].Sum != want[i] {
			t.Errorf("job %d: SumAll = %s, Sum = %s", i, got[i].Sum, want[i])
		}
	}
}

func TestSumAllPopulatesAndReusesTheCache(t *testing.T) {
	dir := t.TempDir()
	jobs := writeFiles(t, dir, 12, 64)
	cp := filepath.Join(dir, CacheFile)

	c1 := NewCache(cp)
	first := c1.SumAll(jobs)
	if c1.Hits() != 0 {
		t.Errorf("Hits = %d on a cold cache, want 0", c1.Hits())
	}
	if err := c1.Save(); err != nil {
		t.Fatal(err)
	}

	c2 := NewCache(cp)
	second := c2.SumAll(jobs)
	if c2.Hits() != len(jobs) {
		t.Errorf("Hits = %d, want %d — the saved cache was not reused", c2.Hits(), len(jobs))
	}
	for i := range jobs {
		if first[i].Sum != second[i].Sum {
			t.Errorf("job %d: cold %s, warm %s", i, first[i].Sum, second[i].Sum)
		}
	}
}

func TestSumAllHonoursStaleMtime(t *testing.T) {
	dir := t.TempDir()
	jobs := writeFiles(t, dir, 4, 32)
	c := NewCache(filepath.Join(dir, CacheFile))
	first := c.SumAll(jobs)

	if err := os.WriteFile(jobs[1].Path, []byte("rewritten, longer than before"), 0o600); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(jobs[1].Path)
	if err != nil {
		t.Fatal(err)
	}
	jobs[1].Size, jobs[1].ModTime = fi.Size(), fi.ModTime().Unix()

	second := c.SumAll(jobs)
	if second[1].Sum == first[1].Sum {
		t.Error("a changed file returned the stale cached digest")
	}
	if c.Hits() != len(jobs)-1 {
		t.Errorf("Hits = %d, want %d", c.Hits(), len(jobs)-1)
	}
}

func TestSumAllOneErrorDoesNotFailTheBatch(t *testing.T) {
	dir := t.TempDir()
	jobs := writeFiles(t, dir, 8, 48)
	want := wantSums(t, jobs)
	// A file the walk listed and something then removed.
	bad := Job{Path: filepath.Join(dir, "absent.bin"), Size: 1, ModTime: 1}
	jobs = append(jobs[:4], append([]Job{bad}, jobs[4:]...)...)
	want = append(want[:4], append([]string{""}, want[4:]...)...)

	c := NewCache(filepath.Join(dir, CacheFile))
	got := c.SumAll(jobs)

	if got[4].Err == nil {
		t.Error("a missing file must report an error in its own Result")
	}
	if got[4].Sum != "" {
		t.Errorf("a failed job carried a digest: %q", got[4].Sum)
	}
	for i := range jobs {
		if i == 4 {
			continue
		}
		if got[i].Err != nil {
			t.Errorf("job %d failed because another job did: %v", i, got[i].Err)
		}
		if got[i].Sum != want[i] {
			t.Errorf("job %d is %s, want %s — the batch stopped early", i, got[i].Sum, want[i])
		}
	}
	if !errors.Is(got[4].Err, os.ErrNotExist) {
		t.Errorf("error does not wrap the cause: %v", got[4].Err)
	}
}

func TestSumAllEmptyBatch(t *testing.T) {
	c := NewCache(filepath.Join(t.TempDir(), CacheFile))
	if got := c.SumAll(nil); len(got) != 0 {
		t.Errorf("SumAll(nil) = %v, want an empty result", got)
	}
	if got := c.SumAll([]Job{}); len(got) != 0 {
		t.Errorf("SumAll([]) = %v, want an empty result", got)
	}
	if err := c.Save(); err != nil {
		t.Fatalf("an empty batch dirtied the cache: %v", err)
	}
}

// TestSumAllErrorIsolatedAtEveryPoolSize is the same guarantee at pool sizes
// small enough to expose it. With one worker, a batch that gives up on the
// first unreadable file abandons every file behind it in the queue; with eight,
// the other seven quietly finish the work and the bug hides.
func TestSumAllErrorIsolatedAtEveryPoolSize(t *testing.T) {
	dir := t.TempDir()
	good := writeFiles(t, dir, 6, 32)
	want := wantSums(t, good)

	// Alternating: absent, present, absent, present, ...
	var jobs []Job
	var expect []string
	for i, g := range good {
		jobs = append(jobs, Job{
			Path: filepath.Join(dir, fmt.Sprintf("gone%d.bin", i)), Size: 1, ModTime: 1,
		}, g)
		expect = append(expect, "", want[i])
	}

	for _, workers := range []int{1, 2, 3, 16} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			c := NewCache(filepath.Join(dir, CacheFile))
			c.workers = workers
			got := c.SumAll(jobs)

			for i := range jobs {
				if expect[i] == "" {
					if got[i].Err == nil {
						t.Errorf("job %d: missing file reported no error", i)
					}
					continue
				}
				if got[i].Err != nil {
					t.Errorf("job %d: %v — an earlier failure took this one with it", i, got[i].Err)
				}
				if got[i].Sum != expect[i] {
					t.Errorf("job %d is %q, want %s — the queue was abandoned",
						i, got[i].Sum, expect[i])
				}
			}
		})
	}
}
