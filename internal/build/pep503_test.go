// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for the pep503 output: the mixed-directory warning, pep503_level's
// hard validation, and the Hugo-mode flag that tells the template to render
// it instead of the normal listing.

package build

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/livingstaccato/cairndex/internal/config"
	"github.com/livingstaccato/cairndex/internal/obs"
)

// PEP503 renders a directory entry as a normalized project link and a file
// entry as a download, on the assumption that a real listing holds only one
// or the other. Nothing in config enforces that a rule's outputs: [pep503]
// only ever matches a directory shaped that way, so a mixed one — bootstrap
// holds bootstrap.sh and apt.list beside the linux/ subdirectory — has to be
// reported rather than silently rendered as if it were faithful to either
// PEP 503 level.
func TestEmitPEP503WarnsOnAMixedDirectory(t *testing.T) {
	root, out := tree(t), t.TempDir()
	outs := []string{config.OutputPEP503}
	c := conf([]config.Rule{{Match: "bootstrap", Override: config.Override{Outputs: &outs}}})

	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if _, err := Run(context.Background(), c, root, out, log); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(buf.String(), "bootstrap") {
		t.Errorf("expected a warning naming the mixed directory, got %q", buf.String())
	}
}

// pep503_level: root is an explicit, checkable claim, unlike the bare
// outputs: [pep503] case above, which only warns. bootstrap holds files
// beside its linux/ subdirectory, so declaring it root must fail the build
// rather than silently render a page untrue to the claim.
func TestEmitPEP503FailsOnADeclaredLevelMismatch(t *testing.T) {
	root, out := tree(t), t.TempDir()
	outs := []string{config.OutputPEP503}
	level := config.PEP503LevelRoot
	c := conf([]config.Rule{{Match: "bootstrap", Override: config.Override{Outputs: &outs, PEP503Level: &level}}})

	if _, err := Run(context.Background(), c, root, out, obs.Discard()); err == nil {
		t.Fatal("a directory violating its declared pep503_level must fail the build")
	} else if !strings.Contains(err.Error(), "bootstrap") {
		t.Errorf("error should name the offending directory: %v", err)
	}
}

// The same declaration succeeds when the directory actually matches it:
// docs holds only intro.md, a file, no subdirectory — a real project level.
func TestEmitPEP503AcceptsAMatchingDeclaredLevel(t *testing.T) {
	root, out := tree(t), t.TempDir()
	outs := []string{config.OutputPEP503}
	level := config.PEP503LevelProject
	c := conf([]config.Rule{{Match: "docs", Override: config.Override{Outputs: &outs, PEP503Level: &level}}})

	if _, err := Run(context.Background(), c, root, out, obs.Discard()); err != nil {
		t.Fatalf("docs holds only a file, no subdirectory: %v", err)
	}
}

// pep503 in hugo mode: the frontmatter carries the flag the template
// dispatches on, since outputs: is an axis independent of present: that no
// template can otherwise see.
func TestRunHugoModeCarriesPEP503InFrontmatter(t *testing.T) {
	root, out := tree(t), t.TempDir()
	outs := []string{config.OutputPEP503}
	c := conf([]config.Rule{{Match: "docs", Override: config.Override{Outputs: &outs}}})
	c.Mode = config.ModeHugo
	run(t, c, root, out)

	b, err := os.ReadFile(filepath.Join(out, "docs", "_index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "pep503: true") {
		t.Errorf("pep503 did not reach docs's frontmatter:\n%s", b)
	}

	b, err = os.ReadFile(filepath.Join(out, "_index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "pep503") {
		t.Errorf("the root, which did not request pep503, carries it anyway:\n%s", b)
	}
}

// mode: hugo writes pep503.json — the same encoding and digest validation
// PEP503 already gives direct mode's HTML, reused here instead of
// reimplemented in the Hugo template that reads it.
func TestRunHugoModeWritesPEP503JSONWithEncodedHrefs(t *testing.T) {
	root, out := t.TempDir(), t.TempDir()
	// Two different characters that need percent-encoding in a URL, not one:
	// # unconditionally (a URL fragment delimiter) and a space (reserved,
	// widely disallowed unencoded). ? would exercise a third, but unlike
	// these two it is one of the characters NTFS refuses in a filename
	// outright, so a real fixture carrying it never gets this far on
	// Windows — emit.PEP503JSON's own unit test already covers ? directly,
	// against an in-memory model.Entry rather than a real file.
	if err := os.WriteFile(filepath.Join(root, `weird#file name.tar.gz`), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	outs := []string{config.OutputPEP503}
	level := config.PEP503LevelProject
	c := conf(nil)
	c.Defaults.Outputs = &outs
	c.Defaults.PEP503Level = &level
	c.Mode = config.ModeHugo
	run(t, c, root, out)

	b, err := os.ReadFile(filepath.Join(out, "pep503.json"))
	if err != nil {
		t.Fatalf("pep503.json was not written: %v", err)
	}
	if !strings.Contains(string(b), "%23") || !strings.Contains(string(b), "%20") {
		t.Errorf("href was not percent-encoded:\n%s", b)
	}
}

// The same declared-level validation as direct mode applies in hugo mode:
// bootstrap holds files beside its linux/ subdirectory, so declaring it
// root must fail the build there too, not just under mode: direct.
func TestRunHugoModeFailsOnADeclaredLevelMismatch(t *testing.T) {
	root, out := tree(t), t.TempDir()
	outs := []string{config.OutputPEP503}
	level := config.PEP503LevelRoot
	c := conf([]config.Rule{{Match: "bootstrap", Override: config.Override{Outputs: &outs, PEP503Level: &level}}})
	c.Mode = config.ModeHugo

	if _, err := Run(context.Background(), c, root, out, obs.Discard()); err == nil {
		t.Fatal("a directory violating its declared pep503_level must fail the build in hugo mode too")
	} else if !strings.Contains(err.Error(), "bootstrap") {
		t.Errorf("error should name the offending directory: %v", err)
	}
}
