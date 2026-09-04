// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

package emit

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// frontmatter is the shape a Hugo template will read back out of .Params.
type parsedFM struct {
	Title  string `yaml:"title"`
	Layout string `yaml:"layout"`
	Cairn  struct {
		Present string `yaml:"present"`
		Path    string `yaml:"path"`
		Count   int    `yaml:"count"`
		Entries []struct {
			Name  string `yaml:"name"`
			Size  int64  `yaml:"size"`
			IsDir bool   `yaml:"is_dir"`
			Title string `yaml:"title"`
		} `yaml:"entries"`
	} `yaml:"cairn"`
}

func split(t *testing.T, b []byte) (parsedFM, string) {
	t.Helper()
	s := string(b)
	if !strings.HasPrefix(s, "---\n") {
		t.Fatal("must start with a YAML frontmatter fence")
	}
	parts := strings.SplitN(s, "\n---\n", 2)
	if len(parts) != 2 {
		t.Fatal("frontmatter fence not closed")
	}
	var fm parsedFM
	if err := yaml.Unmarshal([]byte(strings.TrimPrefix(parts[0], "---\n")), &fm); err != nil {
		t.Fatalf("frontmatter does not parse: %v", err)
	}
	return fm, parts[1]
}

func TestHugoContentFrontmatter(t *testing.T) {
	b, err := HugoContent(sample(), "Some prose.\n", "styled", "_meta.yaml", "a: b\n")
	if err != nil {
		t.Fatal(err)
	}
	fm, body := split(t, b)

	if !strings.Contains(body, "Some prose.") {
		t.Error("prose must be the page body")
	}
	if fm.Layout != HugoLayout {
		t.Errorf("layout = %q, want %q", fm.Layout, HugoLayout)
	}
	if fm.Cairn.Present != "styled" {
		t.Errorf("present = %q", fm.Cairn.Present)
	}
	if fm.Cairn.Count != 2 || len(fm.Cairn.Entries) != 2 {
		t.Errorf("entries = %d, count = %d, want 2 and 2", len(fm.Cairn.Entries), fm.Cairn.Count)
	}
	if fm.Cairn.Entries[1].Size != 64 {
		t.Errorf("size = %d, want exact bytes preserved through YAML", fm.Cairn.Entries[1].Size)
	}
	if fm.Cairn.Entries[1].Title != "APT sources" {
		t.Errorf("title = %q", fm.Cairn.Entries[1].Title)
	}
	if fm.Title != "linux" {
		t.Errorf("title = %q, want the directory name", fm.Title)
	}
}

func TestHugoContentRootTitle(t *testing.T) {
	l := sample()
	l.Path = "/"
	b, err := HugoContent(l, "", "bare", "", "")
	if err != nil {
		t.Fatal(err)
	}
	fm, _ := split(t, b)
	if fm.Title == "" || fm.Title == "/" || fm.Title == "." {
		t.Errorf("root title = %q, want a readable name", fm.Title)
	}
}

// Prose containing a --- line must not truncate the frontmatter.
func TestHugoContentBodyWithFence(t *testing.T) {
	b, err := HugoContent(sample(), "before\n---\nafter\n", "bare", "", "")
	if err != nil {
		t.Fatal(err)
	}
	_, body := split(t, b)
	if !strings.Contains(body, "before") || !strings.Contains(body, "after") {
		t.Errorf("body mangled: %q", body)
	}
}

// YAML keys must match the JSON contract, since a Hugo template reads
// .Params.cairn.entries with the same names a jq consumer uses.
func TestHugoContentKeysMatchJSONContract(t *testing.T) {
	b, err := HugoContent(sample(), "", "styled", "", "")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, key := range []string{"name:", "path:", "is_dir:", "size:", "modified:", "kind:", "depth:"} {
		if !strings.Contains(s, key) {
			t.Errorf("frontmatter missing %q; keys must match the JSON contract", key)
		}
	}
	if strings.Contains(s, "isdir:") {
		t.Error("YAML lowercased a field name instead of using its tag")
	}
}

func TestHugoContentSourceIsOmittedWhenEmpty(t *testing.T) {
	b, err := HugoContent(sample(), "", "styled", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "source:") {
		t.Error("an absent metadata file must not emit an empty source key")
	}
	b, err = HugoContent(sample(), "", "styled", "_meta.yaml", "a: b\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "source: _meta.yaml") {
		t.Errorf("source not emitted:\n%s", b)
	}
}
