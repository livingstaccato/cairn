// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/livingstaccato/cairn/internal/config"
)

func TestManifestFromCairnYAML(t *testing.T) {
	dir := t.TempDir()
	body := `
source: manifest
entries:
  - name: ubuntu-24.04.iso
    path: /bootstrap/ubuntu-24.04.iso
    title: Ubuntu 24.04 LTS
    summary: Fetched at provision time
    kind: image
    size: 6442450944
    tags: [base-image]
  - name: notes
    path: /bootstrap/notes/
    kind: dir
`
	if err := os.WriteFile(filepath.Join(dir, ".cairn.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got, warns, err := Manifest(dir, config.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Errorf("warnings: %v", warns)
	}
	if len(got) != 2 {
		t.Fatalf("got %v, want 2", names(got))
	}
	byName := map[string]int{}
	for i, e := range got {
		byName[e.Name] = i
	}
	iso := got[byName["ubuntu-24.04.iso"]]
	if iso.Title != "Ubuntu 24.04 LTS" || iso.Size != 6442450944 {
		t.Errorf("entry not read: %+v", iso)
	}
	if iso.Kind != KindImage {
		t.Errorf("Kind = %q, want the declared image", iso.Kind)
	}
	if !got[byName["notes"]].IsDir {
		t.Error("an entry with kind: dir must set IsDir")
	}
}

func TestManifestInfersKindWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".cairn.yaml"),
		[]byte("entries:\n  - name: setup.sh\n    path: /x/setup.sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _, err := Manifest(dir, config.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Kind != KindScript {
		t.Errorf("Kind = %q, want %q inferred from the name", got[0].Kind, KindScript)
	}
}

func TestManifestRequiresNameAndPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".cairn.yaml"),
		[]byte("entries:\n  - title: nameless\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, warns, err := Manifest(dir, config.Defaults())
	if err != nil {
		t.Fatalf("an incomplete entry warns, it does not fail: %v", err)
	}
	if len(got) != 0 || len(warns) != 1 {
		t.Errorf("got %d entries and %d warnings, want 0 and 1", len(got), len(warns))
	}
}

func TestManifestMissingFileIsEmpty(t *testing.T) {
	got, _, err := Manifest(t.TempDir(), config.Defaults())
	if err != nil {
		t.Fatalf("a directory with no .cairn.yaml is empty, not an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want none", names(got))
	}
}

func TestManifestMalformedIsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".cairn.yaml"),
		[]byte("entries: [unclosed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Manifest(dir, config.Defaults()); err == nil {
		t.Fatal("expected an error for malformed YAML")
	}
}
