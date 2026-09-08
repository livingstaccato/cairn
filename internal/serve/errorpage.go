// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package serve

import (
	"bytes"
	"html/template"
	"net/http"
)

// errorPageTemplate is what a miss or a rejected method answers with,
// instead of the standard library's plain-text default. Self-contained —
// inline styles, no external asset — the same "no CDN, works airgapped"
// rule the generated listings themselves follow. The color tokens mirror
// assets/cairndex/cairndex.css by value (this page cannot @import that
// file: it answers for whatever tree cairndex serve is pointed at, which
// may not have published cairndex.css at all), plus one addition — --c-warn
// — that stays off the listing pages on purpose: verdigris there is
// reserved for a verified checksum and nothing else, so a rejected request
// gets its own color rather than diluting what that one means.
//
// The mark is three stacked stones with the top one knocked loose, the
// same cairndex-dir glyph from icons.svg but drawn in place: a cairn
// missing a stone reads as "the marker's wrong" without a sentence of
// copy doing the work instead.
var errorPageTemplate = template.Must(template.New("error").Parse(`<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>{{ .Code }} {{ .Status }}</title>
<style>
:root {
  --c-ink: #16181d; --c-slate: #5b6270; --c-paper: #fbfbfa; --c-rule: #e2e2de; --c-warn: #b5622e;
  --c-mono: ui-monospace, "SF Mono", SFMono-Regular, "Cascadia Mono", Menlo, Consolas, monospace;
  --c-sans: ui-sans-serif, system-ui, -apple-system, "Segoe UI", Helvetica, Arial, sans-serif;
}
@media (prefers-color-scheme: dark) {
  :root { --c-ink: #e6e7e9; --c-slate: #8b93a1; --c-paper: #16181d; --c-rule: #2b2f37; --c-warn: #e0854f; }
}
* { box-sizing: border-box; }
body {
  margin: 0; min-height: 100vh; display: flex; align-items: center; justify-content: center;
  background: var(--c-paper); color: var(--c-ink); font-family: var(--c-sans);
}
main { max-width: 26rem; padding: 2rem; text-align: center; }
.mark { color: var(--c-slate); margin-bottom: 1rem; }
.mark .fallen { color: var(--c-warn); }
.code {
  font-family: var(--c-mono); font-variant-numeric: tabular-nums;
  font-size: 2rem; font-weight: 600; letter-spacing: -0.02em; margin: 0;
}
.status { font-family: var(--c-mono); font-size: 0.9375rem; color: var(--c-slate); margin: 0.25rem 0 1.25rem; }
p.detail { color: var(--c-slate); margin: 0 0 1.5rem; font-size: 0.875rem; font-family: var(--c-mono); }
a {
  color: var(--c-warn); text-decoration: none; font-size: 0.875rem;
  display: inline-block; padding: 0.3em 0.5em; margin: -0.3em -0.5em; border-radius: 3px;
}
a:hover { text-decoration: underline; }
a:focus-visible { outline: 2px solid var(--c-warn); outline-offset: 1px; }
@media (prefers-reduced-motion: reduce) { * { transition-duration: 0.01ms !important; animation-duration: 0.01ms !important; } }
</style>
</head>
<body>
<main>
<svg class="mark" width="40" height="40" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" fill="none" aria-hidden="true">
  <ellipse cx="12" cy="18.5" rx="7" ry="2.8"/>
  <ellipse cx="12.8" cy="13" rx="4.6" ry="2.3" transform="rotate(-6 12.8 13)"/>
  <ellipse class="fallen" cx="18.5" cy="7.5" rx="2.6" ry="1.7" transform="rotate(48 18.5 7.5)"/>
</svg>
<h1 class="code">{{ .Code }}</h1>
<p class="status">{{ .Status }}</p>
{{ with .Detail }}<p class="detail">{{ . }}</p>{{ end }}
<a href="/">&larr; back to the top</a>
</main>
</body>
</html>
`))

type errorPageData struct {
	Code   int
	Status string
	Detail string
}

// writeErrorPage renders code as a small standalone HTML page rather than
// the standard library's plain-text default. detail is empty unless a
// caller opted into naming the specific reason — see miss's own comment on
// why a 404 stays generic otherwise.
func writeErrorPage(w http.ResponseWriter, code int, detail string) {
	var buf bytes.Buffer
	// template.Must at package init already proved this template parses;
	// the only remaining failure mode is a future field type it cannot
	// render, which a test would catch long before this runs unattended.
	_ = errorPageTemplate.Execute(&buf, errorPageData{
		Code: code, Status: http.StatusText(code), Detail: detail,
	})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write(buf.Bytes())
}
