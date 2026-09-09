// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

//go:build windows

package walk

import (
	"os"
	"testing"
)

// Windows has no uid/gid equivalent -- real ACL-based ownership is a
// different, larger feature. This is a stated scope limit: show_owner still
// populates Mode (cross-platform, from info.Mode()), just not Owner/Group.
func TestOwnerOfEmptyOnWindows(t *testing.T) {
	dir := t.TempDir()
	path := dir + "\\f"
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	owner, group := ownerOf(info)
	if owner != "" || group != "" {
		t.Errorf("ownerOf on windows = (%q, %q), want (\"\", \"\")", owner, group)
	}
}
