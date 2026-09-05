// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for what comes back: the media type on cairn's own outputs, the index
// that answers a directory, and the requests that must not be answered at all.

package serve

import (
	"bufio"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	appJSON       = "application/json"
	imageSVG      = "image/svg+xml"
	textCSSUTF8   = "text/css; charset=utf-8"
	textCSVUTF8   = "text/csv; charset=utf-8"
	textHTMLUTF8  = "text/html; charset=utf-8"
	textJSUTF8    = "text/javascript; charset=utf-8"
	textPlainUTF8 = "text/plain; charset=utf-8"
)

// poison rewrites this process's mime table to what an unhelpful host would
// say: a Windows registry that calls a CSV a spreadsheet, a container image
// with nothing but coarse guesses in it. A content-type test that passes
// without this proves only that the machine running it happened to agree.
//
// There is no way to remove an entry once added, and none is needed: every
// extension below is one cairn pins, so nothing that runs afterwards reaches
// the table to read it.
func poison(t *testing.T) {
	t.Helper()
	for ext, wrong := range map[string]string{
		".css":  "text/plain",
		".csv":  "application/vnd.ms-excel",
		".html": "text/plain",
		".js":   "text/plain",
		".json": "text/plain",
		".svg":  "text/plain",
		".txt":  "application/octet-stream",
	} {
		if err := mime.AddExtensionType(ext, wrong); err != nil {
			t.Fatal(err)
		}
	}
}

// TestEveryOutputCairnWritesIsDeclared checks the table itself, not a response.
//
// A media type that arrives correct because the host's mime table agreed is not
// a media type cairn chose, and the agreement is not there on every host. What
// this asserts is that something is declared at all, and exactly what.
func TestEveryOutputCairnWritesIsDeclared(t *testing.T) {
	cases := []struct {
		file string
		want string
	}{
		{"index.json", appJSON},
		{"tree.json", appJSON},
		{"search-index.json", appJSON},
		{"index.csv", textCSVUTF8},
		{"index.txt", textPlainUTF8},
		{"index.html", textHTMLUTF8},
		{"SHA256SUMS", textPlainUTF8},
		{"cairn.css", textCSSUTF8},
		{"cairn.js", textJSUTF8},
		{"icons.svg", imageSVG},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			w := httptest.NewRecorder()
			setType(w, "/anywhere/"+c.file)
			if got := w.Header().Get("Content-Type"); got != c.want {
				t.Errorf("setType(%q) declared %q, want %q", c.file, got, c.want)
			}
		})
	}

	// The other half of the rule: cairn declares what it wrote and nothing
	// else. A release artifact is ServeContent's guess to make, because it has
	// the bytes to make it with.
	w := httptest.NewRecorder()
	setType(w, "/anywhere/release.bin")
	if got := w.Header().Get("Content-Type"); got != "" {
		t.Errorf("setType declared %q for a file cairn did not write, want nothing", got)
	}
}

func TestContentTypesArePinned(t *testing.T) {
	poison(t)
	_, base := start(t, tree(t))

	cases := []struct {
		name string
		path string
		want string
	}{
		{"listing json", "/index.json", appJSON},
		{"recursive json", "/tree.json", appJSON},
		{"search index", "/search-index.json", appJSON},
		{"upper-case extension", "/Data.JSON", appJSON},
		{"listing csv", "/index.csv", textCSVUTF8},
		{"listing text", "/index.txt", textPlainUTF8},
		{"checksums, no extension", "/SHA256SUMS", textPlainUTF8},
		{"listing page", "/index.html", textHTMLUTF8},
		{"directory answered by its index", "/", textHTMLUTF8},
		{"subdirectory index", "/docs/", textHTMLUTF8},
		{"stylesheet", "/cairn.css", textCSSUTF8},
		{"script", "/cairn.js", textJSUTF8},
		{"icon sprite", "/icons.svg", imageSVG},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp, _ := get(t, base+c.path)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d, want 200", resp.StatusCode)
			}
			if got := resp.Header.Get("Content-Type"); got != c.want {
				t.Errorf("Content-Type is %q, want %q; the host's mime table is not "+
					"the same on every machine and cannot be the source of this", got, c.want)
			}
		})
	}
}

func TestUnknownTypesAreLeftToBeWorkedOut(t *testing.T) {
	// Nothing is pinned for a release artifact, so it must not be declared as
	// text on the strength of having no rule.
	_, base := start(t, tree(t))
	resp, _ := get(t, base+"/release.bin")
	if got := resp.Header.Get("Content-Type"); strings.HasPrefix(got, "text/") {
		t.Errorf("Content-Type is %q; binary content must not be announced as text", got)
	}
}

func TestDirectoryIsAnsweredByItsIndex(t *testing.T) {
	_, base := start(t, tree(t))

	for path, want := range map[string]string{"/": "root index", "/docs/": "docs index"} {
		resp, body := get(t, base+path)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: status %d, want 200", path, resp.StatusCode)
		}
		if !strings.Contains(body, want) {
			t.Errorf("GET %s returned %q, want the directory's own index", path, body)
		}
	}
}

