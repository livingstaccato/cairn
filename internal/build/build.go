// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

// Package build orchestrates one cairn run: walk, merge metadata, hash, emit.
package build

import (
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/emit"
	"github.com/livingstaccato/cairn/internal/hash"
	"github.com/livingstaccato/cairn/internal/meta"
	"github.com/livingstaccato/cairn/internal/model"
	"github.com/livingstaccato/cairn/internal/walk"
)

// treeBasename is the filename stem for a recursive listing, beside the
// per-directory one.
const treeBasename = "tree"

// Result summarizes a run for the caller to report.
type Result struct {
	Dirs    int
	Files   int
	Written []string
}

// runner carries the values every step of a build needs.
type runner struct {
	cfg    *config.Config
	root   string
	out    string
	log    *slog.Logger
	cache  *hash.Cache
	writer *emit.Writer
	now    time.Time
	result *Result
}

// Run walks rootDir and writes every configured output under outDir. Warnings
// are logged as they happen rather than accumulated: on a large mirror the
// interesting ones are the early ones, and a caller should not have to wait for
// the walk to finish to see them.
func Run(cfg *config.Config, rootDir, outDir string, log *slog.Logger) (*Result, error) {
	r := &runner{
		cfg:    cfg,
		root:   rootDir,
		out:    outDir,
		log:    log,
		cache:  hash.NewCache(filepath.Join(outDir, hash.CacheFile)),
		writer: emit.NewWriter(cfg, outDir),
		now:    time.Now().UTC(),
		result: &Result{},
	}

	if err := r.visit("."); err != nil {
		return r.result, err
	}
	if err := r.cache.Save(); err != nil {
		r.log.Warn("could not save the hash cache; the next run re-hashes",
			"path", hash.CacheFile, "err", err)
	}
	// The manifest records what this run owns. Without it the next run cannot
	// tell its own output from content that was already there, and refuses to
	// overwrite either.
	if err := r.writer.Save(); err != nil {
		return r.result, err
	}
	r.result.Written = r.writer.Written()
	return r.result, nil
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

	entries = r.dropGenerated(entries, s)

	m, mwarns, err := meta.Load(absDir)
	if err != nil {
		return nil, err
	}
	r.warn(mwarns)
	entries = meta.Apply(entries, m)

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
// Excluding unconditionally is safe. If a file cairn would generate already
// exists and cairn did not create it, emit.Writer refuses the whole build, so
// there is no case where this hides someone's own file from its own listing.
func (r *runner) dropGenerated(entries []model.Entry, s config.Settings) []model.Entry {
	skip := r.generatedNames(s)
	out := entries[:0:0]
	for _, e := range entries {
		if e.IsDir || !skip[e.Name] {
			out = append(out, e)
		}
	}
	return out
}

// generatedNames is every filename cairn writes into one directory.
func (r *runner) generatedNames(s config.Settings) map[string]bool {
	names := map[string]bool{
		emit.SumsFile:     true,
		emit.ManifestFile: true,
		hash.CacheFile:    true,
	}
	for _, base := range []string{r.cfg.IndexBasename, treeBasename} {
		for _, ext := range []string{".html", ".json", ".csv", ".txt"} {
			names[base+ext] = true
		}
	}
	if r.cfg.Mode == config.ModeHugo {
		names[emit.HugoContentFile] = true
	}
	return names
}

// hashEntries fills SHA256 for every file in the listing. A file that cannot be
// hashed warns and is left without a digest rather than failing the build: the
// listing is still correct, it just cannot be verified.
func (r *runner) hashEntries(absDir string, entries []model.Entry) {
	for i := range entries {
		if entries[i].IsDir {
			continue
		}
		sum, err := r.cache.Sum(
			filepath.Join(absDir, entries[i].Name),
			entries[i].Size, entries[i].ModTime.Unix())
		if err != nil {
			r.log.Warn("could not hash file; omitted from SHA256SUMS",
				"path", entries[i].Path, "err", err)
			continue
		}
		entries[i].SHA256 = sum
	}
}

// emitTree renders the recursive listing for a directory.
func (r *runner) emitTree(relDir string, s config.Settings, prose string, src meta.FileSource) error {
	all, warns, err := walk.Tree(r.root, relDir, s, r.cfg.TreeMaxEntries)
	if err != nil {
		return err
	}
	r.warn(warns)
	return r.emitFor(relDir, treeBasename, r.listing(relDir, all), s, prose, src)
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
	return model.Listing{Path: p, Generated: r.now, Count: len(entries), Entries: entries}
}

// emitCtx is one directory's rendering job, bundled so the format handlers
// take a receiver and one argument rather than six.
type emitCtx struct {
	relDir     string
	basename   string
	listing    model.Listing
	settings   config.Settings
	prose      string
	source     string
	sourceText string
}

// emitters maps an output format to its handler. A map rather than a switch
// because a switch arm per format is a branch per format, and the complexity
// budget is there to stop exactly this function sprawling.
var emitters = map[string]func(*runner, emitCtx) error{
	config.OutputJSON:   (*runner).emitJSON,
	config.OutputCSV:    (*runner).emitCSV,
	config.OutputText:   (*runner).emitText,
	config.OutputSums:   (*runner).emitSums,
	config.OutputHTML:   (*runner).emitHTML,
	config.OutputPEP503: (*runner).emitPEP503,
}

// emitFor writes the formats named by s.Outputs under basename.
//
// In hugo mode it writes one _index.md instead and returns: Hugo renders every
// format from that page, so emitting them here as well would produce two
// sources for the same URL that could disagree.
func (r *runner) emitFor(relDir, basename string, l model.Listing, s config.Settings, prose string, src meta.FileSource) error {
	c := emitCtx{
		relDir: relDir, basename: basename, listing: l, settings: s,
		prose: prose, source: src.Name, sourceText: src.Text,
	}
	if r.cfg.Mode == config.ModeHugo {
		return r.emitHugo(c)
	}
	for _, format := range s.Outputs {
		fn, ok := emitters[format]
		if !ok {
			return fmt.Errorf("unknown output format %q for %s", format, relDir)
		}
		if err := fn(r, c); err != nil {
			return err
		}
	}
	return nil
}

// emitHugo writes the branch bundle Hugo renders from. A recursive listing does
// not get its own page: Hugo produces tree.json from the same frontmatter.
//
// The split: Hugo renders the page formats, and cairn writes the flat text
// artifacts itself. index.json and index.csv are the listing re-rendered, so
// they belong to whatever theme and URL handling the site has. SHA256SUMS and
// index.txt are not renderings of anything — they are fixed byte formats other
// tools consume — so routing them through Hugo would mean declaring output
// formats whose only job is to add nothing, and every consumer would have to
// copy those declarations into their config.
func (r *runner) emitHugo(c emitCtx) error {
	if c.basename != r.cfg.IndexBasename {
		return nil
	}
	b, err := emit.HugoContent(c.listing, c.prose, c.settings.Present,
		c.source, c.sourceText, machineFormats(c.settings, c.listing))
	if err != nil {
		return err
	}
	if err := r.write(c.relDir, emit.HugoContentFile, b); err != nil {
		return err
	}
	return r.emitFlat(c)
}

// flatOutputs are written by cairn in either mode: fixed byte formats with no
// rendering to do.
var flatOutputs = map[string]func(*runner, emitCtx) error{
	config.OutputSums: (*runner).emitSums,
	config.OutputText: (*runner).emitText,
}

// emitFlat writes the outputs Hugo has no part in.
func (r *runner) emitFlat(c emitCtx) error {
	for _, f := range c.settings.Outputs {
		fn, ok := flatOutputs[f]
		if !ok {
			continue
		}
		if err := fn(r, c); err != nil {
			return err
		}
	}
	return nil
}

// machineFormats lists the non-HTML outputs this directory actually publishes.
//
// The footer links to these, so guessing would produce a link to a file that
// was never written. html and pep503 are excluded because they are the page
// doing the linking, and sums is dropped when nothing in the directory was
// hashed — SHA256SUMS is not written in that case either.
func machineFormats(s config.Settings, l model.Listing) []string {
	var out []string
	for _, f := range s.Outputs {
		switch f {
		case config.OutputJSON, config.OutputCSV, config.OutputText:
			out = append(out, f)
		case config.OutputSums:
			if len(emit.Sums(l)) > 0 {
				out = append(out, f)
			}
		}
	}
	return out
}

func (r *runner) emitJSON(c emitCtx) error {
	b, err := emit.JSON(c.listing)
	if err != nil {
		return err
	}
	return r.write(c.relDir, c.basename+".json", b)
}

func (r *runner) emitCSV(c emitCtx) error {
	b, err := emit.CSV(c.listing)
	if err != nil {
		return err
	}
	return r.write(c.relDir, c.basename+".csv", b)
}

// emitSums writes SHA256SUMS. A recursive listing gets none: the digests are
// already in the per-directory files, and a second copy is a second thing that
// can disagree.
func (r *runner) emitText(c emitCtx) error {
	return r.write(c.relDir, c.basename+".txt", emit.Text(c.listing))
}

func (r *runner) emitSums(c emitCtx) error {
	if c.basename != r.cfg.IndexBasename {
		return nil
	}
	b := emit.Sums(c.listing)
	if len(b) == 0 {
		return nil
	}
	return r.write(c.relDir, emit.SumsFile, b)
}

// emitHTML renders only the bare presenter. Styled HTML is Hugo's job: it needs
// the consumer's theme, which cairn does not have.
func (r *runner) emitHTML(c emitCtx) error {
	if c.settings.Present != config.PresentBare {
		return nil
	}
	b, err := emit.BareHTML(c.listing, c.prose)
	if err != nil {
		return err
	}
	return r.write(c.relDir, c.basename+".html", b)
}

// emitPEP503 renders a Python simple index.
//
// It writes index.html, which is the filename PEP 503 requires, so a directory
// configured with both html and pep503 collides there and the write guard
// refuses. That is correct: they are alternative renderings of one URL.
func (r *runner) emitPEP503(c emitCtx) error {
	if c.basename != r.cfg.IndexBasename {
		return nil
	}
	b, err := emit.PEP503(c.listing)
	if err != nil {
		return err
	}
	return r.write(c.relDir, "index.html", b)
}

// write places one output file and records it.
func (r *runner) write(relDir, name string, body []byte) error {
	target := name
	if relDir != "." {
		target = path.Join(relDir, name)
	}
	return r.writer.Write(target, body)
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
