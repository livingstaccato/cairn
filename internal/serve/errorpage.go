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
// rule the generated listings themselves follow.
var errorPageTemplate = template.Must(template.New("error").Parse(`<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>{{ .Code }} {{ .Status }}</title>
<style>
body { margin: 0; height: 100vh; display: flex; align-items: center; justify-content: center;
  font-family: ui-monospace, "SF Mono", SFMono-Regular, "Cascadia Mono", Menlo, Consolas, monospace;
  background: #fbfbfa; color: #16181d; }
main { text-align: center; }
h1 { font-size: 1.25rem; font-weight: 600; margin: 0 0 0.5rem; }
p { color: #5b6270; margin: 0 0 1.5rem; font-size: 0.875rem; }
a { color: #2f7d5d; }
@media (prefers-color-scheme: dark) {
  body { background: #16181d; color: #e6e7e9; }
  p { color: #8b93a1; }
  a { color: #5fbf92; }
}
</style>
</head>
<body>
<main>
<h1>{{ .Code }} {{ .Status }}</h1>
{{ with .Detail }}<p>{{ . }}</p>{{ end }}
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
