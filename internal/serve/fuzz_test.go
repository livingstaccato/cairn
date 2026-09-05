// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Fuzz targets for the one thing a static server must never get wrong.

package serve

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/livingstaccato/cairn/internal/obs"
)

// Nothing outside the served directory may ever be readable, whatever the
// request path spells.
//
// The property is checked against content rather than status codes: a 200 is
// fine for a file inside the tree and catastrophic for one outside it, and only
// the bytes can tell them apart.
func FuzzNothingEscapesTheServedDirectory(f *testing.F) {
	seeds := []string{
		"/", "/index.html", "/sub/", "/sub/index.html",
		"/..", "/../", "/../outside.txt", "/../../outside.txt",
		"/%2e%2e/outside.txt", "/%2e%2e%2foutside.txt", "/..%2foutside.txt",
		"/....//outside.txt", "/sub/../../outside.txt", "//outside.txt",
		"/./././../outside.txt", "/%2E%2E/outside.txt", "/..\\outside.txt",
		"/\\../outside.txt", "/sub/%2e%2e/%2e%2e/outside.txt",
		"/" + strings.Repeat("../", 40) + "outside.txt",
		"/\x00/outside.txt", "/index.html%00.txt",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	base := f.TempDir()
	root := filepath.Join(base, "public")
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		f.Fatal(err)
	}
	for rel, body := range map[string]string{
		"index.html":     "<html>root</html>",
		"sub/index.html": "<html>sub</html>",
	} {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			f.Fatal(err)
		}
	}
	// A canary beside the served directory, in the shape a real deployment has:
	// the output directory sits next to everything else the process can read.
	const canary = "OUTSIDE-THE-SERVED-TREE"
	if err := os.WriteFile(filepath.Join(base, "outside.txt"), []byte(canary), 0o600); err != nil {
		f.Fatal(err)
	}

	h := &files{root: http.Dir(root), log: obs.Discard()}

	f.Fuzz(func(t *testing.T, target string) {
		req, err := http.NewRequest(http.MethodGet, "http://x"+target, nil) //nolint:noctx // no server, no deadline to carry
		if err != nil {
			return // not a request a client could send
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if strings.Contains(rec.Body.String(), canary) {
			t.Fatalf("path %q served a file outside the tree (status %d)", target, rec.Code)
		}
	})
}

// A directory with no index of cairn's own is a 404, never a listing the
// standard library generated. cairn deciding what a directory listing says is
// the entire point of the project, so no request path may talk net/http into
// writing one instead.
func FuzzNoGeneratedDirectoryListing(f *testing.F) {
	for _, s := range []string{"/", "/bare/", "/bare", "/bare/.", "/bare/./", "//bare//"} {
		f.Add(s)
	}

	root := f.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bare", "deeper"), 0o755); err != nil {
		f.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bare", "listed.txt"), []byte("x"), 0o644); err != nil {
		f.Fatal(err)
	}

	h := &files{root: http.Dir(root), log: obs.Discard()}

	f.Fuzz(func(t *testing.T, target string) {
		req, err := http.NewRequest(http.MethodGet, "http://x"+target, nil) //nolint:noctx // no server, no deadline to carry
		if err != nil {
			return
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		// net/http's own listing is a page of <a href=...> naming the entries.
		// cairn wrote no index here, so any such page came from the library.
		if body := rec.Body.String(); strings.Contains(body, "listed.txt") {
			t.Fatalf("path %q produced a directory listing cairn did not write:\n%s", target, body)
		}
	})
}
