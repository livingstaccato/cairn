// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package serve

import (
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// IndexFile is what a directory request is answered with when the server is
// not told a config's own index_basename.
//
// http.FileServer would fall back to a listing it generates itself when this is
// missing. That fallback is off here and stays off: a directory with no cairndex
// index is a 404. The standard library's listing names every file in the
// directory, honours no hide rule, no protect glob and no sidecar, and quietly
// undoes the one decision this whole program exists to make.
const IndexFile = "index.html"

// noStore is sent on every response.
//
// This viewer sits behind a watcher, so the bytes under it are expected to
// change while someone is looking at them. A cache header promising otherwise
// would be a lie the browser then acts on, and the reported symptom is that
// cairndex did not rebuild — which it did.
const noStore = "no-store"

// Media types cairndex pins rather than looks up.
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

// byName covers what cairndex writes with no extension at all. Matched by exact
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
// Nothing here opens by request path a second time once contained has
// resolved it: target and send both open the resolved path contained
// returns, so the file that gets served is the one just validated rather
// than whatever a fresh resolution of the client's path finds.
type files struct {
	log *slog.Logger
	// index is the filename that answers a directory request, e.g.
	// "home.html" for a config using index_basename: home. Empty falls back
	// to IndexFile, so a caller that only has a directory still works.
	index string
	// base is the served directory, absolute and with its own symlinks
	// resolved once at startup. http.Dir rejects the encoded spellings of
	// ".." a client sends, but it does nothing about a symlink already
	// standing in the tree: opening one follows it to wherever it points,
	// which is how a build artifact that happens to be a symlink hands back
	// a file from outside the served root. Every path is checked against
	// this before it is opened.
	base string
}

// indexName is the file this directory request is answered with.
func (h *files) indexName() string {
	if h.index == "" {
		return IndexFile
	}
	return h.index
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
	http.Error(w, "cairndex serve answers GET and HEAD", http.StatusMethodNotAllowed)
	return false
}

// target reports which file answers a request path, and whether that path named
// a directory. A directory is answered by its index and by nothing else.
func (h *files) target(name string) (string, bool, error) {
	resolved, err := h.contained(name)
	if err != nil {
		return "", false, err
	}

	// #nosec G304 -- resolved is what contained just validated stays inside
	// h.base; opening name again here would only re-resolve it and reopen
	// the TOCTOU gap this exists to narrow.
	f, err := os.Open(resolved)
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
	return path.Join(name, h.indexName()), true, nil
}

// send writes the file out with the type cairndex chose for it.
//
// http.ServeContent handles ranges and conditional requests from here, and it
// leaves an already-set Content-Type alone — which is the whole reason the type
// is decided before the call rather than after.
func (h *files) send(w http.ResponseWriter, r *http.Request, name string) {
	resolved, err := h.contained(name)
	if err != nil {
		h.miss(w, r, name, err)
		return
	}

	// #nosec G304 -- resolved is what contained just validated stays inside
	// h.base; opening name again here would only re-resolve it and reopen
	// the TOCTOU gap this exists to narrow.
	f, err := os.Open(resolved)
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

// contained resolves name against the served root and refuses a result
// outside it, following symlinks the way opening the file would. Callers
// open the resolved path this returns rather than resolving name a second
// time, so the file that gets served is the one this check actually looked
// at.
//
// h.base is resolved once at startup; name is what may hide a symlink, so it
// is the half resolved here, on every request. A missing file resolves with
// an error too, which is fine: the caller treats that the same as any other
// miss.
//
// This narrows the race to the syscall gap between EvalSymlinks here and the
// os.Open a caller makes right after; it does not close it. A concurrent
// writer able to replace the resolved path between those two calls still
// wins — a portable, no-follow, descriptor-relative open would close it, but
// that is platform-specific (Linux's openat2 with RESOLVE_NO_SYMLINKS) and
// this serves macOS and Windows too. Retargeting h.base itself is not
// covered either: base is resolved once at startup, the same trade cairndex
// already makes for verifiedAncestors in internal/verify.
func (h *files) contained(name string) (string, error) {
	candidate := filepath.Join(h.base, filepath.FromSlash(strings.TrimPrefix(name, "/")))
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(h.base, resolved)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s resolves outside the served root", name)
	}
	return resolved, nil
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
// its stylesheet at /cairndex.css and its entries one level too high.
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

// unknownType is declared for anything cairndex did not write and does not
// recognize.
//
// Leaving Content-Type unset used to be the answer here, on the theory that
// content sniffing was a better guess than a table that had never seen the
// file. It is not: net/http's ServeContent sniffs an unset Content-Type
// itself, server-side, before the response ever reaches a browser, and bytes
// shaped like HTML in an extensionless release artifact come back declared
// text/html — X-Content-Type-Options: nosniff stops a browser from
// second-guessing a declared type, it does nothing about the type this
// process just declared. octet-stream downloads rather than executes.
const unknownType = "application/octet-stream"

// setType declares the media type cairndex chose, or the safe unknown default.
func setType(w http.ResponseWriter, name string) {
	base := path.Base(name)
	if ct, ok := byName[base]; ok {
		w.Header().Set("Content-Type", ct)
		return
	}
	if ct, ok := byExtension[strings.ToLower(path.Ext(base))]; ok {
		w.Header().Set("Content-Type", ct)
		return
	}
	w.Header().Set("Content-Type", unknownType)
}
