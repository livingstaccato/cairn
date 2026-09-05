// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/livingstaccato/cairn/internal/build"
	"github.com/livingstaccato/cairn/internal/config"
	"github.com/livingstaccato/cairn/internal/emit"
	"github.com/livingstaccato/cairn/internal/obs"
	"github.com/livingstaccato/cairn/internal/serve"
	"github.com/livingstaccato/cairn/internal/watch"
)

// watchOpts is what the command line said. Grouped rather than passed one by
// one: the list grew past the point where a reader could tell which string was
// which at the call site.
type watchOpts struct {
	configPath string
	settle     time.Duration
	serve      bool
	addr       string
}

func newWatchCmd() *cobra.Command {
	var o watchOpts

	cmd := &cobra.Command{
		Use:   cmdWatch,
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
			if cmd.Flags().Changed("addr") && !o.serve {
				return fmt.Errorf("--addr names an address to serve on; pass --serve to open it")
			}
			return runWatch(ctx, o, cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVarP(&o.configPath, "config", "c", DefaultConfigFile, "path to the root cairn.yaml")
	cmd.Flags().DurationVar(&o.settle, "settle", watch.DefaultSettle,
		"how long the tree must be quiet before a rebuild")
	cmd.Flags().BoolVar(&o.serve, "serve", false,
		"also serve the output over HTTP, so a rebuild is one refresh away")
	cmd.Flags().StringVar(&o.addr, "addr", serve.DefaultAddr, "address --serve listens on")
	return cmd
}

// runWatch builds once and then watches, optionally serving what it built.
//
// The first build is not optional. A watcher reports changes from the moment it
// starts and knows nothing about what happened before, so starting without it
// would leave whatever was already stale stale until something touched it
// again.
func runWatch(ctx context.Context, o watchOpts, stderr io.Writer) error {
	log, shutdown, err := obs.Setup(ctx, "cairn", stderr)
	if err != nil {
		return err
	}
	defer func() {
		if err := shutdown(context.WithoutCancel(ctx)); err != nil {
			_, _ = fmt.Fprintln(stderr, "cairn: telemetry shutdown:", err)
		}
	}()

	cfg, rootDir, outDir, err := loadPathsForBuild(o.configPath)
	if err != nil {
		log.Error("could not load config", "err", err)
		return err
	}

	// One cancellation covers both halves: whichever stops first takes the
	// other down with it, so a server that loses its socket does not leave a
	// watcher rebuilding a tree nobody can see, and Ctrl-C ends both.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	served, err := startServer(ctx, o, outDir, log)
	if err != nil {
		return err
	}

	if _, err := build.Run(cfg, rootDir, outDir, log); err != nil {
		log.Error("the initial build failed", "err", err)
		return err
	}

	w := &watch.Watcher{
		Config: cfg, Root: rootDir, Out: outDir, Log: log, Settle: o.settle,
		Rebuild: func(scope string) error {
			_, err := build.RunScoped(cfg, rootDir, outDir, log, scope)
			return err
		},
	}
	watched := make(chan error, 1)
	go func() { watched <- w.Run(ctx) }()

	// A nil channel blocks forever, which is what --serve absent should mean
	// here: there is no second half to wait on.
	select {
	case err = <-watched:
		cancel()
		if served != nil {
			<-served
		}
	case err = <-served:
		cancel()
		<-watched
	}
	if err != nil {
		return err
	}
	log.Info("stopped watching")
	return nil
}

// startServer opens the socket before the first build, or returns nil when
// --serve was not asked for.
//
// Before, deliberately. On a large tree the first build is minutes long, and an
// address already in use reported at the end of it is reported to somebody who
// has stopped watching. The output directory is created here for the same
// reason: the server refuses a directory that does not exist, and on a first
// run nothing has made it yet.
func startServer(ctx context.Context, o watchOpts, outDir string, log *slog.Logger) (chan error, error) {
	if !o.serve {
		return nil, nil
	}
	// #nosec G301 -- see emit.DirMode: the served tree must be traversable by
	// whatever reads it.
	if err := os.MkdirAll(outDir, emit.DirMode); err != nil {
		return nil, fmt.Errorf("create output directory %s: %w", outDir, err)
	}

	s := &serve.Server{Dir: outDir, Addr: o.addr, Log: log, Ready: make(chan struct{})}
	served := make(chan error, 1)
	go func() { served <- s.Run(ctx) }()

	select {
	case <-s.Ready:
		log.Info("serving the output", "addr", s.BoundAddr(), "dir", outDir)
		return served, nil
	case err := <-served:
		return nil, err // the bind failed; there is nothing to watch for
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// loadPathsForBuild is loadPaths for the commands that read the indexed tree —
// build, check and watch. serve does not: it hands out a directory that was
// built earlier, and the source tree may not be on that machine at all.
func loadPathsForBuild(configPath string) (*config.Config, string, string, error) {
	cfg, rootDir, outDir, err := loadPaths(configPath)
	if err != nil {
		return nil, "", "", err
	}
	if err := checkRoot(cfg.Root, rootDir); err != nil {
		return nil, "", "", err
	}
	return cfg, rootDir, outDir, nil
}

// checkRoot refuses a root: that is not a directory, before the walk reaches it.
//
// The walk's own message was "read dir .: open /abs/path: no such file or
// directory", which names neither the setting at fault nor the value as it was
// written, and opens with the "." of the directory it was about to read — which
// looks like part of the mistake. This is the first error a new config
// produces, so it should say which line to edit.
func checkRoot(configured, resolved string) error {
	fi, err := os.Stat(resolved)
	if err != nil {
		return fmt.Errorf("root: %s does not exist%s", configured, resolvedAs(configured, resolved))
	}
	if !fi.IsDir() {
		return fmt.Errorf("root: %s is not a directory%s", configured, resolvedAs(configured, resolved))
	}
	return nil
}

// resolvedAs names where a relative root: landed, and says nothing when that is
// the same string the config already showed.
func resolvedAs(configured, resolved string) string {
	// Cleaned, because "./tree" and "tree" are the same directory and repeating
	// it back differently reads like a second, different path.
	if filepath.Clean(configured) == filepath.Clean(resolved) {
		return ""
	}
	return fmt.Sprintf(" (resolved to %s)", resolved)
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
