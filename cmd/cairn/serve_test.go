// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// serve shows what is on disk. It builds nothing first: a command that built
// before serving would show a directory that looked right in the browser and
// wrong to whatever published it.
func TestServeServesTheOutputDirectory(t *testing.T) {
	configPath, _ := fixture(t)
	var stderr strings.Builder
	if err := runBuild(configPath, "", false, &stderr); err != nil {
		t.Fatalf("%v, stderr:\n%s", err, stderr.String())
	}

	// The server logs from its own goroutine while this one reads, so its
	// diagnostics need a writer that can take both.
	logged := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	// Port 0: the kernel picks, so a test never fights another process for a
	// fixed one. Everywhere else a taken port is an error, never a fallback.
	go func() { done <- runServe(ctx, configPath, "127.0.0.1:0", logged) }()

	base := waitForListener(t, logged)
	resp, err := http.Get(base + "/bootstrap/index.json") //nolint:noctx // bounded by the test's own deadline
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET index.json = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "bootstrap.sh") {
		t.Errorf("the listing does not name the file it indexes: %s", body)
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("runServe: %v", err)
	}
}

// waitForListener reads the bound address out of the server's own log line,
// which is the only place it exists when the kernel chose the port.
func waitForListener(t *testing.T, log *syncBuffer) string {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if addr := boundAddr(log.String()); addr != "" {
			return "http://" + addr
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the server never reported an address:\n%s", log.String())
	return ""
}

// syncBuffer takes writes from one goroutine while another reads.
type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func boundAddr(logged string) string {
	const key = "addr="
	i := strings.Index(logged, key)
	if i < 0 {
		return ""
	}
	rest := logged[i+len(key):]
	if end := strings.IndexAny(rest, " \n"); end >= 0 {
		rest = rest[:end]
	}
	return strings.Trim(rest, `"`)
}

func TestServeMissingConfigFails(t *testing.T) {
	var stderr strings.Builder
	err := runServe(context.Background(), filepath.Join(t.TempDir(), "nope.yaml"), "127.0.0.1:0", &stderr)
	if err == nil {
		t.Fatal("missing config must be an error")
	}
}

func TestServeRejectsPositionalArgs(t *testing.T) {
	if _, err := exec(t, cmdServe, "somewhere"); err == nil {
		t.Fatal("serve must reject positional arguments")
	}
}

func TestRootCommandHasServe(t *testing.T) {
	for _, c := range newRootCmd().Commands() {
		if c.Name() != cmdServe {
			continue
		}
		if c.Flags().Lookup("addr") == nil {
			t.Error("serve has no --addr flag")
		}
		return
	}
	t.Error("root command has no serve subcommand")
}
