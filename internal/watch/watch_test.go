// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for the watch loop: what wakes it, what it must ignore, and what it
// rebuilds when the tree stops moving.

package watch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/obs"
)

// settle is short enough to keep the suite quick and long enough that one
// editor save does not land in two windows.
const settle = 100 * time.Millisecond

// waitFor bounds how long a test blocks on a filesystem event. Generous: the
// Windows and macOS runners deliver events markedly slower than Linux, and a
// timeout that is tight there is a test that fails for the wrong reason.
const waitFor = 20 * time.Second

func conf(rules []config.Rule) *config.Config {
	return &config.Config{
		Version: 1, IndexBasename: "index", TreeMaxEntries: 1000,
		OnConflict: config.ConflictError, Rules: rules,
	}
}

// tree writes a small indexed tree and returns its root.
func tree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, "bootstrap/bootstrap.sh", "#!/bin/sh\n")
	write(t, root, "bootstrap/linux/apt.list", "deb http://example.invalid stable main\n")
	write(t, root, "docs/intro.md", "# Intro\n")
	return root
}

func write(t *testing.T, root, rel, body string) string {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// started runs a watcher over root and returns the channel its rebuilds are
// announced on. The watcher stops when the test ends.
func started(t *testing.T, c *config.Config, root, out string) <-chan []string {
	t.Helper()
	// Buffered: the loop announces a window before it reads the next event, and
	// a test that is asserting on an earlier window must not stall it.
	settled := make(chan []string, 8)
	ready := make(chan struct{})
	w := &Watcher{
		Config: c, Root: root, Out: out, Log: obs.Discard(), Settle: settle,
		Rebuild: func(string) error { return nil },
		Ready:   ready,
		Settled: settled,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Run: %v", err)
		}
	})

	// The watcher registers the whole tree before it accepts an event, so a
	// test that writes immediately would write before anything is watching.
	select {
	case <-ready:
	case <-time.After(waitFor):
		t.Fatal("the watcher never started")
	}
	return settled
}

// next reads one settle window's scopes, or fails.
func next(t *testing.T, ch <-chan []string) []string {
	t.Helper()
	select {
	case scopes := <-ch:
		return scopes
	case <-time.After(waitFor):
		t.Fatal("no rebuild within the timeout")
		return nil
	}
}

// quiet asserts that nothing is rebuilt: the events a window saw were all ones
// the watcher had to discard.
func quiet(t *testing.T, ch <-chan []string) {
	t.Helper()
	select {
	case scopes := <-ch:
		t.Fatalf("rebuilt %v; expected the change to be ignored", scopes)
	case <-time.After(15 * settle):
	}
}

func TestWatchRebuildsTheDirectoryThatChanged(t *testing.T) {
	root, out := tree(t), t.TempDir()
	ch := started(t, conf(nil), root, out)

	write(t, root, "docs/second.md", "# Second\n")

	if got := next(t, ch); !reflect.DeepEqual(got, []string{"docs"}) {
		t.Errorf("rebuilt %v, want [docs]", got)
	}
}

// One window, several directories: each is rebuilt once, and neither is widened
// to the root.
func TestWatchRebuildsEveryChangedDirectory(t *testing.T) {
	root, out := tree(t), t.TempDir()
	ch := started(t, conf(nil), root, out)

	write(t, root, "docs/second.md", "x")
	write(t, root, "bootstrap/extra.sh", "x")

	// Both writes normally land in one window; a stalled runner can split them
	// across two. Either is correct — what would be wrong is a directory never
	// rebuilt, or one widened to the root.
	got := union(t, ch, 2)
	want := []string{"bootstrap", "docs"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rebuilt %v, want %v", got, want)
	}
}

