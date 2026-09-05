// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package config

import (
	"strings"
	"testing"
)

// A mistyped key used to be dropped in silence, so the build ran with defaults
// the operator never asked for and said nothing. Every case here is a config
// that looked accepted and produced the wrong site.
func TestATypoIsRefusedRatherThanIgnored(t *testing.T) {
	cases := map[string]struct{ body, names string }{
		"unknown top-level key": {
			"version: 1\nnonsense: yes\n", "nonsense",
		},
		"typo in defaults": {
			"version: 1\ndefaults:\n  output: [json]\n", "output",
		},
		"typo in a rule's own key": {
			"version: 1\nrules:\n  - mach: \"a/**\"\n", "mach",
		},
		"typo in a rule's inherited key": {
			"version: 1\nrules:\n  - match: \"a/**\"\n    presnt: bare\n", "presnt", // codespell:ignore presnt
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			err := mustFail(t, c.body)
			// The key has to appear, or the operator is told a config they are
			// looking at is wrong without being told which line to look at.
			if !strings.Contains(err.Error(), c.names) {
				t.Errorf("error does not name %q: %v", c.names, err)
			}
		})
	}
}

// checksum was the one setting with no validation at all, and the failure was
// silent in the worst possible direction: outputs: [sums] with a mistyped
// algorithm wrote no SHA256SUMS and exited zero. A mirror published without the
// integrity it was configured for is the thing this project exists to prevent.
func TestAnUnknownChecksumIsRefused(t *testing.T) {
	err := mustFail(t, "version: 1\ndefaults:\n  checksum: sha-256\n")
	if !strings.Contains(err.Error(), "sha-256") || !strings.Contains(err.Error(), ChecksumSHA256) {
		t.Errorf("error should name what was given and what is accepted: %v", err)
	}
	if _, err := Load(writeConfig(t, "version: 1\nrules:\n  - match: \"a/**\"\n    checksum: md5\n")); err == nil {
		t.Error("an unknown checksum in a rule must be refused too")
	}
}

// hidden: was removed and kept declared so it could be refused by name. Strict
// decoding must not swallow that: "field hidden not found" is true and tells an
// operator nothing about where the setting went.
func TestTheRemovedHiddenKeyKeepsItsOwnMessage(t *testing.T) {
	err := mustFail(t, "version: 1\ndefaults:\n  hidden: \"^_\"\n")
	if !strings.Contains(err.Error(), "hide") {
		t.Errorf("the hidden: error must point at hide:, got: %v", err)
	}
}

// The control. Strictness is worth nothing if it also refuses a real config.
func TestEveryDocumentedKeyStillLoads(t *testing.T) {
	body := `
version: 1
mode: direct
root: ./tree
base_path: /mirror
out: ./site
index_basename: index
tree_max_entries: 50000
on_conflict: skip
protect: ["dists/**"]
defaults:
  source: fs
  present: bare
  outputs: [html, json, csv, txt, sums, pep503, search]
  sort: name
  order: asc
  dirs_first: true
  hide: ["**/.*"]
  checksum: sha256
  recursive: true
  max_rendered: 1000
  follow_symlinks: false
rules:
  - match: "bootstrap/**"
    present: bare
    checksum: sha256
`
	if _, err := Load(writeConfig(t, body)); err != nil {
		t.Fatalf("a config using every documented key must load: %v", err)
	}
}

func mustFail(t *testing.T, body string) error {
	t.Helper()
	_, err := Load(writeConfig(t, body))
	if err == nil {
		t.Fatal("expected the config to be refused, got nil")
	}
	return err
}
