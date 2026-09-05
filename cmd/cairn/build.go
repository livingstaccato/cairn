// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/livingstaccato/cairn/internal/build"
	"github.com/livingstaccato/cairn/internal/obs"
)

// DefaultConfigFile is the config cairn reads when --config is not given.
const DefaultConfigFile = "cairn.yaml"

func newBuildCmd() *cobra.Command {
	var configPath string
	var changedTo string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   cmdBuild,
		Short: "Write indexes for every directory the config covers",
		Long: "Generates index.html, index.json, index.csv and SHA256SUMS for every\n" +
			"directory the config covers.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBuild(configPath, changedTo, dryRun, cmd.ErrOrStderr())
		},
	}
	// pflag reads a single dash as shorthand, so the stdlib flag package's
	// "-config" is no longer accepted; -c is the shorthand, --config the name.
	cmd.Flags().StringVarP(&configPath, "config", "c", DefaultConfigFile, "path to the root cairn.yaml")
	cmd.Flags().StringVar(&changedTo, "changed-to", "",
		"write the outputs this build altered to this file, one per line")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"report what a build would write and remove, and change nothing under out: "+
			"(--changed-to still writes the file it names)")
	return cmd
}

// runBuild loads the config and runs one build.
//
// Diagnostics go to the supplied writer, which is stderr in production: stdout
// stays clean so a caller can pipe generated output without filtering log lines
// out of it.
func runBuild(configPath, changedTo string, dryRun bool, stderr io.Writer) error {
	ctx := context.Background()

	log, shutdown, err := obs.Setup(ctx, "cairn", stderr)
	if err != nil {
		return err
	}
	defer func() {
		if err := shutdown(ctx); err != nil {
			_, _ = fmt.Fprintln(stderr, "cairn: telemetry shutdown:", err)
		}
	}()

	cfg, rootDir, outDir, err := loadPaths(configPath)
	if err != nil {
		log.Error("could not load config", "err", err)
		return err
	}

	run := build.Run
	if dryRun {
		run = build.RunDry
	}
	res, err := run(cfg, rootDir, outDir, log)
	if err != nil {
		log.Error("build failed", "err", err)
		return err
	}

	if err := writeChanged(changedTo, res.Changed); err != nil {
		log.Error("could not write the changed-file list", "path", changedTo, "err", err)
		return err
	}

	// protected is reported even at zero: an operator whose glob is wider than
	// they meant otherwise sees a directory with no listing and no reason why.
	msg := "build complete"
	if dryRun {
		msg = "dry run complete; nothing was written"
	}
	log.Info(msg,
		"directories", res.Dirs, "files", res.Files, "outputs", len(res.Written),
		"unchanged", res.Unchanged, "changed", len(res.Changed),
		"pruned", len(res.Pruned), "protected", res.Protected)
	return nil
}

// writeChanged records the outputs this build altered, one per line.
//
// The format is rsync's --files-from: paths relative to the output directory,
// forward slashes, no leading slash. A mirror of any size republishes almost
// nothing on a typical build, and until now a deployment had no way to know
// that — it re-uploaded every listing because it could not tell which had moved.
//
// Deletions are not in the list. Prune has already removed them from the output
// directory, so a sync of that directory carries them; a --files-from transfer
// does not, and needs its own --delete pass.
//
// Under --dry-run the file is still written, and holds what a real build would
// change. The flag guards the output tree; this path is one the operator named
// on the command line, and previewing a deployment's file list is the reason to
// ask for both at once.
func writeChanged(path string, changed []string) error {
	if path == "" {
		return nil
	}
	body := ""
	if len(changed) > 0 {
		body = strings.Join(changed, "\n") + "\n"
	}
	// #nosec G306 -- a list of generated paths, readable by whatever deploys it.
	return os.WriteFile(path, []byte(body), 0o644)
}
