// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Package build orchestrates one cairndex run: walk, merge metadata, hash, emit.
package build

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/livingstaccato/cairndex/internal/config"
	"github.com/livingstaccato/cairndex/internal/emit"
	"github.com/livingstaccato/cairndex/internal/hash"
	"github.com/livingstaccato/cairndex/internal/walk"
)

// progressInterval throttles how often a build logs how far it has gotten,
// so a mirror of any size gets feedback without a line per directory. A
// build that finishes faster than this — nearly every one — never logs a
// progress line at all, only the final "build complete". 0 disables the
// throttle entirely, which a test sets for a deterministic assertion
// instead of racing wall-clock time against how long a run takes.
var progressInterval = 2 * time.Second

// Result summarizes a run for the caller to report.
type Result struct {
	Dirs    int
	Files   int
	Written []string
	// Pruned lists the outputs a previous run owned that this one did not
	// rewrite, and which have been removed. A dry run reports the same list
	// without having removed anything, which is the whole point of one.
	Pruned    []string
	Protected int
	// Unchanged counts outputs that already held what this run would write.
	Unchanged int
	// Changed lists the outputs whose bytes this run altered, for a deploy that
	// only has to move what moved.
	Changed []string
	// Adopted lists the output paths this run claimed that no previous run had
	// recorded — the conflict check waived, once, on request.
	Adopted []string
	// Forgot counts the hash-cache records this run dropped: digests for files
	// that were under the region it rebuilt and are no longer in the tree. A dry
	// run reports what it would have dropped.
	Forgot int
}

// runner carries the values every step of a build needs.
type runner struct {
	cfg    *config.Config
	root   string
	out    string
	log    *slog.Logger
	cache  *hash.Cache
	writer *emit.Writer
	result *Result
	// outRel is the output directory relative to the indexed root, when it sits
	// inside it. See OutRel: the walk has to skip that subtree, or the build
	// indexes its own output.
	outRel string
	// dry suppresses every change to the filesystem while leaving each decision
	// that leads to one intact.
	dry bool
	// warnedStyled keeps the styled-in-direct-mode diagnostic to one line per
	// run rather than one per directory.
	warnedStyled bool
	// ctx is checked once per directory, in visit. A build has no other loop
	// short enough to make checking more often worth the cost, and long
	// enough — a mirror of any size — that never checking at all left a
	// cancelled context observed only by the process dying under it, taking
	// the manifest save in build's own error path down with it.
	ctx context.Context
	// lastProgress is when reportProgress last logged, seeded to the
	// build's own start time so the interval is measured from there rather
	// than from an unset zero value a first call would otherwise always
	// beat. Runner state rather than a package var: two concurrent Run
	// calls (real in cmd/cairndex's tests) must not throttle each other.
	lastProgress time.Time
	// version is Options.Version, cairndex's own version for the
	// build-info footer banner. Empty turns the banner off regardless of
	// build_info:.
	version string
	// started is when this build began, for the same banner. Distinct from
	// a listing's own Generated, which is the newest entry's mtime — a
	// content-freshness fact, not when the build itself ran.
	started time.Time
}

// buildInfo is what a listing's footer banner shows, or the zero value
// when the operator turned it off with build_info: false, or when the
// caller never set Options.Version — nothing to show is nothing shown,
// not a misleading blank banner.
func (r *runner) buildInfo() emit.BuildInfo {
	if r.version == "" || !r.cfg.ShowBuildInfo() {
		return emit.BuildInfo{}
	}
	return emit.BuildInfo{Version: r.version, Generated: r.started}
}

// reportProgress logs how far the build has gotten, throttled by
// progressInterval so it says something on a mirror large enough for that
// to matter and nothing at all on one finished before the first interval.
func (r *runner) reportProgress() {
	now := time.Now()
	if now.Sub(r.lastProgress) < progressInterval {
		return
	}
	r.lastProgress = now
	r.log.Info("build in progress", "directories", r.result.Dirs, "files", r.result.Files)
}

