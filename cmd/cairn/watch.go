// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/livingstaccato/cairn/internal/build"
	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/obs"
	"github.com/livingstaccato/cairn/internal/watch"
)

func newWatchCmd() *cobra.Command {
	var configPath string
	var settle time.Duration

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Rebuild the directories a change touched, as it happens",
		Long: "Builds once, then watches the indexed tree and rebuilds only the\n" +
			"subtree each change affects. Runs until interrupted.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Ctrl-C ends the watch rather than killing it: the manifest that
			// records what cairn owns is written when a build returns, and a
			// process killed between the write and the save leaves output that
			// nothing claims.
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			return runWatch(ctx, configPath, settle, cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", DefaultConfigFile, "path to the root cairn.yaml")
	cmd.Flags().DurationVar(&settle, "settle", watch.DefaultSettle,
		"how long the tree must be quiet before a rebuild")
	return cmd
}

// runWatch builds once and then watches.
//
// The first build is not optional. A watcher reports changes from the moment it
// starts and knows nothing about what happened before, so starting without it
// would leave whatever was already stale stale until something touched it
// again.
func runWatch(ctx context.Context, configPath string, settle time.Duration, stderr io.Writer) error {
	log, shutdown, err := obs.Setup(ctx, "cairn", stderr)
	if err != nil {
		return err
	}
	defer func() {
		if err := shutdown(context.WithoutCancel(ctx)); err != nil {
			_, _ = fmt.Fprintln(stderr, "cairn: telemetry shutdown:", err)
		}
	}()

	cfg, rootDir, outDir, err := loadPaths(configPath)
	if err != nil {
		log.Error("could not load config", "err", err)
		return err
	}

	if _, err := build.Run(cfg, rootDir, outDir, log); err != nil {
		log.Error("the initial build failed", "err", err)
		return err
	}

	w := &watch.Watcher{
		Config: cfg, Root: rootDir, Out: outDir, Log: log, Settle: settle,
		Rebuild: func(scope string) error {
			_, err := build.RunScoped(cfg, rootDir, outDir, log, scope)
			return err
		},
	}
	if err := w.Run(ctx); err != nil {
		return err
	}
	log.Info("stopped watching")
	return nil
}

// loadPaths reads the config and resolves the two directories a run needs.
func loadPaths(configPath string) (*config.Config, string, string, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, "", "", err
	}
	base := filepath.Dir(configPath)
	return cfg, resolveDir(base, cfg.Root), resolveDir(base, cfg.Out), nil
}

// resolveDir interprets a directory named in the config.
//
// A relative one is resolved against the config's own directory, so a run
// behaves the same wherever it is invoked from. An absolute one is taken as it
// stands — filepath.Join does not honour an absolute second argument, it
// concatenates, so "root: /srv/mirror" under a config in /etc resolved to
// /etc/srv/mirror and the build died with an ENOENT naming a path nobody wrote.
func resolveDir(base, p string) string {
	p = filepath.FromSlash(p)
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
}
