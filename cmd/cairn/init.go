// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// starterConfig is what init writes.
//
// Every value here is a decision a newcomer should not have to make on their
// first run, and one of them is not obvious. present: defaults to styled, which
// themes with a site and therefore needs the Hugo module; asked for in direct
// mode it writes no HTML at all and logs a warning. A first build that produces
// no browsable page is the wrong first impression, so bare is set explicitly and
// says why.
//
// The comments are the point as much as the values. This file is the first
// thing an operator edits, and the decoder refuses a key it does not know, so
// the names have to be right in front of them.
const starterConfig = `# cairn.yaml — see https://github.com/livingstaccato/cairn
version: 1

# The tree to index, and where the indexes go. Point both at the same directory
# and the indexes land beside the files they describe, which is what a mirror
# usually wants.
root: ./tree
out:  ./site

defaults:
  # bare renders HTML with no JavaScript and no theme, so it works with just
  # this binary. styled is prettier and needs the Hugo module; in direct mode it
  # writes no HTML at all.
  present:  bare

  # html is the page; json, csv and txt are the same listing for scripts; sums
  # writes SHA256SUMS in the format sha256sum -c reads.
  outputs:  [html, json, csv, txt, sums]

  # Digest every file so SHA256SUMS has something to say. Drop to none on a tree
  # where hashing everything is too slow to be worth it.
  checksum: sha256

# Per-path overrides, most specific last. Delete this if you do not need it.
#
# rules:
#   - match: "bootstrap/**"
#     recursive: true
#     outputs:   [html, json, csv, sums]
`

func newInitCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   cmdInit,
		Short: "Write a starter cairn.yaml",
		Long: "Writes a commented cairn.yaml that builds as it stands. It refuses to\n" +
			"replace a config that is already there.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit(configPath, cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", DefaultConfigFile, "path to write the config to")
	return cmd
}

// runInit writes the starter config, and refuses to replace one.
//
// Refusing rather than overwriting is the same promise emit.Writer makes about
// every other file: cairn does not replace what it did not put there. A config
// is the one file in a cairn tree that is entirely the operator's, and losing a
// tuned one to a mistyped command would be the worst version of this.
func runInit(configPath string, stderr io.Writer) error {
	if _, err := os.Lstat(configPath); err == nil {
		return fmt.Errorf("refusing to replace %s: it already exists", configPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check %s: %w", configPath, err)
	}

	// #nosec G306 -- a config file, readable by whatever runs the build.
	if err := os.WriteFile(configPath, []byte(starterConfig), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}
	_, _ = fmt.Fprintf(stderr, "wrote %s\n", configPath)
	_, _ = fmt.Fprintf(stderr, "put files under ./tree, then: cairn build && cairn serve\n")
	return nil
}