// Run walks rootDir and writes every configured output under outDir. Warnings
// are logged as they happen rather than accumulated: on a large mirror the
// interesting ones are the early ones, and a caller should not have to wait for
// the walk to finish to see them.
//
// A cancelled ctx stops the walk at the next directory boundary and returns
// ctx.Err(), which RunWith's own error path already treats like any other
// failure: SavePartial claims what was written before the walk stopped, so
// a build cut short by Ctrl-C leaves output a later run can still own rather
// than a mirror on_conflict: error refuses to touch again.
func Run(ctx context.Context, cfg *config.Config, rootDir, outDir string, log *slog.Logger) (*Result, error) {
	return RunWith(ctx, cfg, rootDir, outDir, log, Options{})
}

// RunDry reports what Run would do and changes nothing.
//
// Every decision still runs, against the tree as it stands: what would be
// written, which of those bodies differ from what is on disk, and — the reason
// this exists — which files Prune would delete. Deleting published artifacts is
// the one thing cairndex does that cannot be undone by running it again, and a
// misconfigured out: or a manifest from a different config makes it delete a
// lot of them. Until now the only way to find out was to let it happen.
func RunDry(ctx context.Context, cfg *config.Config, rootDir, outDir string, log *slog.Logger) (*Result, error) {
	return RunWith(ctx, cfg, rootDir, outDir, log, Options{Dry: true})
}

// Options are the departures from a default build that a command line may ask
// for. The zero value is an ordinary build.
type Options struct {
	// Dry reports what a build would do and changes nothing under out:.
	Dry bool
	// Adopt claims output paths that already exist and cairndex does not own,
	// instead of refusing them.
	//
	// This is the way back from a lost or unreadable .cairndex-manifest.json. In
	// that state every file cairndex wrote is a file it no longer claims, and
	// on_conflict: error refuses all of them; the only escapes were deleting the
	// output — impossible where root: and out: are the same directory, since
	// that deletes the artifacts — or on_conflict: skip, which then writes
	// nothing and prunes nothing and freezes the mirror for good.
	//
	// What it claims is exactly the set of paths this build produces that
	// already exist. It never walks out: looking for files that seem generated,
	// so it cannot take one this build does not itself write, and protect: and
	// path containment are checked ahead of it and are not affected.
	Adopt bool
	// Version is cairndex's own version, for the build-info footer banner
	// every listing shows by default — see config.Config.ShowBuildInfo.
	// Empty turns the banner off regardless of build_info:, the same way a
	// library caller who never set it gets no banner rather than a
	// misleading blank one.
	Version string
}

// newRunner builds the fields every entry point needs, so a field added to
// runner later means one call site to update rather than two that can
// silently drift — RunWith and RunScoped used to each build their own
// &runner{} literal, and nothing caught either one forgetting a field the
// other set. What genuinely differs between them — writer, dry — stays
// explicit at each call site, set on the value this returns.
func newRunner(ctx context.Context, cfg *config.Config, rootDir, outDir string, log *slog.Logger, version string, startedAt time.Time) *runner {
	return &runner{
		cfg:     cfg,
		root:    rootDir,
		out:     outDir,
		log:     log,
		cache:   hash.NewCache(filepath.Join(outDir, hash.CacheFile)),
		result:  &Result{},
		outRel:  OutRel(rootDir, outDir),
		ctx:     ctx,
		version: version,
		started: startedAt,
		// The throttle's baseline: the interval is measured from when the
		// build started, not from an unset zero value a first call would
		// otherwise always beat.
		lastProgress: startedAt,
	}
}

// RunWith is Run with the one-run departures a command line asked for.
func RunWith(ctx context.Context, cfg *config.Config, rootDir, outDir string, log *slog.Logger, opts Options) (*Result, error) {
	startedAt := time.Now()
	r := newRunner(ctx, cfg, rootDir, outDir, log, opts.Version, startedAt)
	r.writer = emit.NewWriterWith(cfg, outDir, emit.Options{Dry: opts.Dry, Adopt: opts.Adopt})
	r.dry = opts.Dry
	r.warnAboutTheManifest()

	err := r.safeBuild()
	if err != nil {
		// Claim what this run managed to write before it died. Without this the
		// partial output belongs to nobody, and on_conflict: error refuses every
		// later run until an operator deletes the files by hand — a mirror that
		// cannot be rebuilt without manual cleanup.
		if saveErr := r.writer.SavePartial(); saveErr != nil {
			r.log.Warn("could not record partial output; a retry may report conflicts",
				"path", emit.ManifestFile, "err", saveErr)
		}
	}
	r.result.Written = r.writer.Written()
	r.result.Protected = len(r.writer.Protected())
	r.result.Unchanged = r.writer.Unchanged()
	r.result.Changed = r.writer.Changed()
	r.result.Adopted = r.writer.Adopted()
	return r.result, err
}

