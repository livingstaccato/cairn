// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package verify

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/emit"
	"github.com/livingstaccato/cairn/internal/hash"
)

// The stale-output case: csv came out of outputs:, the build stopped writing
// index.csv, and nothing deleted the copy already being served.
func TestUnclaimedGeneratedFileIsOrphaned(t *testing.T) {
	f := mirror(t)
	f.outFile("docs/index.json", "{}")
	f.outFile("docs/index.csv", "name,size\n")
	f.manifest("docs/index.json")

	eq(t, "Orphaned", f.run().Orphaned, []string{"docs/index.csv"})
}

// In a mirror the output tree is the artifact tree, so almost everything under
// it is content cairn must never claim. Only a basename cairn itself writes can
// be an orphan.
func TestMirrorContentIsNotOrphaned(t *testing.T) {
	f := mirror(t)
	f.file("pool/nginx.deb", "deb\n")
	f.file("pool/README", "prose\n")
	f.file("pool/_meta.yaml", "title: pool\n")
	f.outFile("pool/index.json", "{}")
	f.manifest("pool/index.json")

	eq(t, "Orphaned", f.run().Orphaned, nil)
}

// protect: is the operator naming paths another tool owns. cairn never writes
// there and never claims one, so a manifest that does not mention a protected
// path is the documented state, not a finding — and reporting it would fire on
// every run of every repository mirror that uses the feature.
func TestProtectedPathIsNeverOrphaned(t *testing.T) {
	f := mirror(t)
	f.cfg.Protect = []string{"dists/**"}
	f.file("dists/stable/Packages", "Package: nginx\n")
	f.outFile("dists/stable/SHA256SUMS", digest("Package: nginx\n")+"  Packages\n")
	f.outFile("dists/stable/index.json", "{}")
	f.outFile("index.json", "{}")
	f.manifest("index.json")

	r := f.run()
	eq(t, "Orphaned", r.Orphaned, nil)
	// Checkable on purpose: the digest matches, so a verifier that ignored the
	// glob would hash the file and report a count instead of leaving the subtree
	// to the tool that owns it.
	if r.Checked != 0 {
		t.Errorf("Checked = %d: a protected SHA256SUMS belongs to another tool", r.Checked)
	}
	if !r.OK() {
		t.Errorf("a protected subtree produced findings: %+v", r)
	}
}

// The manifest and the hash cache are written outside emit.Writer, so no
// manifest ever claims them. Flagging them would make every verify of every
// intact tree report two orphans.
func TestBookkeepingFilesAreNeverOrphaned(t *testing.T) {
	f := mirror(t)
	f.outFile(hash.CacheFile, "{}")
	f.outFile("docs/"+hash.CacheFile, "{}")
	f.outFile("docs/"+emit.ManifestFile, "[]")
	f.manifest()

	eq(t, "Orphaned", f.run().Orphaned, nil)
}

// Ownership is per path, not per basename: index.json in one directory says
// nothing about index.json in another.
func TestOrphanIsDecidedPerPath(t *testing.T) {
	f := mirror(t)
	f.outFile("a/index.json", "{}")
	f.outFile("b/index.json", "{}")
	f.manifest("a/index.json")

	eq(t, "Orphaned", f.run().Orphaned, []string{"b/index.json"})
}

// The generated set follows the config, so a renamed index_basename changes
// which names cairn would write and therefore which files it could own.
func TestGeneratedSetFollowsIndexBasename(t *testing.T) {
	f := mirror(t)
	f.cfg.IndexBasename = "listing"
	f.outFile("listing.json", "{}")
	f.outFile("index.json", "{}") // not a name this config generates
	f.manifest()

	eq(t, "Orphaned", f.run().Orphaned, []string{"listing.json"})
}

// hugo mode writes _index.md; direct mode does not, so the same file is an
// orphan in one and someone's content in the other.
func TestGeneratedSetFollowsMode(t *testing.T) {
	f := mirror(t)
	f.outFile(emit.HugoContentFile, "---\n")
	f.manifest()
	eq(t, "Orphaned", f.run().Orphaned, nil)

	f.cfg.Mode = config.ModeHugo
	eq(t, "Orphaned", f.run().Orphaned, []string{emit.HugoContentFile})
}

// A symlink wearing a generated name is not something cairn wrote, and nothing
// claims it. It is exactly the "what is here that cairn does not own" case, so
// it is reported rather than followed.
func TestUnclaimedSymlinkWearingAGeneratedNameIsOrphaned(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privilege on windows")
	}
	f := mirror(t)
	f.file("elsewhere.json", "{}")
	if err := os.Symlink(filepath.Join(f.root, "elsewhere.json"),
		filepath.Join(f.out, "tree.json")); err != nil {
		t.Fatal(err)
	}
	f.manifest()

	eq(t, "Orphaned", f.run().Orphaned, []string{"tree.json"})
}
