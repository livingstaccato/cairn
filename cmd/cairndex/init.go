// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/livingstaccato/cairndex/internal/config"
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
const schemaDirective = "# yaml-language-server: $schema=https://raw.githubusercontent.com/livingstaccato/cairndex/main/cairndex.schema.json\n"

// starterConfigDirect is what init writes by default: a config that renders
// its own HTML with no other tool involved.
const starterConfigDirect = schemaDirective + `# cairndex.yaml — see https://github.com/livingstaccato/cairndex
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

// starterConfigHugo is --mode hugo's starter: Hugo renders the HTML from a
// single _index.md per directory, so there is no present:/outputs: to pick —
// see docs/hugo-setup.md. Importing the module is a step this file cannot do
// for you, so it says so rather than writing a config that fails on first
// build with no clue why.
const starterConfigHugo = schemaDirective + `# cairndex.yaml — see https://github.com/livingstaccato/cairndex/blob/main/docs/hugo-setup.md
#
# Import the module first, or the build has nothing to render with:
#   hugo mod get github.com/livingstaccato/cairndex@main
#   go install github.com/livingstaccato/cairndex/cmd/cairndex@main
version: 1
mode: hugo

# The tree to index. out: is content/, not site/: in hugo mode cairndex writes
# one _index.md per directory and Hugo publishes the rest from there.
root: ./tree
out:  ./content
`

// starterFor names the config a mode writes, refusing one it does not
// recognise rather than guessing which the operator meant.
func starterFor(mode string) (string, error) {
	switch mode {
	case "", config.ModeDirect:
		return starterConfigDirect, nil
	case config.ModeHugo:
		return starterConfigHugo, nil
	}
	return "", fmt.Errorf("--mode must be %s or %s, got %q", config.ModeDirect, config.ModeHugo, mode)
}

func newInitCmd() *cobra.Command {
	var configPath, mode string

	cmd := &cobra.Command{
		Use:   cmdInit,
		Short: "Write a starter cairndex.yaml",
		Long: "Writes a commented cairndex.yaml that builds as it stands. It refuses to\n" +
			"replace a config that is already there.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit(configPath, mode, cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", DefaultConfigFile, "path to write the config to")
	cmd.Flags().StringVar(&mode, "mode", config.ModeDirect,
		"starter config to write: "+config.ModeDirect+" or "+config.ModeHugo)
	return cmd
}

// runInit writes the starter config, and refuses to replace one.
//
// Refusing rather than overwriting is the same promise emit.Writer makes about
// every other file: cairndex does not replace what it did not put there. A config
// is the one file in a cairndex tree that is entirely the operator's, and losing a
// tuned one to a mistyped command would be the worst version of this.
//
// The refusal is the create. Looking first and writing second is a gap wide
// enough to lose a config through: two inits in the same directory, or an init
// racing an editor saving cairndex.yaml, both look and both see nothing, and
// O_CREATE|O_TRUNC then lets the later one clobber the earlier. O_EXCL makes the
// question and the answer one operation, so exactly one caller can win it.
//
// Not internal/atomicfile, which replaces a file's contents by rename and would
// do precisely what this must not.
func runInit(configPath, mode string, stderr io.Writer) error {
	content, err := starterFor(mode)
	if err != nil {
		return err
	}
	// #nosec G304,G302 -- the path is the operator's own --config value, and a
	// config file is readable by whatever runs the build.
	f, err := os.OpenFile(configPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("refusing to replace %s: it already exists", configPath)
	}
	if err != nil {
		return fmt.Errorf("create %s: %w", configPath, err)
	}

	// Captured before the write, while f is still the descriptor O_EXCL handed
	// back: an identity to clean up against rather than a path to trust.
	fi, statErr := f.Stat()

	if err := writeStarter(f, content); err != nil {
		// Take the file this run made back out of the way — but only if
		// configPath still names it. O_EXCL made the create exclusive; it says
		// nothing about the moment writeStarter fails, when an editor racing to
		// save the same path could already have replaced it. Removing by path
		// alone there would delete somebody else's config, which is the loss
		// this whole change exists to prevent.
		if statErr == nil {
			removeIfSameFile(configPath, fi)
		}
		return fmt.Errorf("write %s: %w", configPath, err)
	}
	_, _ = fmt.Fprintf(stderr, "wrote %s\n", configPath)
	_, _ = fmt.Fprintf(stderr, "put files under ./tree, then: cairndex build && cairndex serve\n")
	return nil
}

// removeIfSameFile deletes path only when it still names the file fi describes.
//
// Lstat rather than Stat: a symlink placed at path in the meantime is not the
// file this run created either, whatever it points at.
func removeIfSameFile(path string, fi os.FileInfo) {
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(fi, current) {
		return
	}
	_ = os.Remove(path)
}

// writeStarter fills the created file and closes it, reporting whichever of the
// two failed. Close is checked because a buffered write can fail there and
// nowhere else.
func writeStarter(f *os.File, content string) error {
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
