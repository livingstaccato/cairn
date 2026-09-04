// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package obs

import (
	"bytes"
	"context"
	"runtime/debug"
	"strings"
	"testing"
)

func TestSetupReturnsUsableLoggerAndShutdown(t *testing.T) {
	var buf bytes.Buffer
	ctx := context.Background()

	log, shutdown, err := Setup(ctx, "cairn-test", &buf)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if log == nil {
		t.Fatal("Setup returned a nil logger")
	}
	if shutdown == nil {
		t.Fatal("Setup returned a nil shutdown func")
	}

	log.Info("hello", "count", 1)
	if !strings.Contains(buf.String(), "hello") {
		t.Errorf("log output did not reach the writer: %q", buf.String())
	}

	if err := shutdown(ctx); err != nil {
		t.Errorf("shutdown: %v", err)
	}
}

// A nil writer must be rejected rather than surfacing as a panic on the first
// log line.
func TestSetupRejectsNilWriter(t *testing.T) {
	if _, _, err := Setup(context.Background(), "cairn-test", nil); err == nil {
		t.Fatal("expected an error for a nil writer")
	}
}

func TestDiscardIsUsable(t *testing.T) {
	Discard().Info("this must not panic")
}

// version reports what the build actually carries rather than a constant kept
// in step with a tag by hand, so the test asserts that correspondence. Under
// `go test` the toolchain reports "(devel)"; the empty-version fallback is not
// reachable from a test binary.
func TestVersionReportsTheBuild(t *testing.T) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		t.Skip("no build info in this binary")
	}
	if got := version(); got != info.Main.Version {
		t.Errorf("version() = %q, want %q", got, info.Main.Version)
	}
	if version() == "" {
		t.Error("version() is empty; every log line carries it")
	}
}

// The identity cairn reports when nothing in the environment says otherwise.
// These are the values that appear in a build log, so they are asserted as
// literals: comparing against the constants would pass even if a constant were
// changed to something wrong.
func TestConfigDefaults(t *testing.T) {
	cfg, err := config()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.ServiceName != "cairn" {
		t.Errorf("ServiceName = %q, want %q", cfg.ServiceName, "cairn")
	}
	if cfg.Environment != "production" {
		t.Errorf("Environment = %q, want %q", cfg.Environment, "production")
	}
	if cfg.Version != version() {
		t.Errorf("Version = %q, want %q", cfg.Version, version())
	}
}

// CAIRN_ENVIRONMENT is cairn's own name for this and wins over the telemetry
// library's, which stays honoured so anyone driving that stack directly is not
// cut off. An empty value is not a value: it must not beat the library's.
func TestConfigEnvironmentPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name         string
		cairnEnv     string
		telemetryEnv string
		want         string
	}{
		{"neither set", "", "", "production"},
		{"cairn only", "staging", "", "staging"},
		{"library only", "", "development", "development"},
		{"both set, cairn wins", "staging", "development", "staging"},
		{"cairn empty does not win", "", "development", "development"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CAIRN_ENVIRONMENT", tc.cairnEnv)
			t.Setenv("PROVIDE_TELEMETRY_ENV", tc.telemetryEnv)
			cfg, err := config()
			if err != nil {
				t.Fatalf("config: %v", err)
			}
			if cfg.Environment != tc.want {
				t.Errorf("Environment = %q, want %q", cfg.Environment, tc.want)
			}
		})
	}
}

// Name and version have no cairn-specific variable — the name is an identity
// and the version comes from the build — but the library's own must still
// reach the config, or setting one would silently do nothing.
func TestConfigLibraryVariablesStillApply(t *testing.T) {
	t.Setenv("PROVIDE_TELEMETRY_SERVICE_NAME", "other")
	t.Setenv("PROVIDE_TELEMETRY_VERSION", "9.9.9")
	cfg, err := config()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.ServiceName != "other" {
		t.Errorf("ServiceName = %q, want %q", cfg.ServiceName, "other")
	}
	if cfg.Version != "9.9.9" {
		t.Errorf("Version = %q, want %q", cfg.Version, "9.9.9")
	}
}

// A malformed value has to surface as an error rather than being swallowed
// into a default, which would leave the build logging something untrue.
func TestConfigRejectsMalformedEnvironment(t *testing.T) {
	t.Setenv("PROVIDE_TELEMETRY_STRICT_SCHEMA", "not-a-bool")
	if _, err := config(); err == nil {
		t.Fatal("config accepted a non-boolean PROVIDE_TELEMETRY_STRICT_SCHEMA")
	}
}
