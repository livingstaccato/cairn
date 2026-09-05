// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for turning a window of changed directories into rebuild scopes.

package watch

import (
	"reflect"
	"testing"

	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/obs"
)

func TestCovers(t *testing.T) {
	cases := []struct {
		parent, child string
		want          bool
	}{
		{".", "anything/at/all", true},
		{"", "anything", true},
		{"docs", "docs", true},
		{"docs", "docs/api", true},
		{"docs", "docs/api/v2", true},
		// The one that a prefix test gets wrong: two directories that share
		// spelling and nothing else.
		{"docs", "docs-old", false},
		{"docs", "documents", false},
		{"docs/api", "docs", false},
	}
	for _, tc := range cases {
		if got := Covers(tc.parent, tc.child); got != tc.want {
			t.Errorf("Covers(%q, %q) = %v, want %v", tc.parent, tc.child, got, tc.want)
		}
	}
}

func TestCoalesceDropsScopesInsideOthers(t *testing.T) {
	root := t.TempDir()
	got := Coalesce(conf(nil), root,
		[]string{"docs", "docs/api", "docs/api/v2", "bootstrap"}, obs.Discard())
	want := []string{"bootstrap", "docs"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Coalesce = %v, want %v", got, want)
	}
}

// Sorting alone does not put a parent next to its children: "-" sorts below
// "/", so "docs-old" lands between "docs" and "docs/api". A reduction that only
// compared each scope with the one before it would keep "docs/api".
func TestCoalesceComparesEveryKeptScope(t *testing.T) {
	root := t.TempDir()
	got := Coalesce(conf(nil), root, []string{"docs/api", "docs-old", "docs"}, obs.Discard())
	want := []string{"docs", "docs-old"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Coalesce = %v, want %v", got, want)
	}
}

func TestCoalesceWidensToTheRecursiveOwner(t *testing.T) {
	root := t.TempDir()
	yes := true
	c := conf([]config.Rule{{Match: "bootstrap", Override: config.Override{Recursive: &yes}}})
	got := Coalesce(c, root, []string{"bootstrap/linux"}, obs.Discard())
	if !reflect.DeepEqual(got, []string{"bootstrap"}) {
		t.Errorf("Coalesce = %v, want [bootstrap]", got)
	}
}

// A change the root itself owns cannot be narrowed by anything else in the
// window, and rebuilding both it and a subtree would walk that subtree twice.
func TestCoalesceCollapsesToTheRoot(t *testing.T) {
	root := t.TempDir()
	// "-tmp" sorts below ".", so a reduction that only dropped what a
	// previously kept scope covers would keep both it and the root.
	got := Coalesce(conf(nil), root, []string{"-tmp", ".", "docs"}, obs.Discard())
	if !reflect.DeepEqual(got, []string{"."}) {
		t.Errorf("Coalesce = %v, want [.]", got)
	}
}
