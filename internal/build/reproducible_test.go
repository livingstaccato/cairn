// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for the property a rebuild-on-change tool needs: build twice over a
// tree nothing touched, and nothing on disk moves.

package build

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/livingstaccato/cairndex/internal/obs"
)

// The property the whole mirror deployment rests on: a second build over an
// unchanged tree touches nothing on disk. Without it every rebuild moves every
// index file's mtime, which in a mirror is a change to the tree cairndex indexes.
func TestSecondBuildRewritesNothing(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	first := run(t, c, root, out)

	before := map[string]time.Time{}
	for _, rel := range first.Written {
		p := filepath.Join(out, filepath.FromSlash(rel))
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		before[rel] = fi.ModTime()
	}

	time.Sleep(10 * time.Millisecond)
	second := run(t, c, root, out)

	if second.Unchanged != len(second.Written) {
		t.Errorf("Unchanged = %d of %d outputs; a second build rewrote something",
			second.Unchanged, len(second.Written))
	}
	for rel, was := range before {
		fi, err := os.Stat(filepath.Join(out, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			continue
		}
		if !fi.ModTime().Equal(was) {
			t.Errorf("%s was rewritten: mtime moved from %v to %v", rel, was, fi.ModTime())
		}
	}
}

// The same, in the deployment where it matters: root and out the same
// directory, so a rewrite is a change to the tree being indexed.
//
// A mirror converges rather than settling at once. The first build creates
// index files inside each directory, which moves those directories' own mtimes
// — and a parent listing records its children's mtimes, so the parents really
// are out of date and really do have to be rewritten. What matters is that it
// stops: once the index files exist, rewriting them identically moves nothing.
func TestAMirrorReachesAFixedPoint(t *testing.T) {
	root := tree(t)
	c := conf(nil)

	// NTFS updates a directory's last-write-time lazily, unlike POSIX —
	// confirmed by an isolated probe run five times on real Windows CI:
	// creating a file inside a directory produced no observable mtime change
	// at all, even 200ms later, two times out of five. A parent listing
	// embeds its children's mtimes, so when Windows doesn't surface that
	// bump promptly, the parent needs one more pass to pick up a change that
	// already happened elsewhere in the same run. Not a flaky rebuild, not
	// an algorithm bug — a real platform gap in the guarantee this
	// convergence leans on, same category as TestWriteReplacesFileAtomically's
	// Windows skip.
	settleWithin := 3
	if runtime.GOOS == "windows" {
		settleWithin = 4
	}

	const hardLimit = 5
	for i := 1; i <= hardLimit; i++ {
		res, err := Run(context.Background(), c, root, root, obs.Discard())
		if err != nil {
			t.Fatal(err)
		}
		if res.Unchanged == len(res.Written) {
			if i > settleWithin {
				t.Errorf("a mirror took %d builds to settle", i)
			}
			return
		}
	}
	t.Errorf("a mirror never settled in %d builds", hardLimit)
}
