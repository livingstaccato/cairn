// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package serve

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// A heading is what lets a screen reader jump straight to the page's own
// content instead of reading the whole document top to bottom looking for
// it — the redesign that gave this page real styling dropped the <h1> it
// used to have, and nothing caught it because nothing here checked for one.
func TestWriteErrorPageHasAHeading(t *testing.T) {
	w := httptest.NewRecorder()
	writeErrorPage(w, 404, "")
	body := w.Body.String()
	if !strings.Contains(body, "<h1") {
		t.Errorf("error page has no heading at all:\n%s", body)
	}
	if !strings.Contains(body, ">404</h1>") {
		t.Errorf("the heading does not carry the status code:\n%s", body)
	}
}

func TestWriteErrorPageSetsStatusAndContentType(t *testing.T) {
	w := httptest.NewRecorder()
	writeErrorPage(w, 405, "")
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != textHTMLUTF8 {
		t.Errorf("Content-Type = %q, want %s", ct, textHTMLUTF8)
	}
}

// detail is opt-in — see miss's own reasoning for why a 404 stays generic by
// default — so an empty one must render no trace of the element at all, not
// an empty paragraph a reader can tell apart from "no detail was given".
func TestWriteErrorPageOmitsDetailWhenEmpty(t *testing.T) {
	w := httptest.NewRecorder()
	writeErrorPage(w, 404, "")
	if strings.Contains(w.Body.String(), `class="detail"`) {
		t.Errorf("an empty detail still rendered the detail element:\n%s", w.Body.String())
	}
}

func TestWriteErrorPageShowsDetailWhenGiven(t *testing.T) {
	w := httptest.NewRecorder()
	writeErrorPage(w, 404, "open /srv/site/missing.txt: no such file or directory")
	body := w.Body.String()
	if !strings.Contains(body, `class="detail"`) {
		t.Errorf("a given detail did not render:\n%s", body)
	}
	if !strings.Contains(body, "no such file or directory") {
		t.Errorf("the detail text is missing:\n%s", body)
	}
}

// detail carries an error string, and an error can carry a path — a name on
// a mirror is attacker-influenced, same as every other filename cairndex
// ever writes into a page. html/template escapes by default; this is the
// test that would fail if a future edit switched to text/template or a
// safeHTML cast and reopened the hole.
func TestWriteErrorPageEscapesHostileDetail(t *testing.T) {
	w := httptest.NewRecorder()
	writeErrorPage(w, 404, `<script>alert(1)</script>`)
	body := w.Body.String()
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Errorf("detail was written unescaped, an injection point:\n%s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("detail does not appear escaped at all:\n%s", body)
	}
}
