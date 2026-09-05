// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package build

import (
	"os"
	"path/filepath"
	"testing"
)

// OutRel has to answer the same way whether the two directories arrive as
// absolute paths, as relative ones, or as one of each.
//
// One of each is the arrangement that reaches it in practice: resolveDir joins
// a relative root: onto a relative config base and returns it still relative,
// while an absolute out: is honoured as written and returns absolute. Compared
// raw, filepath.Rel refuses the pair and the error is indistinguishable from
// "the output is not inside the tree" — so the walk stops skipping the output
// subtree and the build indexes what the previous build wrote.
func TestOutRelResolvesBeforeComparing(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// After Chdir because on macOS the temp directory is reached through a
	// symlink, and only the working directory reported from inside it lines up
	// with what filepath.Abs will produce.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	rootAbs := filepath.Join(wd, "tree")
	outAbs := filepath.Join(wd, "tree", "site")

	cases := []struct {
		name      string
		root, out string
		want      string
	}{
		{"both relative", "tree", filepath.Join("tree", "site"), "site"},
		{"both absolute", rootAbs, outAbs, "site"},
		{"relative root, absolute out", "tree", outAbs, "site"},
		{"absolute root, relative out", rootAbs, filepath.Join("tree", "site"), "site"},
		{"mixed, mirror", "tree", rootAbs, ""},
		{"mixed, separate trees", "tree", filepath.Join(wd, "site"), ""},
		{"mixed, out holds root", "tree", wd, ""},
		{"mixed, sibling sharing a prefix", "tree", filepath.Join(wd, "tree-old"), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := OutRel(c.root, c.out); got != c.want {
				t.Errorf("OutRel(%q, %q) = %q, want %q", c.root, c.out, got, c.want)
			}
		})
	}
}
