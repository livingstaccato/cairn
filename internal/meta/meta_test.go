// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

package meta

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/livingstaccato/cairn/internal/model"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAndApply(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "ubuntu.iso", "")
	write(t, dir, "bootstrap.sh", "")
	write(t, dir, "_meta.yaml", `
ubuntu.iso:
  title: Ubuntu 24.04 LTS
  summary: Base image for PXE installs
  tags: [base-image, lts]
  kind: image
bootstrap.sh:
  summary: Entry point
`)

	m, warns, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}

	entries := []model.Entry{
		{Name: "ubuntu.iso", Kind: "image"},
		{Name: "bootstrap.sh", Kind: "script"},
		{Name: "unmentioned.txt", Kind: "doc"},
	}
	got := Apply(entries, m)

	if got[0].Title != "Ubuntu 24.04 LTS" || got[0].Summary != "Base image for PXE installs" {
		t.Errorf("metadata not applied: %+v", got[0])
	}
	if len(got[0].Tags) != 2 {
		t.Errorf("Tags = %v, want 2", got[0].Tags)
	}
	if got[1].Summary != "Entry point" {
		t.Errorf("second entry summary = %q", got[1].Summary)
	}
	if got[2].Title != "" || got[2].Summary != "" {
		t.Errorf("unmentioned entry gained metadata: %+v", got[2])
	}
	// Apply must not mutate its input.
	if entries[0].Title != "" {
		t.Error("Apply mutated the caller's slice")
	}
}

func TestApplyOverridesKindAndExtra(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "blob.bin", "")
	write(t, dir, "_meta.yaml", "blob.bin:\n  kind: image\n  extra:\n    arch: arm64\n")
	m, _, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := Apply([]model.Entry{{Name: "blob.bin", Kind: "other"}}, m)
	if got[0].Kind != "image" {
		t.Errorf("Kind = %q, want the declared image", got[0].Kind)
	}
	if got[0].Extra["arch"] != "arm64" {
		t.Errorf("Extra = %v", got[0].Extra)
	}
}

func TestSidecarBeatsMetaYAML(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "ubuntu.iso", "")
	write(t, dir, "_meta.yaml", "ubuntu.iso:\n  title: From meta\n  summary: Kept\n")
	write(t, dir, "ubuntu.iso.meta.yaml", "title: From sidecar\n")

	m, _, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := Apply([]model.Entry{{Name: "ubuntu.iso"}}, m)
	if got[0].Title != "From sidecar" {
		t.Errorf("Title = %q, want the sidecar to win", got[0].Title)
	}
	if got[0].Summary != "Kept" {
		t.Errorf("Summary = %q, want the _meta.yaml value preserved where the sidecar is silent", got[0].Summary)
	}
}

func TestSidecarAloneWorks(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "x.iso", "")
	write(t, dir, "x.iso.meta.yaml", "title: Only sidecar\nhidden: true\n")
	m, _, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m["x.iso"].Title != "Only sidecar" || !m["x.iso"].Hidden {
		t.Errorf("got %+v", m["x.iso"])
	}
}

func TestMalformedIsFatal(t *testing.T) {
	t.Run("_meta.yaml", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "_meta.yaml", "ubuntu.iso:\n  title: [unclosed\n")
		if _, _, err := Load(dir); err == nil {
			t.Fatal("expected an error for malformed _meta.yaml")
		}
	})
	t.Run("sidecar", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "x.iso", "")
		write(t, dir, "x.iso.meta.yaml", "title: [unclosed\n")
		if _, _, err := Load(dir); err == nil {
			t.Fatal("expected an error for a malformed sidecar")
		}
	})
}

func TestMissingFileKeyWarns(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "_meta.yaml", "ghost.iso:\n  title: Not here\n")
	_, warns, err := Load(dir)
	if err != nil {
		t.Fatalf("a missing file must warn, not fail: %v", err)
	}
	if len(warns) != 1 {
		t.Fatalf("warnings = %v, want exactly 1", warns)
	}
}

func TestLoadMissingDirIsError(t *testing.T) {
	if _, _, err := Load(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected an error for a missing directory")
	}
}

func TestLoadEmptyDir(t *testing.T) {
	m, warns, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 0 || len(warns) != 0 {
		t.Errorf("got %v / %v, want empty", m, warns)
	}
}

