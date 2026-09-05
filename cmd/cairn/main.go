// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Command cairn generates browsable, machine-readable directory indexes.
package main

import (
	"os"

	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		// cobra has already printed the message; this only sets the code.
		os.Exit(1)
	}
}

// newRootCmd builds the fully-wired command tree. Tests execute this rather
// than a hand-assembled subset, so what they exercise is what ships.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "cairn",
		Short: "cairn — static directory-index and artifact-repo generator",
		Long: "Walks a tree of files and writes a browsable page, machine-readable\n" +
			"indexes and optional checksums into every directory it covers.",
		SilenceUsage: true,
		// Usage on a bad invocation, not on a build that failed halfway
		// through: the error is the useful output there, not the flag list.
		SilenceErrors: false,
	}
	root.AddCommand(newBuildCmd())
	root.AddCommand(newWatchCmd())
	root.AddCommand(newCheckCmd())
	return root
}
