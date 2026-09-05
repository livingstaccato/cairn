// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package build

import (
	"fmt"
	"path"

	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/emit"
	"github.com/livingstaccato/cairn/internal/meta"
	"github.com/livingstaccato/cairn/internal/model"
	"github.com/livingstaccato/cairn/internal/walk"
)

// This file answers one question: given a finished listing for a directory,
// which files does that directory get and who renders each. The walk, the
// metadata merge and the hashing that produce the listing are build.go's.

// treeBasename is the filename stem for a recursive listing, beside the
// per-directory one.
const treeBasename = "tree"

// emitTree renders the recursive listing for a directory.
func (r *runner) emitTree(relDir string, s config.Settings, prose string, src meta.FileSource) error {
	all, warns, err := walk.Tree(r.root, relDir, s, r.cfg.TreeMaxEntries, r.treeFilter())
	if err != nil {
		return err
	}
	r.warn(warns)
	return r.emitFor(relDir, treeBasename, r.listing(relDir, all), s, prose, src)
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
	b, err := emit.BareHTML(emit.BarePage{
		Listing:     c.listing,
		Prose:       c.prose,
		MaxRendered: c.settings.MaxRendered,
		// The top of the indexed tree, where a parent link would point at
		// something cairn never wrote.
		AtRoot: c.relDir == ".",
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
	return r.write(c.relDir, emit.SearchFile, b)
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
