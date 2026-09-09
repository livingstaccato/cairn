// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

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
// The content is written to a private temp file in the same directory first,
// then os.Link'd into configPath: link(2) is exclusive the same way
// O_CREATE|O_EXCL is — it fails with EEXIST rather than replacing an existing
// dirent — but the exclusive step happens after the content is already known
// good, so there is no window where a partially-written file sits at
// configPath waiting to be cleaned up. An earlier version created configPath
// directly with O_EXCL and, on a write failure, deleted it again by comparing
// os.Lstat's device and inode against what it had just created — safe only if
// a filesystem never hands the same inode back to an unrelated file created
// moments later. It does: on overlay2 (Docker's default storage driver) a
// remove followed immediately by a create in the same directory can reuse the
// freed inode outright, which made that comparison delete a file it did not
// create. Never creating anything at configPath except via the single
// exclusive Link removes the question entirely — there is no longer a "was
// this still my file" check to get wrong.
//
// Not internal/atomicfile, which replaces a file's contents by rename and
// would do precisely what this must not.
func runInit(configPath, mode string, stderr io.Writer) error {
	content, err := starterFor(mode)
	if err != nil {
		return err
	}

	// #nosec G304 -- dir is derived from the operator's own --config value.
	tmp, err := os.CreateTemp(filepath.Dir(configPath), ".cairndex-init-*.tmp")
	if err != nil {
		return fmt.Errorf("create %s: %w", configPath, err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	// os.CreateTemp always uses 0600; configPath is a config file the build
	// itself has to read back, same mode the old O_EXCL create used.
	// #nosec G302 -- a cairndex.yaml is read by whatever runs the build, same
	// as the O_EXCL create this replaced.
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("create %s: %w", configPath, err)
	}
	if err := writeStarter(tmp, content); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}

	if err := os.Link(tmpPath, configPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("refusing to replace %s: it already exists", configPath)
		}
		return fmt.Errorf("create %s: %w", configPath, err)
	}
	_, _ = fmt.Fprintf(stderr, "wrote %s\n", configPath)
	_, _ = fmt.Fprintf(stderr, "put files under ./tree, then: cairndex build && cairndex serve\n")
	return nil
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
