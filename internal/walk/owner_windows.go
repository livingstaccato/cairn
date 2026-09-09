// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

//go:build windows

package walk

import "os"

// Windows has no uid/gid equivalent -- ownership there is ACL-based, a
// different and larger feature than this pass covers. Mode still populates
// on this platform (see entry() in fs.go): only Owner and Group are a
// stated scope limit here, not a silent gap.
func ownerOf(info os.FileInfo) (owner, group string) {
	return "", ""
}