func TestDirectoryWithoutAnIndexIsNotListed(t *testing.T) {
	_, base := start(t, tree(t))

	resp, body := get(t, base+"/bare/")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d, want 404: a directory cairn has not indexed has no listing", resp.StatusCode)
	}
	if strings.Contains(body, "notes.md") {
		t.Errorf("the response listed the directory's contents:\n%s\n\nWhat a directory "+
			"listing says is cairn's to decide; the standard library must not answer for it", body)
	}
}

func TestDirectoryRedirectsToItsTrailingSlash(t *testing.T) {
	_, base := start(t, tree(t))

	// Follow nothing: the redirect itself is what is being tested.
	noFollow := &http.Client{
		Transport:     &http.Transport{DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := noFollow.Get(base + "/docs?q=1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("status %d, want 301", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "docs/?q=1" {
		t.Errorf("Location is %q, want a relative %q so relative links in the index resolve", got, "docs/?q=1")
	}
}

func TestNothingEscapesTheServedDirectory(t *testing.T) {
	out := tree(t)
	outside := filepath.Join(filepath.Dir(out), "secret.txt")
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("the fixture has no file outside the served root, so this test proves nothing: %v", err)
	}
	s, _ := start(t, out)
	addr := s.BoundAddr()

	cases := []struct {
		name   string
		target string
	}{
		{"plain traversal", "/../secret.txt"},
		{"deep traversal", "/../../secret.txt"},
		{"traversal below a real directory", "/docs/../../secret.txt"},
		{"encoded traversal", "/%2e%2e%2fsecret.txt"},
		{"encoded, segment by segment", "/%2e%2e/%2e%2e/secret.txt"},
		{"half-encoded traversal", "/..%2fsecret.txt"},
		{"encoded traversal below a real directory", "/docs/%2e%2e%2f%2e%2e%2fsecret.txt"},
		{"doubled dots", "/....//secret.txt"},
		{"encoded backslash", "/%2e%2e%5csecret.txt"},
		{"absolute path", "//secret.txt"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp, body := raw(t, addr, c.target)
			if resp.StatusCode == http.StatusOK {
				t.Errorf("GET %s returned 200; serving is confined to the directory given", c.target)
			}
			if strings.Contains(body, "not yours") {
				t.Errorf("GET %s returned a file from outside the served directory", c.target)
			}
		})
	}
}

func TestAnIndexThatIsADirectoryIsNotServed(t *testing.T) {
	out := tree(t)
	if err := os.MkdirAll(filepath.Join(out, "odd", "index.html"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, base := start(t, out)

	resp, _ := get(t, base+"/odd/")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status %d, want 404 for a directory whose index is itself a directory", resp.StatusCode)
	}
}

func TestNothingIsCached(t *testing.T) {
	_, base := start(t, tree(t))

	resp, _ := get(t, base+"/index.json")
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control is %q, want %q: a viewer beside a watcher that serves "+
			"yesterday's bytes is worse than no viewer", got, "no-store")
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options is %q, want %q", got, "nosniff")
	}
}

func TestOnlyGetAndHeadAreAnswered(t *testing.T) {
	_, base := start(t, tree(t))

	resp, err := client.Post(base+"/index.json", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	body := read(t, resp)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status %d, want 405; nothing here writes", resp.StatusCode)
	}
	if got := resp.Header.Get("Allow"); !strings.Contains(got, "GET") || !strings.Contains(got, "HEAD") {
		t.Errorf("Allow is %q, want it to name GET and HEAD", got)
	}
	if strings.Contains(body, "<h1>") {
		t.Errorf("a rejected method still returned content: %q", body)
	}
}

func TestHeadReturnsTheHeadersAndNoBody(t *testing.T) {
	_, base := start(t, tree(t))

	req, err := http.NewRequest(http.MethodHead, base+"/index.csv", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body := read(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != textCSVUTF8 {
		t.Errorf("Content-Type is %q on a HEAD, want the same as a GET", got)
	}
	if body != "" {
		t.Errorf("HEAD returned a body: %q", body)
	}
}

// get fetches a URL with the keep-alive-free client and returns the response
// with its body already read and closed.
func get(t *testing.T, target string) (*http.Response, string) {
	t.Helper()
	resp, err := client.Get(target)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	return resp, read(t, resp)
}

func read(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(body)
}

// raw types a request line onto the socket by hand. An http.Client resolves
// "/../" against the base URL before anything reaches the wire, so a traversal
// attempt sent through one tests the client, not the server.
func raw(t *testing.T, addr, target string) (*http.Response, string) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, waitFor)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(waitFor)); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(conn, "GET %s HTTP/1.0\r\nHost: cairn.invalid\r\n\r\n", target); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read response for %q: %v", target, err)
	}
	return resp, read(t, resp)
}
