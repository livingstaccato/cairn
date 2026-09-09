// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

//go:build unix

package walk

import (
	"errors"
	"os"
	"os/user"
	"testing"
)

// The uid/gid this test resolves the fallback for. Not a real current user's
// id: resolveUser/resolveGroup cache by id, and TestOwnerOfResolvesCurrentUser
// below caches the real result for the actual current uid/gid -- reusing that
// id here would read the cache instead of exercising the injected failure.
const noSuchID = "999999"

func TestOwnerOfResolvesCurrentUser(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Skipf("no current user in this environment: %v", err)
	}
	owner, _ := resolveUser(me.Uid), resolveGroup(me.Gid)
	if owner != me.Username {
		t.Errorf("resolveUser(%s) = %q, want %q", me.Uid, owner, me.Username)
	}
}

func TestOwnerOfFallsBackToNumericOnLookupFailure(t *testing.T) {
	origUser, origGroup := lookupUserID, lookupGroupID
	t.Cleanup(func() { lookupUserID, lookupGroupID = origUser, origGroup })
	lookupUserID = func(string) (*user.User, error) { return nil, errors.New("no such user") }
	lookupGroupID = func(string) (*user.Group, error) { return nil, errors.New("no such group") }

	if got := resolveUser(noSuchID); got != noSuchID {
		t.Errorf("resolveUser fallback = %q, want the numeric id %q", got, noSuchID)
	}
	if got := resolveGroup(noSuchID); got != noSuchID {
		t.Errorf("resolveGroup fallback = %q, want the numeric id %q", got, noSuchID)
	}
}

// A real file's owner comes from its syscall.Stat_t, not a fabricated one --
// this is the one test that goes through ownerOf itself rather than the
// resolve helpers directly.
func TestOwnerOfReadsARealFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/f"
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	me, err := user.Current()
	if err != nil {
		t.Skipf("no current user in this environment: %v", err)
	}
	owner, _ := ownerOf(info)
	if owner != me.Username {
		t.Errorf("ownerOf a file this test just created = %q, want the current user %q", owner, me.Username)
	}
}
