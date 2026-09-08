// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/livingstaccato/cairndex/internal/obs"
	"github.com/livingstaccato/cairndex/internal/verify"
)

// ErrNotIntact is returned when a check finds something wrong, so the process
// exits non-zero. A verification that reports a damaged mirror and succeeds is
// a verification no deployment script can act on.
var ErrNotIntact = errors.New("the published tree is not intact")

func newCheckCmd() *cobra.Command {
	var configPath string
	var removeOrphaned bool

	cmd := &cobra.Command{
		Use:   cmdCheck,
		Short: "Verify a published tree against what cairndex recorded",
		Long: "Re-hashes every file SHA256SUMS names, reports what the manifest\n" +
			"claims and the disk no longer has, and finds output cairndex does not own.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Claims SIGINT's disposition, the same reason build's RunE
			// does: --remove-orphaned deletes as it goes, and a Ctrl-C
			// reaching the OS default action mid-removal is a process
			// killed with no record of what it had already removed.
			// stopOnCancel hands the disposition back as soon as ctx is
			// cancelled rather than holding it for the whole run, so a
			// check this cancellation does not stop on its own still has a
			// second Ctrl-C left to end it.
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			stopOnCancel(ctx, stop)
			afterSignalRegistered()
			return runCheck(ctx, configPath, removeOrphaned, cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", DefaultConfigFile, "path to the root cairndex.yaml")
	cmd.Flags().BoolVar(&removeOrphaned, "remove-orphaned", false,
		"delete unowned output whose content shows cairndex wrote it; a file that only "+
			"wears a generated name is kept and still reported. Refuses outright when "+
			"the manifest claims nothing, since everything looks unowned then")
	return cmd
}

// runCheck verifies one tree and reports what it found.
//
// Nothing is repaired. An operator unsure about a mirror needs to know what
// changed before anything touches it, and a command that fixes what it finds
// cannot be run to answer that question.
func runCheck(ctx context.Context, configPath string, removeOrphaned bool, stderr io.Writer) error {
	log, shutdown, err := obs.Setup(ctx, "cairndex", stderr)
	if err != nil {
		return err
	}
	defer func() {
		if err := shutdown(context.WithoutCancel(ctx)); err != nil {
			_, _ = fmt.Fprintln(stderr, "cairndex: telemetry shutdown:", err)
		}
	}()

	cfg, rootDir, outDir, err := loadPathsForBuild(configPath)
	if err != nil {
		log.Error("could not load config", "err", err)
		return err
	}

	rep, err := verify.Run(ctx, cfg, rootDir, outDir, log)
	if err != nil {
		log.Error("check failed", "err", err)
		return err
	}

	// Every finding is listed, not counted. A count tells an operator that
	// something is wrong and nothing about which file to look at.
	report(log, "a file cairndex recorded is gone", rep.Missing)
	report(log, "a file no longer matches its recorded digest", rep.Modified)
	// Orphans are reported after the removal when one was asked for, so what is
	// logged as an error is what an operator still has to act on. Reporting them
	// first made a --remove-orphaned run that cleaned up perfectly emit one
	// error per orphan and then exit zero, so severity and exit status said
	// opposite things about the same run.
	if !removeOrphaned {
		report(log, "output cairndex does not own", rep.Orphaned)
	}
	report(log, "generated output no longer holds what cairndex wrote", rep.Altered)

	if removeOrphaned {
		if err := removeAndReport(ctx, log, outDir, rep); err != nil {
			return err
		}
	}

	log.Info("check complete",
		"checked", rep.Checked, "compared", rep.Compared, "missing", len(rep.Missing),
		"modified", len(rep.Modified), "orphaned", len(rep.Orphaned),
		"altered", len(rep.Altered))
	if !rep.OK() {
		return ErrNotIntact
	}
	return nil
}

// removeAndReport deletes the unowned output and says what became of it.
//
// Everything it removed is named, including the directories the removals left
// empty: a directory that disappears with no record is one nobody can account
// for afterwards, and in a mirror it is part of the published artifact tree.
//
// What it kept is reported as an error, because a kept path still fails the
// check. The name says cairndex could have written it and the bytes say nothing
// did, which is a collision only a person can settle.
func removeAndReport(ctx context.Context, log *slog.Logger, outDir string, rep *verify.Report) error {
	res, err := verify.RemoveOrphaned(ctx, outDir, rep)
	// Logged before the error is handled: a removal that failed part way through
	// has already deleted files, and an operator needs that list more than they
	// need the error on its own.
	for _, p := range res.Removed {
		log.Info("removed output cairndex does not own", "path", p)
	}
	for _, p := range res.RemovedDirs {
		log.Info("removed a directory the removals left empty", "path", p)
	}
	report(log, "kept: nothing in its content shows cairndex wrote it", res.Kept)
	if err != nil {
		log.Error("could not remove unowned output", "err", err)
		return err
	}
	// Only what was actually deleted is dealt with.
	rep.Orphaned = res.Kept
	return nil
}

func report(log *slog.Logger, msg string, paths []string) {
	for _, p := range paths {
		log.Error(msg, "path", p)
	}
}
