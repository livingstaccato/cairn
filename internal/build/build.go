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

// treeBasename is the filename stem for a recursive listing, beside the
// per-directory one.
const treeBasename = "tree"

// Result summarizes a run for the caller to report.
type Result struct {
	Dirs      int
	Files     int
	Written   []string
	Pruned    int
	Protected int
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
	// warnedStyled keeps the styled-in-direct-mode diagnostic to one line per
	// run rather than one per directory.
	warnedStyled bool
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
	return r.result, err
}

// build runs the walk, the prune and the manifest save for a successful run.
func (r *runner) build() error {
	if err := r.visit("."); err != nil {
		return err
	}
	if err := r.cache.Save(); err != nil {
		r.log.Warn("could not save the hash cache; the next run re-hashes",
			"path", hash.CacheFile, "err", err)
	}
	// Anything the previous run wrote and this one did not is stale: a removed
	// file's digest, or a whole listing for a directory that no longer exists.
	pruned, err := r.writer.Prune()
	if err != nil {
		return err
	}
	for _, p := range pruned {
		r.log.Info("removed stale output", "path", p)
	}
	r.result.Pruned = len(pruned)

	// The manifest records what this run owns. Without it the next run cannot
	// tell its own output from content that was already there, and refuses to
	// overwrite either.
	return r.writer.Save()
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
	skip := r.generatedNames(s)
	out := entries[:0:0]
	for _, e := range entries {
		generated := !e.IsDir && skip[e.Name] &&
			!r.cfg.IsProtected(path.Join(relDir, e.Name))
		if !generated {
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
	p = strings.TrimSuffix(r.cfg.BasePath+p, "/")
	if p == "" {
		p = "/"
	}
	return model.Listing{
		Path: p, Generated: r.now, Count: len(entries),
		Entries: r.rebase(entries),
	}
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

// emitHugo writes the branch bundle Hugo renders from.
//
// Only the HTML is Hugo's. Every other output is written here, into the page's
// own bundle, and Hugo publishes a branch-bundle resource verbatim — so
// index.json, index.csv, index.txt and SHA256SUMS reach the site as the exact
// bytes cairn produced.
//
// That is why a consumer's hugo.toml declares no output formats. It also removes
// a whole class of defect: while Hugo re-rendered these from frontmatter there
// were two producers of the same file, and they drifted — a key dropped by
// omitempty came out as "<no value>", and timestamps disagreed in precision
// because YAML carried nanoseconds that Go's RFC3339 does not.
//
// index.json is written whether or not it was requested: the page reads its
// entries from it.
func (r *runner) emitHugo(c emitCtx) error {
	// A recursive listing is data, not a page: one fetch of tree.json instead of
	// a walk. It gets resources and no _index.md, because a directory holds one
	// page and that page is the directory's own listing.
	//
	// Returning nil for a non-index basename would make recursive: true write
	// nothing at all in hugo mode: Hugo renders only the page, and the recursive
	// listing is not one.
	if c.basename != r.cfg.IndexBasename {
		if err := r.emitJSON(c); err != nil {
			return err
		}
		return r.emitResources(c)
	}
	b, err := emit.HugoContent(emit.HugoPage{
		Listing:     c.listing,
		Prose:       c.prose,
		Present:     c.settings.Present,
		Source:      c.source,
		SourceText:  c.sourceText,
		Formats:     machineFormats(c.settings, c.listing),
		Recursive:   c.settings.Recursive,
		MaxRendered: c.settings.MaxRendered,
	})
	if err != nil {
		return err
	}
	if err := r.write(c.relDir, emit.HugoContentFile, b); err != nil {
		return err
	}
	if err := r.emitJSON(c); err != nil {
		return err
	}
	return r.emitResources(c)
}

// hugoRenders reports whether Hugo produces this format, in which case cairn
// must not write it too. json is excluded separately: cairn always writes it,
// because the page reads its entries from it.
func hugoRenders(format string) bool {
	switch format {
	case config.OutputHTML, config.OutputPEP503, config.OutputJSON:
		return true
	}
	return false
}

// emitResources writes the remaining outputs into the page's bundle, for Hugo
// to publish verbatim.
func (r *runner) emitResources(c emitCtx) error {
	for _, f := range c.settings.Outputs {
		if hugoRenders(f) {
			continue
		}
		fn, ok := emitters[f]
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
	// The styled presenter lives in the Hugo templates; Go renders only the bare
	// one. Asking for styled HTML in direct mode is unsatisfiable, and silence
	// would leave a mirror with no browsable page and nothing to explain why —
	// present: defaults to styled, so that is the default outcome.
	if c.settings.Present != config.PresentBare {
		if !r.warnedStyled {
			r.warnedStyled = true
			r.log.Warn("no HTML written: the styled presenter needs mode: hugo; "+
				"use present: bare to render HTML directly",
				"path", c.relDir, "present", c.settings.Present)
		}
		return nil
	}
	b, err := emit.BareHTML(c.listing, c.prose, c.settings.MaxRendered)
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
