// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package build

import (
	"fmt"
	"path"
	"path/filepath"
	"slices"

	cairndexassets "github.com/livingstaccato/cairndex/assets/cairndex"
	"github.com/livingstaccato/cairndex/internal/config"
	"github.com/livingstaccato/cairndex/internal/emit"
	"github.com/livingstaccato/cairndex/internal/meta"
	"github.com/livingstaccato/cairndex/internal/model"
	"github.com/livingstaccato/cairndex/internal/walk"
)

// This file answers one question: given a finished listing for a directory,
// which files does that directory get and who renders each. The walk, the
// metadata merge and the hashing that produce the listing are build.go's.

// treeBasename is the filename stem for a recursive listing, beside the
// per-directory one.
const treeBasename = "tree"

// emitTree renders the recursive listing for a directory.
func (r *runner) emitTree(relDir string, s config.Settings, prose string, src meta.FileSource) error {
	all, err := r.treeEntries(relDir)
	if err != nil {
		return err
	}
	return r.emitFor(relDir, treeBasename, r.listing(relDir, all), s, prose, src)
}

// treeEntries collects every descendant of relDir for the recursive listing,
// each resolved through the same pipeline as its own per-directory listing:
// settings resolved per directory, source dispatched on it, generated output
// dropped, and a sidecar's hidden/weight/title/url applied. Without this the
// recursive listing and the per-directory ones disagree about what a
// directory contains — a file a sidecar hides stays out of index.json but
// still appears in tree.json, and source: manifest or source: pages content
// is invisible to it entirely.
//
// Depth counts from relDir, matching Dir's convention, so it is set here
// rather than trusted from collect: every producer's Depth answers "how deep
// under the directory I was asked to list", which for a producer called at
// relDir="a/b/c" is always relative to c, not to relDir.
func (r *runner) treeEntries(relDir string) ([]model.Entry, error) {
	var out []model.Entry
	seen := map[string]bool{}
	// Seeded with the traversal root's own identity: without this, a link
	// back to relDir itself is not caught until the second time something
	// reaches it — first through the link with nothing yet marking relDir,
	// then again when recursing into relDir's own subtree finds the same
	// link a second time. One wasted full pass over the tree before the
	// cycle stops, and every entry under relDir counted twice against
	// tree_max_entries along the way.
	if abs, err := filepath.EvalSymlinks(filepath.Join(r.root, filepath.FromSlash(relDir))); err == nil {
		seen[abs] = true
	}

	var recurse func(rel string, depth int) error
	recurse = func(rel string, depth int) error {
		absDir := filepath.Join(r.root, filepath.FromSlash(rel))
		s := r.cfg.Resolve(rel, r.dirOverride(absDir))
		entries, err := r.collect(rel, absDir, s)
		if err != nil {
			return err
		}
		for i := range entries {
			entries[i].Depth = depth
		}
		for _, e := range entries {
			out = append(out, e)
			if len(out) > r.cfg.TreeMaxEntries {
				return fmt.Errorf("tree under %q exceeds tree_max_entries (%d); "+
					"raise the cap or set recursive: false for this rule", relDir, r.cfg.TreeMaxEntries)
			}
			if !e.IsDir {
				continue
			}
			child := path.Join(rel, e.Name)
			leave, loop := r.enterSymlink(seen, child)
			if loop {
				continue
			}
			err := recurse(child, depth+1)
			leave()
			if err != nil {
				return err
			}
		}
		return nil
	}

	if err := recurse(relDir, 1); err != nil {
		return nil, err
	}
	return out, nil
}

