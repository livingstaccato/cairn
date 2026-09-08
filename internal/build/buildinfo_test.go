// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// The build-info footer banner: on by default, carrying Options.Version,
// off with build_info: false.

package build

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/livingstaccato/cairndex/internal/config"
	"github.com/livingstaccato/cairndex/internal/obs"
)

func TestBuildInfoReachesTheBarePage(t *testing.T) {
	root, out := tree(t), t.TempDir()
	outs := []string{config.OutputHTML}
	bare := config.PresentBare
	c := conf([]config.Rule{{Match: "docs", Override: config.Override{Present: &bare, Outputs: &outs}}})

	if _, err := RunWith(context.Background(), c, root, out, obs.Discard(), Options{Version: "v1.2.3"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "docs", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "v1.2.3") {
		t.Errorf("bare page does not carry the build version:\n%s", b)
	}
}

func TestBuildInfoAbsentWithoutAVersion(t *testing.T) {
	root, out := tree(t), t.TempDir()
	outs := []string{config.OutputHTML}
	c := conf([]config.Rule{{Match: "docs", Override: config.Override{Present: strPtr(config.PresentBare), Outputs: &outs}}})

	if _, err := RunWith(context.Background(), c, root, out, obs.Discard(), Options{}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "docs", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "<footer") {
		t.Errorf("a library caller who never set Options.Version got a banner anyway:\n%s", b)
	}
}

func TestBuildInfoFalseTurnsItOff(t *testing.T) {
	root, out := tree(t), t.TempDir()
	outs := []string{config.OutputHTML}
	c := conf([]config.Rule{{Match: "docs", Override: config.Override{Present: strPtr(config.PresentBare), Outputs: &outs}}})
	no := false
	c.BuildInfo = &no

	if _, err := RunWith(context.Background(), c, root, out, obs.Discard(), Options{Version: "v1.2.3"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "docs", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "<footer") {
		t.Errorf("build_info: false did not turn the banner off:\n%s", b)
	}
}

func TestBuildInfoReachesTheHugoFrontmatter(t *testing.T) {
	root, out := tree(t), t.TempDir()
	c := conf(nil)
	c.Mode = config.ModeHugo

	if _, err := RunWith(context.Background(), c, root, out, obs.Discard(), Options{Version: "v1.2.3"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "docs", "_index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "build_info_version: v1.2.3") {
		t.Errorf("hugo frontmatter does not carry the build version:\n%s", b)
	}
}

func strPtr(s string) *string { return &s }
