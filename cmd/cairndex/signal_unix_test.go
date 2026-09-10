// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

//go:build unix

// A one-shot build or check no longer has an otherwise ctx-unaware walk to
// worry about — build.Run and verify.Run both check ctx between directories
// now — but this still has to prove the one thing that check alone cannot:
// registering for SIGINT claims the process's default disposition for it,
// so a Ctrl-C reaches ctx cancellation instead of killing the process
// outright before it can finish writing its manifest. That claim can only
// be observed by actually raising the signal and seeing whether the process
// is still alive afterward.
//
// Whether the command finishes before its own per-directory ctx check ever
// runs, or is cancelled by it, both prove the process survived — this
// fixture is one directory, fast enough that a real SIGINT's delivery time
// is not reliably slower than the whole build. internal/build's own
// TestCancelledCtxStopsTheWalkAndSavesAPartialManifest proves the
// cancel-mid-walk behavior deterministically, with a hook instead of a
// signal in the wall-clock; this proves survival either way.

package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/livingstaccato/cairndex/internal/build"
)

// TestBuildCommandSurvivesAnInterruptMidBuild is the regression test for the
// gap runBuild and runCheck had that watch and serve did not: neither
// registered any signal handling, so a single Ctrl-C reached the OS default
// action and killed the process outright, mid-write, skipping everything
// SavePartial exists to do.
func TestBuildCommandSurvivesAnInterruptMidBuild(t *testing.T) {
	configPath, _ := fixture(t)

	cmd := newBuildCmd()
	cmd.SetArgs([]string{"--config", configPath})
	survivesAnInterrupt(t, cmd)
}

// TestCheckCommandSurvivesAnInterruptMidCheck is runCheck's half of the same
// gap: --remove-orphaned deletes as it walks, so a Ctrl-C reaching the OS
// default action here is a process killed with no record of what it had
// already removed, on top of skipping the report the rest of the walk would
// have produced.
func TestCheckCommandSurvivesAnInterruptMidCheck(t *testing.T) {
	configPath, _ := fixture(t)
	if err := runBuild(context.Background(), configPath, "", build.Options{}, &strings.Builder{}); err != nil {
		t.Fatalf("could not build the fixture to check: %v", err)
	}

	cmd := newCheckCmd()
	cmd.SetArgs([]string{"--config", configPath})
	survivesAnInterrupt(t, cmd)
}

// survivesAnInterrupt runs cmd and raises SIGINT against this test process
// once RunE has registered its signal handler, then requires cmd to return
// cleanly rather than the process dying.
//
// A signal delivered while nothing in this process has called signal.Notify
// for it keeps the Go runtime's default action, which for SIGINT is
// immediate termination — of this whole test binary, not a recoverable
// failure inside it. Racing that delivery against RunE reaching its own
// registration on wall-clock time alone is not reliable: under load the gap
// between starting the goroutine below and RunE actually getting there
// stretched past every fixed sleep tried, one of them losing to a real
// pre-commit run. afterSignalRegistered removes the race instead of
// shrinking it: the command does not run at all — cmd.Execute is still
// blocked inside it — until this function has heard the hook fire, so
// nothing here is racing a scheduler.
func survivesAnInterrupt(t *testing.T, cmd *cobra.Command) {
	t.Helper()
	survivesSignal(t, cmd, syscall.SIGINT)
}

// survivesSignal is survivesAnInterrupt for one specific signal, so the
// SIGTERM cases can make the same claim without a second copy of the
// registration handshake.
func survivesSignal(t *testing.T, cmd *cobra.Command, sig syscall.Signal) {
	t.Helper()
	var stderr strings.Builder
	cmd.SetErr(&stderr)

	registered := make(chan struct{})
	orig := afterSignalRegistered
	afterSignalRegistered = func() { close(registered) }
	t.Cleanup(func() { afterSignalRegistered = orig })

	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()

	select {
	case <-registered:
	case <-time.After(5 * time.Second):
		t.Fatal("RunE never reached its signal registration")
	}

	if err := syscall.Kill(os.Getpid(), sig); err != nil {
		t.Fatalf("raise %v: %v", sig, err)
	}

	select {
	case err := <-done:
		// nil: the command finished before its next ctx check ran. Canceled:
		// the check caught it first. Either is the process surviving; the OS
		// default action surviving neither would be this test hanging above,
		// killed along with the rest of the binary before this ever runs.
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("command returned %v, want nil or context.Canceled, stderr:\n%s", err, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("command never returned")
	}
}

// TestBuildCommandSurvivesATerminationMidBuild is the SIGTERM half of the
// same claim. SIGTERM is what `docker stop` and a Kubernetes pod shutdown
// send, so a container running any of these commands takes SIGTERM on every
// ordinary stop -- and a signal nothing has registered for keeps the Go
// runtime's default action, which for SIGTERM is immediate termination.
// Mid-build that skips everything SavePartial exists to do, exactly as an
// unregistered SIGINT did.
func TestBuildCommandSurvivesATerminationMidBuild(t *testing.T) {
	configPath, _ := fixture(t)

	cmd := newBuildCmd()
	cmd.SetArgs([]string{"--config", configPath})
	survivesSignal(t, cmd, syscall.SIGTERM)
}

// TestCheckCommandSurvivesATerminationMidCheck is runCheck's half, for the
// same reason its SIGINT twin exists: --remove-orphaned deletes as it walks.
func TestCheckCommandSurvivesATerminationMidCheck(t *testing.T) {
	configPath, _ := fixture(t)
	if err := runBuild(context.Background(), configPath, "", build.Options{}, &strings.Builder{}); err != nil {
		t.Fatalf("could not build the fixture to check: %v", err)
	}

	cmd := newCheckCmd()
	cmd.SetArgs([]string{"--config", configPath})
	survivesSignal(t, cmd, syscall.SIGTERM)
}
