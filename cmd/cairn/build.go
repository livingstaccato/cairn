// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/livingstaccato/cairn/internal/build"
	"github.com/livingstaccato/cairn/internal/obs"
)

// DefaultConfigFile is the config cairn reads when --config is not given.
const DefaultConfigFile = "cairn.yaml"

func newBuildCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Write indexes for every directory the config covers",
		Long: "Generates index.html, index.json, index.csv and SHA256SUMS for every\n" +
			"directory the config covers.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBuild(configPath, cmd.ErrOrStderr())
		},
	}
	// pflag reads a single dash as shorthand, so the stdlib flag package's
	// "-config" is no longer accepted; -c is the shorthand, --config the name.
	cmd.Flags().StringVarP(&configPath, "config", "c", DefaultConfigFile, "path to the root cairn.yaml")
	return cmd
}

// runBuild loads the config and runs one build.
//
// Diagnostics go to the supplied writer, which is stderr in production: stdout
// stays clean so a caller can pipe generated output without filtering log lines
// out of it.
func runBuild(configPath string, stderr io.Writer) error {
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

	res, err := build.Run(cfg, rootDir, outDir, log)
	if err != nil {
		log.Error("build failed", "err", err)
		return err
	}

	// protected is reported even at zero: an operator whose glob is wider than
	// they meant otherwise sees a directory with no listing and no reason why.
	log.Info("build complete",
		"directories", res.Dirs, "files", res.Files, "outputs", len(res.Written),
		"pruned", res.Pruned, "protected", res.Protected)
	return nil
}
