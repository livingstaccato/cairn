// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

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
	"os"
	"runtime/debug"

	telemetry "github.com/provide-io/provide-telemetry/go"
)

// Identity this binary reports on every log line. Without it the library's own
// defaults appear instead, which name the library rather than this program.
const (
	serviceName = "cairn"

	// A build is a build. cairn runs before Hugo rather than under it, so it
	// cannot read Hugo's --environment.
	serviceEnv = "production"

	// cairn's own knob, spelled like Hugo's HUGO_ENVIRONMENT. The telemetry
	// library has a variable of its own, but naming it here would put the
	// backend into cairn's interface: a user would have to know which library
	// this package imports in order to configure the program, and swapping the
	// backend would break a documented name. This package exists to keep that
	// contained, so the environment is cairn's to name.
	envVar = "CAIRN_ENVIRONMENT"
)

// version reports the module version the binary was built from, so nothing
// here has to be kept in step with a tag by hand. A build from source rather
// than a module has no version to report.
func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return "unknown"
	}
	return info.Main.Version
}

// config resolves telemetry settings, letting the environment win.
//
// SetupTelemetry reads PROVIDE_TELEMETRY_* only when no config is passed to it
// — WithConfig replaces environment loading entirely rather than layering over
// it. So the environment is read here first and cairn fills in only the fields
// it left alone, which keeps the variables working.
func config() (*telemetry.TelemetryConfig, error) {
	cfg, err := telemetry.ConfigFromEnv()
	if err != nil {
		return nil, fmt.Errorf("read telemetry environment: %w", err)
	}
	if os.Getenv("PROVIDE_TELEMETRY_SERVICE_NAME") == "" {
		cfg.ServiceName = serviceName
	}
	// CAIRN_ENVIRONMENT first, the library's own variable second, the default
	// last. ConfigFromEnv has already applied the library's, so this only has
	// to decide whether anything set it.
	switch {
	case os.Getenv(envVar) != "":
		cfg.Environment = os.Getenv(envVar)
	case os.Getenv("PROVIDE_TELEMETRY_ENV") == "":
		cfg.Environment = serviceEnv
	}
	if os.Getenv("PROVIDE_TELEMETRY_VERSION") == "" {
		cfg.Version = version()
	}
	return cfg, nil
}

// Setup initialises telemetry and returns a named logger plus the shutdown
// function the caller must run before exiting. The writer is explicit so a test
// can capture output and a CLI can send it to stderr, keeping stdout free for
// anything a caller might pipe.
func Setup(ctx context.Context, name string, w io.Writer) (*slog.Logger, func(context.Context) error, error) {
	if w == nil {
		return nil, nil, fmt.Errorf("obs.Setup: writer is nil")
	}
	cfg, err := config()
	if err != nil {
		return nil, nil, err
	}
	if _, err := telemetry.SetupTelemetry(
		telemetry.WithConfig(cfg),
		telemetry.WithLogOutput(w),
	); err != nil {
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
