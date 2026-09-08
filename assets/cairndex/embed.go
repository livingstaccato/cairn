// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Package cairndex embeds the static assets in this directory for mode:
// direct, which has no Hugo Pipes to read them through. mode: hugo reads
// search.js the same way it reads cairndex.js — resources.Get "cairndex/..."
// — so this embed exists only so both modes ship the exact same bytes rather
// than one importing a copy that can drift from the other.
package cairndex

import _ "embed"

//go:embed search.js
var SearchJS []byte
