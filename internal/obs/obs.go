// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

// Package obs is the only place cairn imports its telemetry library.
//
// Everything else takes a *slog.Logger, which is standard library. That keeps
// the dependency at one file and one import: a consumer vendoring cairn's Hugo
// module, or a test, never pulls a telemetry stack in behind it, and swapping
// the backend touches nothing but this file.
package obs

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	telemetry "github.com/provide-io/provide-telemetry/go"
)

// Setup initialises telemetry and returns a named logger plus the shutdown
// function the caller must run before exiting. The writer is explicit so a test
// can capture output and a CLI can send it to stderr, keeping stdout free for
// anything a caller might pipe.
func Setup(ctx context.Context, name string, w io.Writer) (*slog.Logger, func(context.Context) error, error) {
	if w == nil {
		return nil, nil, fmt.Errorf("obs.Setup: writer is nil")
	}
	if _, err := telemetry.SetupTelemetry(telemetry.WithLogOutput(w)); err != nil {
		return nil, nil, fmt.Errorf("set up telemetry: %w", err)
	}
	return telemetry.GetLogger(ctx, name), telemetry.ShutdownTelemetry, nil
}

// Discard returns a logger that drops everything, for tests and for callers
// that have not set telemetry up. Returning this rather than accepting a nil
// *slog.Logger means no call site needs a nil check before it logs.
func Discard() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
