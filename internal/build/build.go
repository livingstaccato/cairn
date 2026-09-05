// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Package build orchestrates one cairn run: walk, merge metadata, hash, emit.
package build

import (
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/emit"
	"github.com/livingstaccato/cairn/internal/hash"
	"github.com/livingstaccato/cairn/internal/meta"
	"github.com/livingstaccato/cairn/internal/model"
	"github.com/livingstaccato/cairn/internal/walk"
)

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
}

// Run walks rootDir and writes every configured output under outDir. Warnings
// are logged as they happen rather than accumulated: on a large mirror the
// interesting ones are the early ones, and a caller should not have to wait for
// the walk to finish to see them.
func Run(cfg *config.Config, rootDir, outDir string, log *slog.Logger) (*Result, error) {
	return RunWith(cfg, rootDir, outDir, log, Options{})
}

// RunDry reports what Run would do and changes nothing.
//
// Every decision still runs, against the tree as it stands: what would be
// written, which of those bodies differ from what is on disk, and — the reason
// this exists — which files Prune would delete. Deleting published artifacts is
// the one thing cairn does that cannot be undone by running it again, and a
// misconfigured out: or a manifest from a different config makes it delete a
// lot of them. Until now the only way to find out was to let it happen.
func RunDry(cfg *config.Config, rootDir, outDir string, log *slog.Logger) (*Result, error) {
	return RunWith(cfg, rootDir, outDir, log, Options{Dry: true})
}

// Options are the departures from a default build that a command line may ask
// for. The zero value is an ordinary build.
type Options struct {
	// Dry reports what a build would do and changes nothing under out:.
	Dry bool
	// Adopt claims output paths that already exist and cairn does not own,
	// instead of refusing them.
	//
	// This is the way back from a lost or unreadable .cairn-manifest.json. In
	// that state every file cairn wrote is a file it no longer claims, and
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
}

