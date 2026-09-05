// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/livingstaccato/cairn/internal/obs"
	"github.com/livingstaccato/cairn/internal/verify"
)

// ErrNotIntact is returned when a check finds something wrong, so the process
// exits non-zero. A verification that reports a damaged mirror and succeeds is
// a verification no deployment script can act on.
var ErrNotIntact = errors.New("the published tree is not intact")

func newCheckCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   cmdCheck,
		Short: "Verify a published tree against what cairn recorded",
		Long: "Re-hashes every file SHA256SUMS names, reports what the manifest\n" +
			"claims and the disk no longer has, and finds output cairn does not own.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCheck(cmd.Context(), configPath, cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", DefaultConfigFile, "path to the root cairn.yaml")
	return cmd
}

// runCheck verifies one tree and reports what it found.
//
// Nothing is repaired. An operator unsure about a mirror needs to know what
// changed before anything touches it, and a command that fixes what it finds
// cannot be run to answer that question.
func runCheck(ctx context.Context, configPath string, stderr io.Writer) error {
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

	rep, err := verify.Run(cfg, rootDir, outDir, log)
	if err != nil {
		log.Error("check failed", "err", err)
		return err
	}

	// Every finding is listed, not counted. A count tells an operator that
	// something is wrong and nothing about which file to look at.
	report(log, "a file cairn recorded is gone", rep.Missing)
	report(log, "a file no longer matches its recorded digest", rep.Modified)
	report(log, "output cairn does not own", rep.Orphaned)
	report(log, "generated output no longer holds what cairn wrote", rep.Altered)

	log.Info("check complete",
		"checked", rep.Checked, "compared", rep.Compared, "missing", len(rep.Missing),
		"modified", len(rep.Modified), "orphaned", len(rep.Orphaned),
		"altered", len(rep.Altered))
	if !rep.OK() {
		return ErrNotIntact
	}
	return nil
}

func report(log *slog.Logger, msg string, paths []string) {
	for _, p := range paths {
		log.Error(msg, "path", p)
	}
}