// safeBuild runs build and turns a panic into an error, so a panic mid-walk
// takes the same path out as any other build failure.
//
// Without this a panic propagates straight out of RunWith and SavePartial
// never runs: the files this run already wrote are left claimed by nobody,
// and on_conflict: error then refuses all of them on the next run — the
// "mirror that cannot be rebuilt without manual cleanup" SavePartial exists to
// prevent, reached by a path that skipped it.
func (r *runner) safeBuild() (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	return buildStep(r)
}

// buildStep runs one build. A var, like atomicfile's rename, because a panic
// mid-build is a failure this package otherwise has no way to produce in a
// test.
var buildStep = (*runner).build

// build runs the walk, the prune and the manifest save for a successful run.
func (r *runner) build() error {
	if err := r.visit("."); err != nil {
		return err
	}
	r.forget(r.root)
	r.saveCache()
	// Anything the previous run wrote and this one did not is stale: a removed
	// file's digest, or a whole listing for a directory that no longer exists.
	pruned, err := r.writer.Prune()
	if err != nil {
		return err
	}
	r.reportPruned(pruned)

	// The manifest records what this run owns. Without it the next run cannot
	// tell its own output from content that was already there, and refuses to
	// overwrite either.
	return r.writer.Save()
}

// warnAboutTheManifest says so, once, when the previous run's manifest could
// not be read.
//
// The run continues: cairndex owning nothing is a recoverable state, and refusing
// to start would be worse than the conflict errors that follow. But those
// errors name a path and say it already exists, which is true and useless —
// the file exists because cairndex wrote it, and the reason it is now a conflict
// is upstream of anything the path can tell you.
func (r *runner) warnAboutTheManifest() {
	if err := r.writer.ManifestError(); err != nil {
		r.log.Warn("the manifest could not be read, so cairndex claims none of the output "+
			"already in place; expect conflicts, and nothing will be pruned",
			"path", emit.ManifestFile, "err", err)
	}
}

// saveCache writes the hash cache back, unless this run is not writing.
//
// A dry run leaves it alone even though the cache is only an optimization: it
// is still a file under the output directory, and "--dry-run changed something"
// is not a sentence that should ever be true.
func (r *runner) saveCache() {
	if r.dry {
		return
	}
	if err := r.cache.Save(); err != nil {
		r.log.Warn("could not save the hash cache; the next run re-hashes",
			"path", hash.CacheFile, "err", err)
	}
}

// forget drops the cache records for files that have left the region this run
// rebuilt, and records how many.
//
// Placed here, on the success path, rather than beside SavePartial: a build that
// died partway never reached the rest of its scope, so "this run did not consult
// it" says nothing about whether the file is still there. Sweeping then would
// discard digests that are perfectly good and re-hash a mirror that may be
// terabytes.
//
// The prefix is the region the run was authoritative for — see Cache.Sweep. A
// dry run counts and drops nothing: the cache is a file under out:, and
// "--dry-run changed something" is not a sentence that should ever be true.
func (r *runner) forget(prefix string) {
	if r.dry {
		r.result.Forgot = r.cache.Stale(prefix)
		return
	}
	r.result.Forgot = r.cache.Sweep(prefix)
}

// reportPruned logs and records what a prune removed, or would have.
func (r *runner) reportPruned(pruned []string) {
	msg := "removed stale output"
	if r.dry {
		msg = "would remove stale output"
	}
	for _, p := range pruned {
		r.log.Info(msg, "path", p)
	}
	r.result.Pruned = pruned
}

// warn logs each walk warning.
func (r *runner) warn(ws []walk.Warning) {
	for _, w := range ws {
		r.log.Warn("skipped an entry", "path", w.Path, "err", w.Err)
	}
}
