// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package obs

import (
	"bytes"
	"context"
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
