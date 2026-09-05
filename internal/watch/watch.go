// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Package watch rebuilds the directories a change touched, as it happens.
package watch

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"
	"sort"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/livingstaccato/cairn/internal/config"
)

// DefaultSettle is how long the watcher waits for a tree to stop moving before
// it rebuilds.
//
// An editor saving a file writes, renames and chmods it; an unpacked archive
// arrives as thousands of creates. Rebuilding on the first event of either
// would rebuild against a half-written tree and then rebuild again. A quarter
// of a second is below what a person notices and above what one save costs.
const DefaultSettle = 250 * time.Millisecond

// reserve is the budget check, indirected so a test can assert that Run refuses
// a tree the platform cannot hold. The real one is platform-specific and cannot
// be made to fail on every platform the watcher has to work on.
var reserve = Reserve

// Rebuild runs one scoped rebuild. The watcher owns when it is called and with
// what scope; what it does is the caller's.
type Rebuild func(scope string) error

// Watcher registers a tree with the platform's change notification and calls
// Rebuild for the directories that changed.
type Watcher struct {
	Config  *config.Config
	Root    string
	Out     string
	Log     *slog.Logger
	Settle  time.Duration
	Rebuild Rebuild

	// Ready, when set, is closed once the whole tree is registered and events
	// are being read. A caller that writes into the tree itself waits on it
	// rather than guessing how long registration takes.
	Ready chan struct{}
	// Settled, when set, receives the scopes of every rebuild a settle window
	// produced, after they have run. Tests wait on it rather than on a clock.
	Settled chan<- []string

	filter *Filter
	fsw    *fsnotify.Watcher
}

// Run watches until ctx is cancelled.
//
// The whole tree is registered before the first event is accepted, and the
// budget is checked before any of it is registered. A watcher that starts with
// half a tree registered reports half the changes and says so nowhere; the
// index it leaves behind is wrong in a way no later run detects.
func (w *Watcher) Run(ctx context.Context) error {
	if w.Settle <= 0 {
		w.Settle = DefaultSettle
	}
	w.filter = NewFilter(w.Config, w.Root, w.Out, w.Log)

	plan := Enumerate(w.Config, w.Root, w.Out, w.Log)
	if err := reserve(plan); err != nil {
		return err
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	w.fsw = fsw
	defer func() {
		if err := w.fsw.Close(); err != nil {
			w.Log.Warn("could not close the watcher", "err", err)
		}
	}()

	for _, dir := range plan.Dirs {
		if err := w.fsw.Add(dir); err != nil {
			return fmt.Errorf("watch %s: %w", dir, err)
		}
	}
	w.Log.Info("watching", "directories", len(plan.Dirs), "files", plan.Files,
		"settle", w.Settle.String())
	if w.Ready != nil {
		close(w.Ready)
	}

	return w.loop(ctx)
}

// loop collects events into a settle window and rebuilds when it closes.
func (w *Watcher) loop(ctx context.Context) error {
	pending := map[string]bool{}
	// Stopped and drained: a timer is only running while changes are pending,
	// so an idle watcher wakes for nothing.
	timer := time.NewTimer(w.Settle)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case ev, ok := <-w.fsw.Events:
			if !ok {
				return nil
			}
			if !w.accept(ev, pending) {
				continue
			}
			// Each accepted event restarts the window: the tree is still
			// moving, so rebuilding now would rebuild against a partial state.
			w.restart(timer)

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return nil
			}
			// A dropped event is a listing that may now be stale, which the
			// operator has to know about; it is not a reason to stop watching
			// the rest of the tree.
			w.Log.Warn("the watcher dropped events; some output may be stale", "err", err)

		case <-timer.C:
			w.settled(pending)
			pending = map[string]bool{}
		}
	}
}

// restart begins the settle window again, draining a firing this loop has not
// read yet. Without the drain the window would close immediately on the stale
// value the timer left behind.
func (w *Watcher) restart(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(w.Settle)
}

// accept records what one event changed, and reports whether it changed
// anything. A new directory is registered here rather than at the next rebuild:
// between the two, everything created inside it would go unreported.
func (w *Watcher) accept(ev fsnotify.Event, pending map[string]bool) bool {
	rel, ok := w.filter.Rel(ev.Name)
	if !ok || w.filter.Ignore(ev.Name) {
		return false
	}
	if redundant(ev, ev.Name) {
		return false
	}
	if ev.Has(fsnotify.Create) {
		w.addTree(ev.Name)
	}
	// The directory that has to be rebuilt is the one holding the entry that
	// changed: its listing names the entry, and its count and mtime moved.
	pending[path.Dir(rel)] = true
	return true
}

// redundant reports whether an event repeats what the watch on the directory
// that changed already reports in full.
//
// Windows announces a change inside a directory twice: once on that
// directory's own watch, naming the entry, and once on its parent's, naming
// the directory. Acting on both is wrong twice over. The parent's copy does
// not say what moved, so cairn's own output cannot be recognised and discarded
// — in a mirror the index writes itself back into the tree and wakes the
// watcher that asked for them. And it scopes the rebuild to the parent, which
// on a deep tree is most of it, so every scoped rebuild widens to something
// close to a full one.
//
// Only a plain write is dropped. A directory that appeared, went away or was
// renamed changes what its parent lists, and nothing else reports it.
// Permissions are not dropped either — no listing carries a mode, so a chmod on
// a directory changes no output.
func redundant(ev fsnotify.Event, absPath string) bool {
	if ev.Has(fsnotify.Create) || ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename) {
		return false
	}
	fi, err := os.Lstat(absPath)
	return err == nil && fi.IsDir()
}

// addTree registers a directory that has just appeared, and everything under
// it.
//
// A directory copied in whole exists with its contents before the create event
// for it is read, so registering only the directory would miss every file
// already inside. Failures are logged rather than returned: the tree is being
// written while it is read, so a path that vanishes between the two is normal.
func (w *Watcher) addTree(absPath string) {
	fi, err := os.Lstat(absPath)
	if err != nil || !fi.IsDir() {
		return
	}
	rel, ok := w.filter.Rel(absPath)
	if !ok {
		return
	}
	for _, dir := range EnumerateUnder(w.Config, w.Root, w.Out, rel, w.Log).Dirs {
		if err := w.fsw.Add(dir); err != nil {
			w.Log.Warn("could not watch a new directory", "path", dir, "err", err)
		}
	}
}

// settled rebuilds what the closed window changed.
func (w *Watcher) settled(pending map[string]bool) {
	if len(pending) == 0 {
		return
	}
	dirs := make([]string, 0, len(pending))
	for d := range pending {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	scopes := Coalesce(w.Config, w.Root, dirs, w.Log)
	for _, scope := range scopes {
		start := time.Now()
		if err := w.Rebuild(scope); err != nil {
			// The next change rebuilds again. Stopping here would leave the
			// operator watching a tree that reports nothing.
			w.Log.Error("rebuild failed", "scope", scope, "err", err)
			continue
		}
		w.Log.Info("rebuilt", "scope", scope, "took", time.Since(start).Round(time.Millisecond).String())
	}
	if w.Settled != nil {
		w.Settled <- scopes
	}
}
