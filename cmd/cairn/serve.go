// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/livingstaccato/cairn/internal/obs"
	"github.com/livingstaccato/cairn/internal/serve"
)

func newServeCmd() *cobra.Command {
	var configPath, addr string

	cmd := &cobra.Command{
		Use:   cmdServe,
		Short: "Serve the generated output over HTTP",
		Long: "Serves the output directory so the generated listings can be read in a\n" +
			"browser without Hugo or a web server. Runs until interrupted.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			return runServe(ctx, configPath, addr, cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", DefaultConfigFile, "path to the root cairn.yaml")
	cmd.Flags().StringVar(&addr, "addr", serve.DefaultAddr, "address to listen on")
	return cmd
}

// runServe serves the configured output directory.
//
// Nothing is built first. serve shows what is on disk, which is the question it
// exists to answer — running a build here would mean a directory that looked
// right in the browser and wrong to whatever published it.
func runServe(ctx context.Context, configPath, addr string, stderr io.Writer) error {
	log, shutdown, err := obs.Setup(ctx, "cairn", stderr)
	if err != nil {
		return err
	}
	defer func() {
		if err := shutdown(context.WithoutCancel(ctx)); err != nil {
			_, _ = fmt.Fprintln(stderr, "cairn: telemetry shutdown:", err)
		}
	}()

	_, _, outDir, err := loadPaths(configPath)
	if err != nil {
		log.Error("could not load config", "err", err)
		return err
	}

	s := &serve.Server{Dir: outDir, Addr: addr, Log: log}
	return s.Run(ctx)
}
