// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"os"
	"syscall"
)

// stopSignals is what every command claims the disposition of: a person's
// Ctrl-C and an orchestrator's ordinary stop.
//
// SIGTERM matters as much as SIGINT and for the same reason. It is what
// `docker stop`, `systemctl stop` and a Kubernetes pod shutdown send, so a
// long-running `watch --serve` takes SIGTERM on every routine stop it will
// ever see. Registered for neither, the Go runtime's default action for
// SIGTERM ends the process where it stands -- mid-write, past the point
// SavePartial would have recorded what the build had already produced, and
// with an exit code that reads as a crash rather than a stop.
var stopSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}

// stopOnCancel releases stop the moment ctx is cancelled, instead of leaving
// that to the caller's own deferred call at the end of the command.
//
// signal.NotifyContext cancels ctx on the first signal it sees but does not
// itself stop intercepting the signal — that only happens when stop is
// called. build and check run a single pass with no loop of their own to
// notice ctx.Done(), so left to a deferred stop() the registration stays
// claimed for the whole run: a second Ctrl-C during a build or check that
// hangs, loops or is simply slow has nothing to reach but a channel nobody
// is draining anymore, instead of the OS default action that would actually
// end the process. Calling stop as soon as the first signal lands hands SIGINT's
// disposition back immediately, so a second Ctrl-C during that run terminates
// it the way it would have before this package ever registered for the signal.
func stopOnCancel(ctx context.Context, stop func()) {
	go func() {
		<-ctx.Done()
		stop()
	}()
}
