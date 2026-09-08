// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package emit

import "time"

// BuildInfo is the small footer banner a listing page shows by default:
// when this build ran and what built it. The zero value renders nothing —
// BareHTML and HugoContent both check Version's truthiness rather than
// carrying a separate on/off flag, the same pattern base_path's own
// omitempty already uses, so there is one field to check instead of two
// that could disagree.
type BuildInfo struct {
	// Version is cairndex's own version — a tag, or a git-describe string
	// carrying the short SHA, exactly what --version reports. Empty turns
	// the banner off.
	Version string
	// Generated is when this build ran, not when a listing's newest entry
	// was modified — Listing.Generated already means that.
	Generated time.Time
}
