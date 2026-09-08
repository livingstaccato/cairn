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
