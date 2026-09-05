// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package hash

import (
	"runtime"
	"sync"
)

// Job is one file to digest: the path, plus the stat fields that decide whether
// the digest already recorded for it is still good.
type Job struct {
	Path    string
	Size    int64
	ModTime int64
}

// Result is one digest, or the reason there is none.
//
// The error rides in the result rather than ending the batch because a file
// that cannot be read is not a failed build: the listing is still correct, it
// simply cannot be verified. The caller warns about that one file and keeps the
// digests of every other.
type Result struct {
	Sum string
	Err error
}

// MaxWorkers caps the pool SumAll sizes for itself, whatever the machine
// reports.
//
// Parallelism here is bounded, not unbounded, because a batch is a whole
// directory: fifty thousand entries would be fifty thousand simultaneous opens,
// and this process has a descriptor limit that internal/watch already budgets
// against.
//
// The number is measured, not guessed. BenchmarkSumAllWorkers sweeps the bound
// over 2,048 files on a 24-core machine and throughput peaks at eight workers
// (~1.1 GB/s) and then falls back by more than a tenth at sixteen and above:
// past that point the extra goroutines are queueing on the filesystem and
// competing for the same cores, which costs throughput rather than buying it.
// Eight is also cheap against the descriptor budget, which is the other thing
// this constant is protecting.
const MaxWorkers = 8

// workersPerProc oversubscribes the cores on purpose. Digesting a file is a
// read syscall and then SHA-256 over what came back, so a worker spends a good
// share of its life blocked on the filesystem rather than hashing; one worker
// per core would leave the CPU idle for exactly that share. Two per core keeps
// a core fed while its neighbour waits, and reaches the measured ceiling on
// anything with four cores or more.
const workersPerProc = 2

// SumAll digests every job and returns the results positionally: out[i] is the
// digest of jobs[i], whatever order the files actually finished in. The caller
// assigns digests back to listing entries by index, so any reordering here
// would attach the wrong checksum to the wrong file.
//
// The batch runs in three phases — look the whole batch up in the cache, hash
// the misses on a bounded pool, write the new records back — rather than
// locking around each file. Two locked passes on the calling goroutine cost two
// acquisitions per batch instead of two per file, and the workers, which are
// the part that runs concurrently, never touch the cache at all. The lock still
// exists (see Cache) because two callers may run batches through one cache; it
// just never appears on the hot path.
func (c *Cache) SumAll(jobs []Job) []Result {
	out := make([]Result, len(jobs))
	todo := c.lookupAll(jobs, out)
	if len(todo) == 0 {
		return out
	}
	digestAll(jobs, todo, out, c.workerCount())
	c.storeAll(jobs, todo, out)
	return out
}

// lookupAll fills in every result the cache already knows and returns the
// indices of the jobs that still need reading.
func (c *Cache) lookupAll(jobs []Job, out []Result) []int {
	c.mu.Lock()
	defer c.mu.Unlock()

	var todo []int
	for i := range jobs {
		r, ok := c.entries[jobs[i].Path]
		if ok && r.matches(jobs[i].Size, jobs[i].ModTime) {
			c.hits++
			r.touched = true
			c.entries[jobs[i].Path] = r
			out[i].Sum = r.Sum
			continue
		}
		todo = append(todo, i)
	}
	return todo
}

// digestAll hashes the listed jobs on a bounded pool of workers.
//
// Each worker writes only out[i] for the index it drew from the channel, and no
// index is handed out twice, so the shared slice needs no lock: the goroutines
// never touch the same element. Wait then orders every one of those writes
// before the caller reads them back.
func digestAll(jobs []Job, todo []int, out []Result, workers int) {
	// Never more goroutines than there is work for them.
	n := min(workers, len(todo))

	queue := make(chan int)
	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			for i := range queue {
				out[i].Sum, out[i].Err = digestFile(jobs[i].Path)
			}
		}()
	}
	for _, i := range todo {
		queue <- i
	}
	close(queue)
	wg.Wait()
}

// storeAll folds the batch's new digests into the cache in one pass. A job that
// failed records nothing: a cache entry for a file that could not be read would
// be a digest of nothing at all.
func (c *Cache) storeAll(jobs []Job, todo []int, out []Result) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, i := range todo {
		if out[i].Err != nil {
			continue
		}
		c.entries[jobs[i].Path] = record{
			Size: jobs[i].Size, ModUnix: jobs[i].ModTime, Sum: out[i].Sum, touched: true,
		}
		c.dirty = true
	}
}

// workerCount reports how many files this cache will read at once.
func (c *Cache) workerCount() int {
	if c.workers > 0 {
		return c.workers
	}
	return min(max(runtime.GOMAXPROCS(0)*workersPerProc, 1), MaxWorkers)
}
