// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for `cairn watch --serve`: one process that rebuilds what changed and
// hands the result to a refresh.

package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The two halves run together: what the initial build wrote is reachable while
// the watcher is still watching.
//
// Polled rather than fetched once, because the socket opens before the build
// runs. A request arriving in that window is a 404 by design — the tree really
// does not have an index yet — and the thing worth asserting is that one turns
// up without anything else being asked of the process.
func TestWatchServeServesWhatItBuilt(t *testing.T) {
	configPath, _ := fixture(t)

	logged := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	// Port 0: the kernel picks, so a test never fights another process for a
	// fixed one. Everywhere else a taken port is an error, never a fallback.
	o := watchOpts{configPath: configPath, settle: 10 * time.Millisecond,
		serve: true, addr: "127.0.0.1:0"}
	go func() { done <- runWatch(ctx, o, logged) }()

	base := waitForListener(t, logged)
	if code := getUntilOK(t, base+"/bootstrap/index.json"); code != http.StatusOK {
		t.Errorf("GET index.json = %d, want 200; log:\n%s", code, logged.String())
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("runWatch: %v, log:\n%s", err, logged.String())
	}
}

// An address already in use is reported before the build, not after it. On a
// large tree the first build is minutes long, and a bind error at the end of it
// reaches somebody who has stopped watching.
func TestWatchServeReportsATakenPortBeforeBuilding(t *testing.T) {
	configPath, out := fixture(t)

	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()

	var stderr strings.Builder
	o := watchOpts{configPath: configPath, settle: time.Millisecond,
		serve: true, addr: held.Addr().String()}
	err = runWatch(context.Background(), o, &stderr)
	if err == nil {
		t.Fatal("a taken port must be an error")
	}
	if !strings.Contains(err.Error(), "cannot listen") {
		t.Errorf("error does not name the problem: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "bootstrap", "index.json")); err == nil {
		t.Error("the build ran before the address was known to be free")
	}
}

// --serve absent means no second half to wait on, and the watch behaves exactly
// as it did before the flag existed.
func TestWatchWithoutServeOpensNoSocket(t *testing.T) {
	configPath, out := fixture(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var stderr strings.Builder
	o := watchOpts{configPath: configPath, settle: 10 * time.Millisecond}
	if err := runWatch(ctx, o, &stderr); err != nil {
		t.Fatalf("%v, stderr:\n%s", err, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(out, "bootstrap", "index.json")); err != nil {
		t.Errorf("watch did not build: %v", err)
	}
	if strings.Contains(stderr.String(), "serving the output") {
		t.Errorf("a watch without --serve opened a socket: %q", stderr.String())
	}
}

// --addr without --serve is a typo, not a request. Accepting it silently would
// leave an operator waiting for a server that was never asked for.
func TestWatchAddrWithoutServeIsAnError(t *testing.T) {
	if _, err := exec(t, cmdWatch, "--addr", "127.0.0.1:0"); err == nil {
		t.Fatal("--addr without --serve must be an error")
	}
}

func TestWatchHasServeFlags(t *testing.T) {
	for _, c := range newRootCmd().Commands() {
		if c.Name() != cmdWatch {
			continue
		}
		for _, f := range []string{"serve", "addr"} {
			if c.Flags().Lookup(f) == nil {
				t.Errorf("watch has no --%s flag", f)
			}
		}
		return
	}
	t.Error("root command has no watch subcommand")
}

// getUntilOK polls until the build it is waiting on has written the file, and
// reports the last status it saw.
func getUntilOK(t *testing.T, url string) int {
	t.Helper()
	last := 0
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url) //nolint:noctx // bounded by the deadline above
		if err != nil {
			t.Fatal(err)
		}
		last = resp.StatusCode
		_ = resp.Body.Close()
		if last == http.StatusOK {
			return last
		}
		time.Sleep(5 * time.Millisecond)
	}
	return last
}