// enterSymlink resolves child's real identity and, if it is already on the
// current descent's own path, reports a loop rather than recursing into it
// again. leave removes the mark once the caller is done descending — not
// left standing for the rest of the walk, which is what let two unrelated
// symlinks resolving to the same real directory falsely flag the second one:
// siblings sharing a target are not a cycle, only an ancestor reached again
// through a link is. A path with nothing to resolve (not a symlink, or one
// that is broken) returns a no-op leave and never loops.
func (r *runner) enterSymlink(seen map[string]bool, child string) (leave func(), loop bool) {
	abs, err := filepath.EvalSymlinks(filepath.Join(r.root, filepath.FromSlash(child)))
	if err != nil {
		return func() {}, false
	}
	if seen[abs] {
		r.warn([]walk.Warning{{Path: child, Err: fmt.Errorf("symlink loop, skipped")}})
		return func() {}, true
	}
	seen[abs] = true
	return func() { delete(seen, abs) }, false
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
	config.OutputSearch: (*runner).emitSearch,
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
// bytes cairndex produced.
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
	wantsPEP503 := slices.Contains(c.settings.Outputs, config.OutputPEP503)
	if wantsPEP503 {
		if err := r.checkPEP503(c); err != nil {
			return err
		}
		if err := r.emitPEP503JSON(c); err != nil {
			return err
		}
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
		// The same rule emitHTML applies, for the renderer that is Hugo's.
		AtRoot:    c.relDir == ".",
		BasePath:  r.cfg.BasePath,
		PEP503:    wantsPEP503,
		BuildInfo: r.buildInfo(),
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

// hugoRenders reports whether Hugo produces this format, in which case cairndex
// must not write it too. json is excluded separately: cairndex always writes it,
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
		case config.OutputJSON, config.OutputCSV, config.OutputText, config.OutputSearch:
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
// the consumer's theme, which cairndex does not have.
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
	b, err := emit.BareHTML(emit.BarePage{
		Listing:     c.listing,
		Prose:       c.prose,
		MaxRendered: c.settings.MaxRendered,
		// The top of the indexed tree, where a parent link would point at
		// something cairndex never wrote.
		AtRoot:    c.relDir == ".",
		BuildInfo: r.buildInfo(),
	})
	if err != nil {
		return err
	}
	return r.write(c.relDir, c.basename+".html", b)
}

// emitSearch writes the standalone search index.
//
// The filename is fixed, so the choice is which listing fills it. Under
// recursive: true the tree listing is the one worth searching — an index
// covering a single directory of a deep tree finds almost nothing — and
// emitting from both listings would write the same file twice, the second
// overwriting the first with less.
func (r *runner) emitSearch(c emitCtx) error {
	want := r.cfg.IndexBasename
	if c.settings.Recursive {
		want = treeBasename
	}
	if c.basename != want {
		return nil
	}
	b, err := emit.Search(c.listing)
	if err != nil {
		return err
	}
	if err := r.write(c.relDir, emit.SearchFile, b); err != nil {
		return err
	}
	// The script the search page runs. cairndexassets.SearchJS is embedded
	// from assets/cairndex/search.js — the same file mode: hugo reads
	// through Hugo Pipes for the styled listing's own JS — so both modes
	// ship identical bytes instead of two copies that can drift.
	if err := r.write(c.relDir, emit.SearchScriptFile, cairndexassets.SearchJS); err != nil {
		return err
	}
	return r.emitSearchPage(c)
}

// emitSearchPage writes the search box as its own page, one level under the
// listing, at emit.SearchPageDir — not a sibling index.html: Hugo parses
// any .html file inside a content bundle as a page source of its own and
// refuses to publish raw HTML content by policy, so mode: hugo needs a real
// page here, the same reason pep503 needs its own layout rather than
// nesting inside the listing's.
func (r *runner) emitSearchPage(c emitCtx) error {
	searchDir := path.Join(c.relDir, emit.SearchPageDir)
	if r.cfg.Mode == config.ModeHugo {
		b, err := emit.HugoSearchContent(c.listing.Path)
		if err != nil {
			return err
		}
		return r.write(searchDir, emit.HugoContentFile, b)
	}
	page, err := emit.SearchPage(c.listing)
	if err != nil {
		return err
	}
	return r.write(searchDir, r.cfg.IndexBasename+".html", page)
}

// emitPEP503 renders a Python simple index.
//
// It writes index.html, which is the filename PEP 503 requires, so a
// directory configured with both html and pep503 collides there. config's
// own validateOutputConflicts refuses that combination outright now; mode:
// direct's write guard happened to catch it too, as a side effect of two
// writes landing on one path within a run, but mode: hugo has no such guard
// to fall back on.
func (r *runner) emitPEP503(c emitCtx) error {
	if c.basename != r.cfg.IndexBasename {
		return nil
	}
	if err := r.checkPEP503(c); err != nil {
		return err
	}
	b, err := emit.PEP503(c.listing)
	if err != nil {
		return err
	}
	return r.write(c.relDir, "index.html", b)
}

// emitPEP503JSON writes the anchors mode: hugo's pep503.html partial reads:
// hrefFor's encoding and hex-digest validation run once here, the same code
// PEP503 already gives direct mode's HTML, so the template needs no
// encoding or normalization logic of its own.
func (r *runner) emitPEP503JSON(c emitCtx) error {
	b, err := emit.PEP503JSON(c.listing)
	if err != nil {
		return fmt.Errorf("%s: %w", c.relDir, err)
	}
	return r.write(c.relDir, "pep503.json", b)
}

// checkPEP503 validates a listing against pep503_level when the operator
// declared one, and warns when nothing was declared and the listing mixes
// project directories with files anyway. Shared between emitPEP503 (mode:
// direct writes its own page) and emitHugo (mode: hugo hands the same
// listing to a Hugo template), since the listing a directory holds does not
// depend on which mode renders it.
func (r *runner) checkPEP503(c emitCtx) error {
	if c.settings.PEP503Level != "" {
		// The operator staked a specific, checkable claim with
		// pep503_level: a violation fails the build rather than
		// rendering a page silently untrue to it.
		if err := emit.ValidatePEP503Level(c.settings.PEP503Level, c.listing); err != nil {
			return fmt.Errorf("%s: %w", c.relDir, err)
		}
		return nil
	}
	if emit.PEP503Mixed(c.listing) {
		// PEP 503 defines two separate levels — a root page of project
		// directories, a project's own page of its download files — and
		// this renders both with the same code, trusting a directory this
		// rule matches to hold only one kind of entry. A mixed one renders
		// a page true to neither: a subdirectory reads to a client as
		// another downloadable project, a stray file reads as one. Nothing
		// declared which level was intended here, so this warns rather
		// than fails — pep503_level is how an operator upgrades this to
		// a build error.
		r.log.Warn("pep503: directory holds both project directories and "+
			"files; PEP 503 defines two separate levels and this page is not "+
			"faithful to either", "path", c.relDir)
	}
	return nil
}

// write places one output file and records it.
func (r *runner) write(relDir, name string, body []byte) error {
	target := name
	if relDir != "." {
		target = path.Join(relDir, name)
	}
	return r.writer.Write(target, body)
}
