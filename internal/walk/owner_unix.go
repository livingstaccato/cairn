// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

//go:build unix

package walk

import (
	"os"
	"os/user"
	"strconv"
	"sync"
	"syscall"
)

// Indirected so a test can inject a failing lookup without needing a uid
// that is actually absent from the machine running the test.
var (
	lookupUserID  = user.LookupId
	lookupGroupID = user.LookupGroupId
)

// A mirror can hold many files owned by the same few users; resolving the
// same uid/gid to a name on every one of them would mean a LookupId call
// (a real /etc/passwd or NSS read) per file rather than per distinct owner.
var (
	userCache  sync.Map // uid string -> resolved username or the uid itself
	groupCache sync.Map // gid string -> resolved group name or the gid itself
)

// ownerOf reads owner and group from info's platform-specific Sys(), falling
// back to empty strings when Sys() is not the type this platform's os
// package actually returns (never observed in practice, but Sys() is
// documented as returning `any`, so this has to be checked rather than
// asserted).
func ownerOf(info os.FileInfo) (owner, group string) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", ""
	}
	return resolveUser(strconv.FormatUint(uint64(stat.Uid), 10)),
		resolveGroup(strconv.FormatUint(uint64(stat.Gid), 10))
}

// resolveUser resolves a uid to a username, falling back to the uid itself
// on any lookup failure -- a uid with no passwd entry (common inside a
// container) must not fail the build over a column nobody required be
// perfectly resolved.
func resolveUser(uid string) string {
	if v, ok := userCache.Load(uid); ok {
		return v.(string)
	}
	name := uid
	if u, err := lookupUserID(uid); err == nil {
		name = u.Username
	}
	userCache.Store(uid, name)
	return name
}

func resolveGroup(gid string) string {
	if v, ok := groupCache.Load(gid); ok {
		return v.(string)
	}
	name := gid
	if g, err := lookupGroupID(gid); err == nil {
		name = g.Name
	}
	groupCache.Store(gid, name)
	return name
}
