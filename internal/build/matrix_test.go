// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

// A sweep over the settings a config can actually express.
//
// Every enum value already appeared in some test, and that was not enough: a
// silent no-op lived in the mode/present/outputs combination for the entire
// life of the project, because direct mode and present: styled were each
// covered alone and never together. Individually-tested values say nothing
// about the pairs.

package build

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/obs"
)

// outputFile names the file a format is supposed to produce in a directory.
var outputFile = map[string]string{
	config.OutputHTML:   "index.html",
	config.OutputJSON:   "index.json",
	config.OutputCSV:    "index.csv",
	config.OutputText:   "index.txt",
	config.OutputSums:   "SHA256SUMS",
	config.OutputPEP503: "index.html",
}

// TestMatrixEveryRequestedOutputAppearsOrIsExplained is the contract: cairn
// writes what the config asked for, or says why it did not. A format that
// silently produces nothing is the defect this sweep exists to catch.
func TestMatrixEveryRequestedOutputAppearsOrIsExplained(t *testing.T) {
	modes := []string{config.ModeDirect, config.ModeHugo}
	presents := []string{config.PresentBare, config.PresentStyled}
	formats := []string{
		config.OutputHTML, config.OutputJSON, config.OutputCSV,
		config.OutputText, config.OutputSums,
	}
	checksums := []string{config.ChecksumNone, config.ChecksumSHA256}

	for _, mode := range modes {
		for _, present := range presents {
			for _, format := range formats {
				for _, sum := range checksums {
					name := strings.Join([]string{mode, present, format, sum}, "/")
					t.Run(name, func(t *testing.T) {
						assertOutputOrReason(t, mode, present, format, sum)
					})
				}
			}
		}
	}
}

func assertOutputOrReason(t *testing.T, mode, present, format, sum string) {
	t.Helper()
	root, out := tree(t), t.TempDir()

	c := conf(nil)
	c.Mode = mode
	outs := []string{format}
	c.Defaults = config.Override{
		Present: &present, Outputs: &outs, Checksum: &sum,
	}

	var log bytes.Buffer
	if _, err := Run(c, root, out, slog.New(slog.NewTextHandler(&log, nil))); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	// bootstrap/linux holds two files, so every format has something to say
	// about it whatever the checksum setting.
	want := filepath.Join(out, "bootstrap", "linux", outputFile[format])
	if _, err := os.Stat(want); err == nil {
		return
	}

	// Hugo renders the HTML from _index.md, so cairn writing none is correct
	// there — the page still exists.
	if mode == config.ModeHugo && format == config.OutputHTML {
		if _, err := os.Stat(filepath.Join(out, "bootstrap", "linux", "_index.md")); err != nil {
			t.Errorf("hugo mode wrote neither index.html nor _index.md: %v", err)
		}
		return
	}

	// SHA256SUMS with nothing hashed is an empty file nobody can verify
	// against, so not writing it is right.
	if format == config.OutputSums && sum == config.ChecksumNone {
		return
	}

	if log.Len() == 0 {
		t.Errorf("%s produced no %s and said nothing about it",
			format, outputFile[format])
		return
	}
	t.Logf("no %s, explained: %s", outputFile[format], strings.TrimSpace(log.String()))
}

// TestMatrixSourceAcrossModes sweeps the producers against both output modes.
// A producer is chosen per rule and the mode is global, so every pair is
// reachable from a config, and none of them had a test.
func TestMatrixSourceAcrossModes(t *testing.T) {
	for _, mode := range []string{config.ModeDirect, config.ModeHugo} {
		for _, source := range []string{config.SourceFS, config.SourcePages, config.SourceManifest} {
			t.Run(mode+"/"+source, func(t *testing.T) {
				root, out := tree(t), t.TempDir()
				c := conf(nil)
				c.Mode = mode
				src := source
				c.Defaults = config.Override{Source: &src}
				if _, err := Run(c, root, out, obs.Discard()); err != nil {
					t.Fatalf("build failed: %v", err)
				}
				// Whatever the producer, the root directory gets a listing.
				name := "index.json"
				if _, err := os.Stat(filepath.Join(out, name)); err != nil {
					t.Errorf("no %s: %v", name, err)
				}
			})
		}
	}
}

// TestMatrixRecursiveAcrossModes covers the pairing that hid a silent no-op:
// recursive: true wrote nothing whatever in hugo mode, and direct mode was the
// only one anybody had run.
func TestMatrixRecursiveAcrossModes(t *testing.T) {
	for _, mode := range []string{config.ModeDirect, config.ModeHugo} {
		for _, present := range []string{config.PresentBare, config.PresentStyled} {
			t.Run(mode+"/"+present, func(t *testing.T) {
				root, out := tree(t), t.TempDir()
				c := conf(nil)
				c.Mode = mode
				yes := true
				p := present
				c.Defaults = config.Override{Recursive: &yes, Present: &p}
				if _, err := Run(c, root, out, obs.Discard()); err != nil {
					t.Fatalf("build failed: %v", err)
				}
				if _, err := os.Stat(filepath.Join(out, "tree.json")); err != nil {
					t.Errorf("recursive: true produced no tree.json: %v", err)
				}
			})
		}
	}
}

// TestHiddenShowDoesNotListCairnState is a trap the in-place deployment walks
// straight into: hidden: show plus root == out would list .cairn-manifest.json
// and .cairn-cache.json as though they were published artifacts, and SHA256SUMS
// would cover two files that change on every run — so it would never settle.
func TestHiddenShowDoesNotListCairnState(t *testing.T) {
	root := tree(t)
	c := conf(nil)
	show := config.HiddenShow
	sum := config.ChecksumSHA256
	outs := []string{config.OutputJSON, config.OutputSums}
	c.Defaults = config.Override{Hidden: &show, Checksum: &sum, Outputs: &outs}

	run(t, c, root, root)
	first := readFile(t, filepath.Join(root, "bootstrap", "linux", "SHA256SUMS"))
	run(t, c, root, root)
	second := readFile(t, filepath.Join(root, "bootstrap", "linux", "SHA256SUMS"))

	// cairn's own state lives at the output root, which is the tree itself here.
	rootListing := readFile(t, filepath.Join(root, "index.json"))
	for _, bad := range []string{".cairn-manifest.json", ".cairn-cache.json"} {
		if strings.Contains(string(rootListing), bad) {
			t.Errorf("the listing shows %s; cairn's own state is not published content", bad)
		}
	}
	if !bytes.Equal(first, second) {
		t.Errorf("SHA256SUMS is not stable across runs:\n%s\nvs\n%s", first, second)
	}
}

func readFile(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p) // #nosec G304 -- test-owned tempdir
	if err != nil {
		t.Fatal(err)
	}
	return b
}