func TestProse(t *testing.T) {
	dir := t.TempDir()
	if got, err := Prose(dir); err != nil || got != "" {
		t.Errorf("empty dir prose = %q, err = %v", got, err)
	}
	write(t, dir, "_index.md", "from index")
	if got, _ := Prose(dir); got != "from index" {
		t.Errorf("prose = %q, want _index.md content", got)
	}
	write(t, dir, "README.md", "from readme")
	if got, _ := Prose(dir); got != "from readme" {
		t.Errorf("prose = %q, want README.md to win", got)
	}
}

func TestSourceFile(t *testing.T) {
	dir := t.TempDir()
	if got := SourceFile(dir); got != "" {
		t.Errorf("empty dir = %q, want empty", got)
	}
	write(t, dir, DirConfigFile, "outputs: [json]\n")
	if got := SourceFile(dir); got != DirConfigFile {
		t.Errorf("got %q, want %q", got, DirConfigFile)
	}
	// _meta.yaml wins: it is the file describing the entries, which is what a
	// reader following the link is looking for.
	write(t, dir, DirFile, "x:\n  title: y\n")
	if got := SourceFile(dir); got != DirFile {
		t.Errorf("got %q, want %q", got, DirFile)
	}
}

func TestSourceReadsContents(t *testing.T) {
	dir := t.TempDir()
	if got, err := Source(dir); err != nil || got.Name != "" || got.Text != "" {
		t.Errorf("empty dir = %+v, err %v", got, err)
	}
	write(t, dir, DirFile, "x:\n  title: y\n")
	got, err := Source(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != DirFile || got.Text != "x:\n  title: y\n" {
		t.Errorf("got %+v", got)
	}
}

// A directory description running past the cap is a file to fetch, not
// something to inline into every page that mentions it.
func TestSourceIsCapped(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, DirFile, strings.Repeat("# filler\n", SourceMaxBytes))
	got, err := Source(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Text) != SourceMaxBytes {
		t.Errorf("len = %d, want %d", len(got.Text), SourceMaxBytes)
	}
}

// A sidecar overrides field by field, so it can change one thing without
// restating everything the directory file already said.
func TestSidecarMergesPerField(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "x.iso", "")
	write(t, dir, DirFile, `
x.iso:
  title: From meta
  summary: From meta
  kind: doc
  tags: [a, b]
  weight: 1
  extra:
    from: meta
`)
	write(t, dir, "x.iso"+SidecarSuffix, `
kind: image
weight: 5
hidden: true
extra:
  from: sidecar
`)
	m, _, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := m["x.iso"]

	// Overridden by the sidecar.
	if got.Kind != "image" || got.Weight != 5 || !got.Hidden || got.Extra["from"] != "sidecar" {
		t.Errorf("sidecar did not win per field: %+v", got)
	}
	// Untouched by it.
	if got.Title != "From meta" || got.Summary != "From meta" || len(got.Tags) != 2 {
		t.Errorf("sidecar clobbered fields it said nothing about: %+v", got)
	}
}

func TestApplyHonorsHiddenWeightAndURL(t *testing.T) {
	entries := []model.Entry{
		{Name: "keep.txt", Path: "/keep.txt"},
		{Name: "secret.txt", Path: "/secret.txt"},
		{Name: "talk.md", Path: "/talk.md"},
	}
	m := map[string]FileMeta{
		// Declared, parsed, merged — and until now read by nothing, so writing
		// it in a sidecar did exactly nothing and said so nowhere.
		"secret.txt": {Hidden: true},
		// A real file that should link somewhere else. The bytes stay on disk
		// and keep their size and digest; only where the listing points moves.
		"talk.md":  {URL: "https://youtu.be/example", Title: "The talk"},
		"keep.txt": {Weight: 10},
	}

	got := Apply(entries, m)

	for _, e := range got {
		if e.Name == "secret.txt" {
			t.Error("hidden: true left the entry in the listing")
		}
		if e.Name == "talk.md" {
			if e.Path != "https://youtu.be/example" {
				t.Errorf("url: did not move the link, path = %q", e.Path)
			}
			if e.Title != "The talk" {
				t.Errorf("title lost alongside url:, got %q", e.Title)
			}
		}
		if e.Name == "keep.txt" && e.Weight != 10 {
			t.Errorf("weight: not carried onto the entry, got %d", e.Weight)
		}
	}
	if len(got) != 2 {
		t.Errorf("got %d entries, want 2 after hiding one", len(got))
	}
}