// union reads settle windows until it has seen n distinct scopes, and returns
// them sorted.
func union(t *testing.T, ch <-chan []string, n int) []string {
	t.Helper()
	seen := map[string]bool{}
	for len(seen) < n {
		for _, s := range next(t, ch) {
			seen[s] = true
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// A directory created after the watch started has to be registered as it
// appears. Nothing else reports what is written inside it, and the rebuild that
// covers the parent does not run again for a change one level down.
func TestWatchFollowsDirectoriesCreatedAfterItStarted(t *testing.T) {
	root, out := tree(t), t.TempDir()
	ch := started(t, conf(nil), root, out)

	if err := os.MkdirAll(filepath.Join(root, "docs", "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := next(t, ch); !reflect.DeepEqual(got, []string{"docs"}) {
		t.Errorf("rebuilt %v, want [docs] for the new directory", got)
	}

	write(t, root, "docs/api/v2.md", "# v2\n")
	if got := next(t, ch); !reflect.DeepEqual(got, []string{"docs/api"}) {
		t.Errorf("rebuilt %v, want [docs/api]; the new directory was not watched", got)
	}
}

// The failure that makes a watcher unusable: cairn writing its index into the
// tree it indexes, and its own writes waking it to write them again.
func TestWatchIgnoresItsOwnOutput(t *testing.T) {
	root := tree(t)
	// out == root is the mirror deployment: one directory that rsyncs whole.
	ch := started(t, conf(nil), root, root)

	write(t, root, "docs/index.json", `{"entries":[]}`)
	write(t, root, "docs/index.html", "<html></html>")
	write(t, root, "SHA256SUMS", "")

	quiet(t, ch)
}

func TestWatchIgnoresHiddenDirectories(t *testing.T) {
	root, out := tree(t), t.TempDir()
	// Present before the watch starts, so this asserts the enumeration skipped
	// it rather than that the event was filtered afterwards.
	write(t, root, ".git/objects/ab/cdef", "x")
	ch := started(t, conf(nil), root, out)

	write(t, root, ".git/objects/ab/beef", "x")
	write(t, root, ".DS_Store", "x")

	quiet(t, ch)
}

// A build writing into a subdirectory of the tree it indexes must not wake the
// watcher either, and that subtree is skipped whole rather than by name.
func TestWatchIgnoresAnOutputDirectoryInsideTheTree(t *testing.T) {
	root := tree(t)
	out := filepath.Join(root, "_index")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	ch := started(t, conf(nil), root, out)

	write(t, root, "_index/docs/anything.json", "{}")

	quiet(t, ch)
}

// A rebuild that fails must not stop the watch. The operator is told which
// scope failed and the next change tries again; a watcher that exited would
// leave a tree reporting nothing, which is the failure it exists to prevent.
func TestSettledContinuesAfterAFailedRebuild(t *testing.T) {
	root := tree(t)
	var tried []string
	settled := make(chan []string, 1)
	w := &Watcher{
		Config: conf(nil), Root: root, Out: t.TempDir(), Log: obs.Discard(),
		Rebuild: func(scope string) error {
			tried = append(tried, scope)
			if scope == "bootstrap" {
				return errors.New("emit refused")
			}
			return nil
		},
		Settled: settled,
	}

	w.settled(map[string]bool{"bootstrap": true, "docs": true})

	want := []string{"bootstrap", "docs"}
	if !reflect.DeepEqual(tried, want) {
		t.Errorf("attempted %v, want %v", tried, want)
	}
	if got := <-settled; !reflect.DeepEqual(got, want) {
		t.Errorf("announced %v, want %v", got, want)
	}
}

// An empty window rebuilds nothing. Every event in it was one the watcher had
// to discard, and a rebuild of the root is not the answer to no change at all.
func TestSettledDoesNothingWithNothingPending(t *testing.T) {
	w := &Watcher{
		Config: conf(nil), Root: t.TempDir(), Out: t.TempDir(), Log: obs.Discard(),
		Rebuild: func(string) error {
			t.Error("rebuilt on an empty window")
			return nil
		},
	}
	w.settled(map[string]bool{})
}

// A change to the root's own directory rebuilds the root listing.
func TestAcceptScopesATopLevelChangeToTheRoot(t *testing.T) {
	root := tree(t)
	w := &Watcher{Config: conf(nil), Root: root, Out: t.TempDir(), Log: obs.Discard()}
	w.filter = NewFilter(w.Config, root, w.Out, w.Log)

	pending := map[string]bool{}
	ev := fsnotify.Event{Name: filepath.Join(root, "README.md"), Op: fsnotify.Create}
	if !w.accept(ev, pending) {
		t.Fatal("a new file at the root was discarded")
	}
	if !pending["."] {
		t.Errorf("pending = %v, want the root", pending)
	}
}

// A tree that cannot be registered is a refusal, with the directory named. The
// alternative is a watcher that runs having registered nothing.
func TestRunFailsWhenTheTreeCannotBeWatched(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-there")
	w := &Watcher{
		Config: conf(nil), Root: missing, Out: t.TempDir(), Log: obs.Discard(),
		Rebuild: func(string) error { return nil },
	}
	err := w.Run(context.Background())
	if err == nil {
		t.Fatal("watching a missing tree must be an error")
	}
	if !strings.Contains(err.Error(), "not-there") {
		t.Errorf("error %q does not name the directory", err)
	}
}

// Settle has a default, so a caller that sets only the paths gets a watcher
// that debounces rather than one that rebuilds on every event.
func TestRunDefaultsTheSettleWindow(t *testing.T) {
	root := tree(t)
	ready := make(chan struct{})
	w := &Watcher{
		Config: conf(nil), Root: root, Out: t.TempDir(), Log: obs.Discard(),
		Rebuild: func(string) error { return nil },
		Ready:   ready,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	select {
	case <-ready:
	case <-time.After(waitFor):
		t.Fatal("the watcher never started")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
	if w.Settle != DefaultSettle {
		t.Errorf("Settle = %v, want %v", w.Settle, DefaultSettle)
	}
}

// The budget is checked before anything is registered, and a tree that does not
// fit is refused. Registering what fits and starting anyway would report some
// changes and say nowhere which ones were dropped.
func TestRunRefusesATreeThatDoesNotFit(t *testing.T) {
	full := &BudgetError{Need: 1_000_000, Have: 256, Limit: "open files"}
	original := reserve
	reserve = func(Plan) error { return full }
	t.Cleanup(func() { reserve = original })

	root := tree(t)
	w := &Watcher{
		Config: conf(nil), Root: root, Out: t.TempDir(), Log: obs.Discard(),
		Rebuild: func(string) error {
			t.Error("rebuilt a tree it refused to watch")
			return nil
		},
		Ready: make(chan struct{}),
	}
	// Bounded, so a Run that skipped the budget check and started watching
	// fails on the assertion rather than blocking the suite.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := w.Run(ctx); !errors.Is(err, error(full)) {
		t.Fatalf("Run = %v, want the budget refusal", err)
	}
	select {
	case <-w.Ready:
		t.Error("announced itself ready without watching")
	default:
	}
}

// Windows reports a change inside a directory on that directory's watch and on
// its parent's, and the parent's copy names only the directory. Acting on it
// scopes the rebuild to the parent — on a deep tree, most of it — and cannot
// tell cairn's own output from content, which in a mirror is a rebuild that
// wakes itself. The child's copy says everything the parent's does.
func TestRedundantDropsAParentsCopyOfAChildsChange(t *testing.T) {
	root := tree(t)
	dir := filepath.Join(root, "docs")
	file := filepath.Join(root, "docs", "intro.md")

	cases := []struct {
		name string
		ev   fsnotify.Event
		want bool
	}{
		{"a write naming a directory", fsnotify.Event{Name: dir, Op: fsnotify.Write}, true},
		{"a chmod naming a directory", fsnotify.Event{Name: dir, Op: fsnotify.Chmod}, true},
		// Everything else about a directory changes what its parent lists, and
		// no other event reports it.
		{"a directory created", fsnotify.Event{Name: dir, Op: fsnotify.Create}, false},
		{"a directory removed", fsnotify.Event{Name: dir, Op: fsnotify.Remove}, false},
		{"a directory renamed", fsnotify.Event{Name: dir, Op: fsnotify.Rename}, false},
		// A file's write is the event that carries the change.
		{"a write naming a file", fsnotify.Event{Name: file, Op: fsnotify.Write}, false},
	}
	for _, tc := range cases {
		if got := redundant(tc.ev, tc.ev.Name); got != tc.want {
			t.Errorf("redundant(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The same, through accept: a parent's copy must leave nothing pending, or the
// rebuild is scoped to the parent of the directory that actually changed.
func TestAcceptDropsAParentsCopyOfAChildsChange(t *testing.T) {
	root := tree(t)
	w := &Watcher{Config: conf(nil), Root: root, Out: root, Log: obs.Discard()}
	w.filter = NewFilter(w.Config, root, w.Out, w.Log)

	pending := map[string]bool{}
	ev := fsnotify.Event{Name: filepath.Join(root, "docs"), Op: fsnotify.Write}
	if w.accept(ev, pending) {
		t.Error("a parent's copy of a child's change was acted on")
	}
	if len(pending) != 0 {
		t.Errorf("pending = %v, want nothing", pending)
	}
}
