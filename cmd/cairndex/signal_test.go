// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"testing"
	"time"
)

// stopOnCancel exists so a second Ctrl-C during a build or check that never
// observes ctx has something to reach: signal.NotifyContext cancels ctx on
// the first signal but does not stop intercepting it, so without releasing
// stop early, a second signal is queued into a channel nothing drains again
// instead of falling through to the OS default action that would actually
// end the process.
func TestStopOnCancelReleasesAsSoonAsCtxIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	called := make(chan struct{})
	stopOnCancel(ctx, func() { close(called) })

	cancel()

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("stop was not called promptly after ctx was cancelled")
	}
}

func TestStopOnCancelDoesNothingUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	called := make(chan struct{})
	stopOnCancel(ctx, func() { close(called) })

	select {
	case <-called:
		t.Fatal("stop was called before ctx was cancelled")
	case <-time.After(50 * time.Millisecond):
	}
}
