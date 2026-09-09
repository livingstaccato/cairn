// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Command cairndex generates browsable, machine-readable directory indexes.
package main

import (
	"context"
	"errors"
	"os"

	"github.com/spf13/cobra"
)

// sigintExitCode is the shell's own convention (128+signal number, 128+2 for
// SIGINT), not one invented here -- an orchestrator, a wrapper script, or a
// person at a terminal already reads 130 as "this was interrupted," and
// reusing that means cairndex needs no documentation of its own for the
// distinction to be understood.
const sigintExitCode = 130

// Subcommand names, named once so the command tree and the tests that inspect
// it cannot drift apart.
const (
	cmdBuild = "build"
	cmdWatch = "watch"
	cmdCheck = "check"
	cmdServe = "serve"
	cmdInit  = "init"
)

// version is a plain var, not a const, so `ci/build.sh` can override it with
// -ldflags "-X main.version=...". "dev" is what a plain `go build` or `go run`
// reports: a literal version number in source drifts the moment a release ships
// and nobody remembers to bump it, so the tag is the only source of truth.
var version = "dev"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		// cobra has already printed the message; this only sets the code.
		os.Exit(exitCode(err))
	}
}

// exitCode maps an error Execute returned to the process exit code a script
// or orchestrator branches on. build, watch's initial build, and check all
// return ctx.Err() raw and unwrapped on a SIGINT that lands before they are
// done -- unlike watch's own steady-state loop, which already treats
// ctx.Done() as the clean, expected end of "watches until interrupted" and
// returns nil -- so this is the one place left to tell an operator's own
// Ctrl-C apart from a real failure once that distinction reaches here as
// just an error value.
func exitCode(err error) int {
	if errors.Is(err, context.Canceled) {
		return sigintExitCode
	}
	return 1
}

// newRootCmd builds the fully-wired command tree. Tests execute this rather
// than a hand-assembled subset, so what they exercise is what ships.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "cairndex",
		Short: "cairndex — static directory-index and artifact-repo generator",
		Long: "Walks a tree of files and writes a browsable page, machine-readable\n" +
			"indexes and optional checksums into every directory it covers.",
		SilenceUsage: true,
		// Usage on a bad invocation, not on a build that failed halfway
		// through: the error is the useful output there, not the flag list.
		SilenceErrors: false,
		Version:       version,
	}
	root.AddCommand(newBuildCmd())
	root.AddCommand(newWatchCmd())
	root.AddCommand(newCheckCmd())
	root.AddCommand(newServeCmd())
	root.AddCommand(newInitCmd())
	return root
}
