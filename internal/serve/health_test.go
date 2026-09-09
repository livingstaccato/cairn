// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for /healthz and /metrics: off by default, reserved only once an
// operator asks for them, and reporting the build loop behind a watch
// --serve server rather than just "the process answered".
package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/livingstaccato/cairndex/internal/obs"
)

// startDiag is start with Diagnostics on and an optional BuildStatus wired
// in, for the tests below that exercise /healthz and /metrics.
func startDiag(t *testing.T, dir string, health *BuildStatus) (*Server, string) {
	t.Helper()
	s := &Server{Dir: dir, Addr: anyPort, Log: obs.Discard(), Diagnostics: true, Health: health, Ready: make(chan struct{})}
	return runStarted(t, s)
}

func TestHealthzAndMetricsAreReservedOnlyWithDiagnostics(t *testing.T) {
	_, base := start(t, tree(t))

	for _, path := range []string{"/healthz", "/metrics"} {
		resp, err := client.Get(base + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s without Diagnostics = %d, want 404: a server nobody asked "+
				"for diagnostics on must serve its tree faithfully, not shadow a real "+
				"path the tree might hold", path, resp.StatusCode)
		}
	}
}

func TestHealthzOKWithNoBuildLoop(t *testing.T) {
	_, base := startDiag(t, tree(t), nil)

	resp, err := client.Get(base + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200: a plain `cairndex serve` never builds, so "+
			"the only thing to report is that the process is up", resp.StatusCode)
	}
	var body healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status field = %q, want \"ok\"", body.Status)
	}
}

func TestHealthzDegradedWhenServedDirIsGone(t *testing.T) {
	dir := tree(t)
	_, base := startDiag(t, dir, nil)

	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}

	resp, err := client.Get(base + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503: the directory this server hands out no "+
			"longer exists", resp.StatusCode)
	}
}

func TestHealthzReflectsTheBuildLoop(t *testing.T) {
	var h BuildStatus
	_, base := startDiag(t, tree(t), &h)

	h.Record(nil, 250*time.Millisecond, 12, 3)
	resp, err := client.Get(base + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	var ok healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&ok); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || ok.Status != "ok" || ok.LastBuild == nil || ok.LastBuild.Files != 12 {
		t.Errorf("after a clean build: status=%d body=%+v, want 200 and last_build.files=12",
			resp.StatusCode, ok)
	}

	h.Record(errors.New("boom"), 10*time.Millisecond, 12, 3)
	resp, err = client.Get(base + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	var bad healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&bad); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable || bad.Status != "degraded" ||
		bad.LastBuild == nil || !strings.Contains(bad.LastBuild.Error, "boom") {
		t.Errorf("after a failed build: status=%d body=%+v, want 503 and last_build.error "+
			"naming it", resp.StatusCode, bad)
	}
}

func TestMetricsReportsUpAndBuildSeries(t *testing.T) {
	var h BuildStatus
	h.Record(nil, 250*time.Millisecond, 12, 3)
	_, base := startDiag(t, tree(t), &h)

	resp, err := client.Get(base + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain (Prometheus exposition format)", ct)
	}
	var body strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		body.Write(buf[:n])
		if err != nil {
			break
		}
	}
	got := body.String()
	for _, want := range []string{
		"cairndex_up 1",
		"cairndex_build_success 1",
		"cairndex_build_files 12",
		"cairndex_build_dirs 3",
		"cairndex_builds_total 1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("metrics body missing %q:\n%s", want, got)
		}
	}
}

func TestMetricsCountsRequestsToTheServedTree(t *testing.T) {
	_, base := startDiag(t, tree(t), nil)

	for i := 0; i < 3; i++ {
		resp, err := client.Get(base + "/no-such-file")
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}

	resp, err := client.Get(base + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		body.Write(buf[:n])
		if err != nil {
			break
		}
	}
	if !strings.Contains(body.String(), `cairndex_http_requests_total{status="4xx"} 3`) {
		t.Errorf("metrics body does not show the three 404s just made:\n%s", body.String())
	}
}
