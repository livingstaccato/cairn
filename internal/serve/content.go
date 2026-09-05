// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package serve

import (
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
)

// IndexFile is the only thing a directory request is ever answered with.
//
// http.FileServer would fall back to a listing it generates itself when this is
// missing. That fallback is off here and stays off: a directory with no cairn
// index is a 404. The standard library's listing names every file in the
// directory, honours no hide rule, no protect glob and no sidecar, and quietly
// undoes the one decision this whole program exists to make.
const IndexFile = "index.html"

// noStore is sent on every response.
//
// This viewer sits behind a watcher, so the bytes under it are expected to
// change while someone is looking at them. A cache header promising otherwise
// would be a lie the browser then acts on, and the reported symptom is that
// cairn did not rebuild — which it did.
const noStore = "no-store"

// Media types cairn pins rather than looks up.
//
// mime.TypeByExtension is not a fixed table. Go's built-in list is overwritten
// at init by the host's — /etc/mime.types on Unix, the registry on Windows — so
// the same index.csv is text/csv on one machine, application/vnd.ms-excel on a
// Windows box whose registry says so, and nothing at all in a scratch container
// with no mime table, where it falls through to sniffing the first 512 bytes
// and arrives as text/plain. An output that renders differently depending on
// which machine served it is not a view of the tree.
//
// Extensions are matched lower-cased. JSON carries no charset because
// RFC 8259 defines none: the encoding is UTF-8 by the media type itself, and a
// charset parameter on it is at best ignored.
var byExtension = map[string]string{
	".css":  "text/css; charset=utf-8",
	".csv":  "text/csv; charset=utf-8",
	".htm":  "text/html; charset=utf-8",
	".html": "text/html; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
	".json": "application/json",
	".svg":  "image/svg+xml",
	".txt":  "text/plain; charset=utf-8",
	".xml":  "application/xml",
}

// byName covers what cairn writes with no extension at all. Matched by exact
// name rather than by "anything without a dot", because a mirror is full of
// release artifacts that carry no suffix and are not text; declaring those as
// text on the strength of a missing extension would be a worse guess than the
// one this table exists to replace.
var byName = map[string]string{
	// emit.SumsFile. Named here rather than imported so that serving a tree
	// does not depend on the package that built it.
	"SHA256SUMS": "text/plain; charset=utf-8",
}

// files answers requests out of one directory and nothing above it.
//
// The root is an http.Dir, which resolves the request path against the
// directory and rejects what climbs out of it — including the encoded spellings
// of ".." that a client never sends but a script does. Containment is not
// re-implemented here; it is delegated to the one place in the standard library
// that has already had every traversal trick thrown at it.
type files struct {
	root http.FileSystem
	log  *slog.Logger
}

func (h *files) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.accept(w, r) {
		return
	}

	clean := path.Clean("/" + r.URL.Path)
	name, isDir, err := h.target(clean)
	if err != nil {
		h.miss(w, r, clean, err)
		return
	}
	if isDir && !strings.HasSuffix(r.URL.Path, "/") {
		redirect(w, r)
		return
	}
	h.send(w, r, name)
}

// accept sets the headers every response carries and turns away anything that
// is not a read. Nothing here writes to the tree, so a PUT or a DELETE is a
// client talking to the wrong server; answering it with a 404 would tell them
// the path was the problem.
func (h *files) accept(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Cache-Control", noStore)
	// The types below are chosen, not guessed, so there is nothing for a
	// browser to improve on by second-guessing them.
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	w.Header().Set("Allow", "GET, HEAD")
	http.Error(w, "cairn serve answers GET and HEAD", http.StatusMethodNotAllowed)
	return false
}

// target reports which file answers a request path, and whether that path named
// a directory. A directory is answered by its index and by nothing else.
func (h *files) target(name string) (string, bool, error) {
	f, err := h.root.Open(name)
	if err != nil {
		return "", false, err
	}
	fi, err := f.Stat()
	h.close(f, name)
	if err != nil {
		return "", false, err
	}
	if !fi.IsDir() {
		return name, false, nil
	}
	return path.Join(name, IndexFile), true, nil
}

// send writes the file out with the type cairn chose for it.
//
// http.ServeContent handles ranges and conditional requests from here, and it
// leaves an already-set Content-Type alone — which is the whole reason the type
// is decided before the call rather than after.
func (h *files) send(w http.ResponseWriter, r *http.Request, name string) {
	f, err := h.root.Open(name)
	if err != nil {
		h.miss(w, r, name, err)
		return
	}
	defer h.close(f, name)

	fi, err := f.Stat()
	if err != nil {
		h.miss(w, r, name, err)
		return
	}
	if fi.IsDir() {
		// Something in the tree is named index.html and is a directory. There
		// is no page here, and serving the directory would mean generating the
		// listing this server refuses to generate.
		h.miss(w, r, name, fs.ErrNotExist)
		return
	}

	setType(w, name)
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
}

// miss answers everything that could not be served with the same 404.
//
// A missing file, an unreadable one and one outside the served directory are
// one answer on purpose. Distinguishing them tells a caller which paths exist
// on a machine they cannot see, and no viewer of a static tree is any better
// off for knowing. The real reason goes to the log, where the operator is.
func (h *files) miss(w http.ResponseWriter, r *http.Request, name string, err error) {
	h.log.Debug("not served", "path", r.URL.Path, "resolved", name, "err", err)
	http.Error(w, "not found", http.StatusNotFound)
}

// close reports what it could not close rather than dropping it. A leaked
// descriptor in a long-running server is a failure that shows up hours later as
// something else entirely.
func (h *files) close(f http.File, name string) {
	if err := f.Close(); err != nil {
		h.log.Warn("could not close a served file", "path", name, "err", err)
	}
}

// redirect sends a browser to the trailing-slash form of a directory URL.
//
// Without it every relative link in the index resolves against the parent
// directory instead of the one being viewed, so a page served at /docs finds
// its stylesheet at /cairn.css and its entries one level too high.
//
// The target is relative and built only from the last segment of the path the
// client already asked for, so there is no spelling of a request that turns
// this into a redirect to another host.
func redirect(w http.ResponseWriter, r *http.Request) {
	target := path.Base(r.URL.Path) + "/"
	if q := r.URL.RawQuery; q != "" {
		target += "?" + q
	}
	w.Header().Set("Location", target)
	w.WriteHeader(http.StatusMovedPermanently)
}

// setType declares the media type, or declares nothing.
//
// Leaving the header unset is a real answer: for anything cairn did not write
// — a tarball, a package, an image in a directory being indexed — the host's
// table and then content sniffing are better guesses than a table that has
// never seen the file.
func setType(w http.ResponseWriter, name string) {
	base := path.Base(name)
	if ct, ok := byName[base]; ok {
		w.Header().Set("Content-Type", ct)
		return
	}
	if ct, ok := byExtension[strings.ToLower(path.Ext(base))]; ok {
		w.Header().Set("Content-Type", ct)
	}
}
