// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/livingstaccato/cairn/internal/build"
)

// A root: that is not there produced "read dir .: open nope: no such file or
// directory" -- which names neither the setting that is wrong nor the path it
// resolved to, and leads with a "." that reads like part of the mistake. It is
// the first error a new config produces and it should say what to edit.
func TestAMissingRootNamesTheSettingAndThePath(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "cairn.yaml")
	body := "version: 1\nroot: ./nope\nout: ./site\n"
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	var stderr strings.Builder
	err := runBuild(configPath, "", build.Options{}, &stderr)
	if err == nil {
		t.Fatal("a build whose root: does not exist must fail")
	}
	for _, want := range []string{"root:", "./nope"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "read dir .") {
		t.Errorf("error still leads with the walk's relative dir: %v", err)
	}
}

// A root: that is a file, not a directory, is the same class of mistake and was
// reported as an opaque syscall error too.
func TestARootThatIsAFileIsRefusedClearly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tree"), []byte("not a directory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "cairn.yaml")
	if err := os.WriteFile(configPath, []byte("version: 1\nroot: ./tree\nout: ./site\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stderr strings.Builder
	err := runBuild(configPath, "", build.Options{}, &stderr)
	if err == nil {
		t.Fatal("a root: pointing at a file must fail")
	}
	if !strings.Contains(err.Error(), "root:") || !strings.Contains(err.Error(), "directory") {
		t.Errorf("error should say root: must be a directory: %v", err)
	}
}
