// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

// Command cairn generates browsable, machine-readable directory indexes.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/livingstaccato/cairn/internal/build"
	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/obs"
)

// DefaultConfigFile is the config cairn reads when -config is not given.
const DefaultConfigFile = "cairn.yaml"

const usage = `usage: cairn build [-config cairn.yaml]

Generates index.html, index.json, index.csv and SHA256SUMS for every
directory the config covers.
`

func main() {
	os.Exit(dispatch(os.Args, os.Stderr))
}

// dispatch routes argv to a subcommand. It is separate from main so the exit
// path is testable; main does nothing but hand it os.Args.
func dispatch(argv []string, stderr io.Writer) int {
	if len(argv) < 2 || argv[1] != "build" {
		_, _ = fmt.Fprint(stderr, usage)
		return 2
	}

	fs := flag.NewFlagSet("cairn build", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", DefaultConfigFile, "path to the root cairn.yaml")
	if err := fs.Parse(argv[2:]); err != nil {
		return 2
	}
	return runBuild(*configPath, stderr)
}

// runBuild loads the config and runs one build, returning a process exit code.
//
// Diagnostics go to the supplied writer, which is stderr in production: stdout
// stays clean so a caller can pipe generated output without filtering log lines
// out of it.
func runBuild(configPath string, stderr io.Writer) int {
	ctx := context.Background()

	log, shutdown, err := obs.Setup(ctx, "cairn", stderr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "cairn:", err)
		return 1
	}
	defer func() {
		if err := shutdown(ctx); err != nil {
			_, _ = fmt.Fprintln(stderr, "cairn: telemetry shutdown:", err)
		}
	}()

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Error("could not load config", "err", err)
		return 1
	}

	// root and out are resolved against the config's own directory, so a build
	// behaves the same wherever it is invoked from.
	base := filepath.Dir(configPath)
	rootDir := filepath.Join(base, filepath.FromSlash(cfg.Root))
	outDir := filepath.Join(base, filepath.FromSlash(cfg.Out))

	res, err := build.Run(cfg, rootDir, outDir, log)
	if err != nil {
		log.Error("build failed", "err", err)
		return 1
	}

	log.Info("build complete",
		"directories", res.Dirs, "files", res.Files, "outputs", len(res.Written))
	return 0
}
