// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Progress reporting: a build long enough to matter says how far it has
// gotten, throttled so a short one never logs a line for it at all.

package build

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// progressInterval: 0 disables the throttle, so every directory reports —
// deterministic, unlike racing wall-clock time against however long the
// walk takes.
func TestBuildReportsProgressWhenUnthrottled(t *testing.T) {
	orig := progressInterval
	progressInterval = 0
	defer func() { progressInterval = orig }()

	root, out := tree(t), t.TempDir()
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if _, err := Run(context.Background(), conf(nil), root, out, log); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "build in progress") {
		t.Errorf("no progress reported:\n%s", buf.String())
	}
}

// The default interval is long enough that a build finishing in
// milliseconds — every test fixture in this package — never crosses it, so
// "build in progress" never appears beside the final "build complete".
func TestBuildDoesNotReportProgressForAFastBuild(t *testing.T) {
	orig := progressInterval
	progressInterval = time.Hour
	defer func() { progressInterval = orig }()

	root, out := tree(t), t.TempDir()
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if _, err := Run(context.Background(), conf(nil), root, out, log); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "build in progress") {
		t.Errorf("a fast build should not have crossed the interval:\n%s", buf.String())
	}
}
