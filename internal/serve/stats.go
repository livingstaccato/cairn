// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// /metrics: a Prometheus text-exposition scrape target for this process's
// own request counts and, if Diagnostics wired one in, the build loop
// behind it. provide-telemetry (internal/obs) is a push/OTLP client with
// no scrape surface of its own, and internal/obs is the only file allowed
// to import it — so this is cairndex's own counters, not a pass-through.
package serve

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// requestStats counts requests to the served tree, by response status
// class, plus total time spent answering them. Atomic fields rather than a
// mutex: every request on the hot path touches this, and a scrape reads it
// far less often than a request writes it.
type requestStats struct {
	c2xx, c3xx, c4xx, c5xx atomic.Int64
	durationNs             atomic.Int64
	count                  atomic.Int64
}

func (r *requestStats) record(status int, d time.Duration) {
	switch {
	case status < 300:
		r.c2xx.Add(1)
	case status < 400:
		r.c3xx.Add(1)
	case status < 500:
		r.c4xx.Add(1)
	default:
		r.c5xx.Add(1)
	}
	r.durationNs.Add(d.Nanoseconds())
	r.count.Add(1)
}

type requestStatsSnapshot struct {
	c2xx, c3xx, c4xx, c5xx int64
	durationSum            time.Duration
	count                  int64
}

func (r *requestStats) snapshot() requestStatsSnapshot {
	return requestStatsSnapshot{
		c2xx: r.c2xx.Load(), c3xx: r.c3xx.Load(), c4xx: r.c4xx.Load(), c5xx: r.c5xx.Load(),
		durationSum: time.Duration(r.durationNs.Load()), count: r.count.Load(),
	}
}

// statusRecorder captures the status code a handler wrote, since
// http.ResponseWriter has no way to ask for it back afterward.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (rec *statusRecorder) WriteHeader(code int) {
	rec.status = code
	rec.ResponseWriter.WriteHeader(code)
}

// instrument wraps next so every request it serves is counted in stats. A
// handler that never calls WriteHeader gets the http.ResponseWriter default
// of 200, matching what the client actually saw.
func instrument(stats *requestStats, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)
		stats.record(rec.status, time.Since(start))
	})
}

// handleMetrics writes the current counters in Prometheus text exposition
// format. No client library: the format is a handful of fixed lines, and
// pulling in a dependency to print "name value" is a worse trade than
// writing it once here.
func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	var b strings.Builder
	metric(&b, "cairndex_up", "gauge", "Whether the server process is running.", "%d", 1)

	rs := s.stats.snapshot()
	metricHelp(&b, "cairndex_http_requests_total", "counter", "HTTP requests served, by response status class.")
	for class, n := range map[string]int64{"2xx": rs.c2xx, "3xx": rs.c3xx, "4xx": rs.c4xx, "5xx": rs.c5xx} {
		fmt.Fprintf(&b, "cairndex_http_requests_total{status=%q} %d\n", class, n)
	}
	metric(&b, "cairndex_http_request_duration_seconds_sum", "counter",
		"Total time spent answering requests.", "%f", rs.durationSum.Seconds())
	metric(&b, "cairndex_http_request_duration_seconds_count", "counter",
		"Requests counted in the duration sum.", "%d", rs.count)

	if s.Health != nil {
		writeBuildMetrics(&b, s.Health.snapshot())
	}

	_, _ = w.Write([]byte(b.String()))
}

func writeBuildMetrics(b *strings.Builder, snap buildSnapshot) {
	if !snap.recorded {
		return
	}
	ok := 0
	if snap.ok {
		ok = 1
	}
	metric(b, "cairndex_build_success", "gauge", "Whether the most recent build succeeded.", "%d", ok)
	metric(b, "cairndex_build_timestamp_seconds", "gauge", "Unix time of the most recent build.", "%d", snap.at.Unix())
	metric(b, "cairndex_build_duration_seconds", "gauge", "Wall-clock time the most recent build took.",
		"%f", snap.duration.Seconds())
	metric(b, "cairndex_build_files", "gauge", "Files the most recent build indexed.", "%d", snap.files)
	metric(b, "cairndex_build_dirs", "gauge", "Directories the most recent build indexed.", "%d", snap.dirs)
	metric(b, "cairndex_builds_total", "counter", "Builds completed since the process started.", "%d", snap.builds)
	metric(b, "cairndex_build_failures_total", "counter", "Builds that failed since the process started.",
		"%d", snap.failures)
}

// metric writes one HELP/TYPE/value line group. format is the printf verb
// for value's type, since Prometheus renders a gauge and a counter the same
// way and the only thing that differs line to line is int vs. float.
func metric(b *strings.Builder, name, kind, help, format string, value any) {
	metricHelp(b, name, kind, help)
	fmt.Fprintf(b, name+" "+format+"\n", value)
}

func metricHelp(b *strings.Builder, name, kind, help string) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, kind)
}