// RunWith is Run with the one-run departures a command line asked for.
func RunWith(cfg *config.Config, rootDir, outDir string, log *slog.Logger, opts Options) (*Result, error) {
	r := &runner{
		cfg:    cfg,
		root:   rootDir,
		out:    outDir,
		log:    log,
		cache:  hash.NewCache(filepath.Join(outDir, hash.CacheFile)),
		writer: emit.NewWriterWith(cfg, outDir, emit.Options{Dry: opts.Dry, Adopt: opts.Adopt}),
		result: &Result{},
		outRel: OutRel(rootDir, outDir),
		dry:    opts.Dry,
	}
	r.warnAboutTheManifest()

	err := r.build()
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
// The run continues: cairn owning nothing is a recoverable state, and refusing
// to start would be worse than the conflict errors that follow. But those
// errors name a path and say it already exists, which is true and useless —
// the file exists because cairn wrote it, and the reason it is now a conflict
// is upstream of anything the path can tell you.
func (r *runner) warnAboutTheManifest() {
	if err := r.writer.ManifestError(); err != nil {
		r.log.Warn("the manifest could not be read, so cairn claims none of the output "+
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

// visit processes one directory and recurses into its children.
func (r *runner) visit(relDir string) error {
	absDir := filepath.Join(r.root, filepath.FromSlash(relDir))
	s := r.cfg.Resolve(relDir, r.dirOverride(absDir))

	entries, err := r.collect(relDir, absDir, s)
	if err != nil {
		return err
	}

	prose, err := meta.Prose(absDir)
	if err != nil {
		return err
	}

	src, err := meta.Source(absDir)
	if err != nil {
		return err
	}
	if err := r.emitFor(relDir, r.cfg.IndexBasename, r.listing(relDir, entries), s, prose, src); err != nil {
		return err
	}
	if s.Recursive {
		if err := r.emitTree(relDir, s, prose, src); err != nil {
			return err
		}
	}

	r.result.Dirs++
	return r.recurse(relDir, entries)
}

// collect walks one directory, merges its authored metadata, and hashes what
// the settings ask for.
func (r *runner) collect(relDir, absDir string, s config.Settings) ([]model.Entry, error) {
	entries, warns, err := r.produce(relDir, absDir, s)
	if err != nil {
		return nil, err
	}
	r.warn(warns)

	entries = r.dropGenerated(relDir, entries, s)

	m, mwarns, err := meta.Load(absDir)
	if err != nil {
		return nil, err
	}
	r.warn(mwarns)
	entries = meta.Apply(entries, m)
	// Weight comes from the sidecar, so the walker's order predates it.
	walk.Sort(entries, s)

	if s.Checksum == config.ChecksumSHA256 {
		r.hashEntries(absDir, entries)
	}
	return entries, nil
}

// produce lists a directory using the configured source. fs walks the disk,
// pages reads an existing Hugo content section, and manifest reads an authored
// list for contents that are not on disk at build time.
func (r *runner) produce(relDir, absDir string, s config.Settings) ([]model.Entry, []walk.Warning, error) {
	switch s.Source {
	case config.SourcePages:
		return walk.Pages(r.root, relDir, s)
	case config.SourceManifest:
		return walk.Manifest(absDir, s)
	default:
		return walk.Dir(r.root, relDir, s)
	}
}

// dropGenerated removes cairn's own output from a listing.
//
// Required for the deployment that matters most: writing index files into the
// artifact tree itself, so a mirror is one directory that rsyncs whole and
// verifies in place with sha256sum -c. Without this the second run lists the
// first run's index.json, SHA256SUMS covers files that change on every run, and
// the build never reaches a fixed point.
//
// Excluding is safe for a path cairn would actually write: if such a file
// already exists and cairn did not create it, emit.Writer refuses the whole
// build, so there is no case where this hides someone's own file from its own
// listing. A protected path is the exception — cairn writes nothing there, so a
// file of that name belongs to whoever put it there and must stay listed.
func (r *runner) dropGenerated(relDir string, entries []model.Entry, s config.Settings) []model.Entry {
	keep := r.treeFilter()
	out := entries[:0:0]
	for _, e := range entries {
		if keep(relDir, e) {
			out = append(out, e)
		}
	}
	return out
}

// treeFilter returns the rule for what cairn's own output is, for the two walks
// that have to agree about it.
//
// One function because two callers must agree, the same reason OutRel is one
// function. The per-directory walk reaches it through dropGenerated and the
// recursive walk through walk.Tree, and they disagreed once: the exclusion was
// applied on the collect side only, so tree.json and the search index built from
// it published the output directory and every generated file as content, and a
// rebuild that changed nothing still rewrote tree.json.
//
// The generated-name set is built once and captured rather than rebuilt per
// entry: a package pool directory is thousands of entries and this is called for
// every one of them.
func (r *runner) treeFilter() walk.Filter {
	skip := r.generatedNames()
	return func(relDir string, e model.Entry) bool {
		if r.isOutputDir(relDir, e) {
			return false
		}
		generated := !e.IsDir && skip[e.Name] &&
			!r.cfg.IsProtected(path.Join(relDir, e.Name))
		return !generated
	}
}

// isOutputDir reports whether this entry is the output directory, sitting
// inside the tree being indexed.
//
// Dropping it here covers both halves at once: entries is what the listing
// renders and what recurse descends into, so the directory is neither named nor
// walked. Leaving it listed would publish a link to a page that is never
// written, and walking it made every build index the previous build's output
// one level deeper.
//
// This is the same exclusion cairn already applies to its own generated
// filenames, one level up: a directory cairn fills is no more part of the tree
// it describes than a file cairn wrote.
func (r *runner) isOutputDir(relDir string, e model.Entry) bool {
	if !e.IsDir || r.outRel == "" {
		return false
	}
	return path.Join(relDir, e.Name) == r.outRel
}

// generatedNames is every filename cairn writes into one directory.
func (r *runner) generatedNames() map[string]bool {
	return GeneratedNames(r.cfg)
}

// hashEntries fills SHA256 for every file in the listing. A file that cannot be
// hashed warns and is left without a digest rather than failing the build: the
// listing is still correct, it just cannot be verified.
func (r *runner) hashEntries(absDir string, entries []model.Entry) {
	// Collected first, then hashed together. One directory of a package pool is
	// thousands of files, and digesting them one at a time leaves every core but
	// one idle waiting on a read that has already returned.
	jobs := make([]hash.Job, 0, len(entries))
	at := make([]int, 0, len(entries))
	for i := range entries {
		if entries[i].IsDir {
			continue
		}
		jobs = append(jobs, hash.Job{
			Path:    filepath.Join(absDir, entries[i].Name),
			Size:    entries[i].Size,
			ModTime: entries[i].ModTime.Unix(),
		})
		at = append(at, i)
	}

	for j, res := range r.cache.SumAll(jobs) {
		i := at[j]
		if res.Err != nil {
			// A file that cannot be hashed is left without a digest rather than
			// failing the build: the listing is still correct, it just cannot be
			// verified.
			r.log.Warn("could not hash file; omitted from SHA256SUMS",
				"path", entries[i].Path, "err", res.Err)
			continue
		}
		entries[i].SHA256 = res.Sum
	}
}

// recurse descends into each subdirectory of a completed listing.
func (r *runner) recurse(relDir string, entries []model.Entry) error {
	for _, e := range entries {
		if !e.IsDir {
			r.result.Files++
			continue
		}
		child := e.Name
		if relDir != "." {
			child = path.Join(relDir, e.Name)
		}
		if err := r.visit(child); err != nil {
			return err
		}
	}
	return nil
}

// listing wraps entries in the envelope the emitters write.
func (r *runner) listing(relDir string, entries []model.Entry) model.Listing {
	p := "/" + relDir
	if relDir == "." {
		p = "/"
	}
	p = strings.TrimSuffix(r.cfg.BasePath+p, "/")
	if p == "" {
		p = "/"
	}
	return model.Listing{
		Path: p, Generated: r.newest(relDir, entries), Count: len(entries),
		Entries: r.rebase(entries),
	}
}

// newest is the most recent modification time in a listing.
//
// Not the build clock. A wall-clock stamp makes every index.json differ from
// the last one even when nothing in the directory moved, so no build ever
// settles — and in a mirror, where cairn writes into the tree it indexes, that
// rewrite moves the file's own mtime, which is itself a change the parent
// listing records. Derived from the content, two builds of one tree produce the
// same bytes, and the field answers the more useful question anyway: as of when
// is this listing accurate.
//
// Entry times arrive already in UTC and truncated to the second, so a listing
// built in two timezones is byte-identical.
func (r *runner) newest(relDir string, entries []model.Entry) time.Time {
	var t time.Time
	for _, e := range entries {
		if e.ModTime.After(t) {
			t = e.ModTime
		}
	}
	if !t.IsZero() {
		return t
	}
	// An empty directory, or a source carrying no times of its own. The
	// directory's own stamp is still derived from the tree rather than the run.
	fi, err := os.Stat(filepath.Join(r.root, filepath.FromSlash(relDir)))
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime().UTC().Truncate(time.Second)
}

// rebase moves every entry path under base_path.
//
// Entry.Path is rooted at cairn's root, which is only the site root when the
// two happen to coincide. A tree indexed from static/_odds and served at /_odds
// emitted /mockups/x.html for a file the site serves at /_odds/mockups/x.html,
// so every link was a fresh 404 and nothing in cairn mentioned it. This is the
// one funnel all three producers pass through.
//
// An authored manifest path that is already a full URL is left alone: it names
// somewhere else entirely, which is what the manifest source is for.
func (r *runner) rebase(entries []model.Entry) []model.Entry {
	if r.cfg.BasePath == "" {
		return entries
	}
	out := make([]model.Entry, len(entries))
	copy(out, entries)
	for i := range out {
		if strings.HasPrefix(out[i].Path, "/") {
			out[i].Path = r.cfg.BasePath + out[i].Path
		}
	}
	return out
}

// warn logs each walk warning.
func (r *runner) warn(ws []walk.Warning) {
	for _, w := range ws {
		r.log.Warn("skipped an entry", "path", w.Path, "err", w.Err)
	}
}

// dirOverride reads a directory's .cairn.yaml, if present. An unreadable or
// malformed one is ignored rather than fatal — it is a local preference, not
// authored content whose loss would make the index wrong.
//
// Deliberately not decoded with KnownFields, unlike the root cairn.yaml. The
// same file is read twice by two decoders: this one takes the settings, and
// walk.Manifest takes entries: from it when source: manifest. Each has to
// ignore what the other owns, so strictness here would refuse every authored
// manifest in the repository.
func (r *runner) dirOverride(absDir string) *config.Override {
	p := filepath.Join(absDir, ".cairn.yaml")
	// #nosec G304 -- p is a directory cairn was configured to scan plus a fixed
	// filename.
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var o config.Override
	if err := yaml.Unmarshal(b, &o); err != nil {
		r.log.Warn("ignoring malformed .cairn.yaml", "path", p, "err", err)
		return nil
	}
	return &o
}
