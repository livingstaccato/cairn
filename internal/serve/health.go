// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// /healthz: whether this server, and the build loop behind it if there is
// one, are in a state worth being served traffic.
package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"sync"
	"time"
)

// BuildStatus records the outcome of the build loop behind a `watch
// --serve` server, so /healthz and /metrics can report on it instead of
// only on whether the process answers. The zero value means no build has
// finished yet — Snapshot's Recorded stays false until the first Record.
//
// serve itself never builds; a plain `cairndex serve` has no BuildStatus to
// give the Server, and /healthz falls back to reporting only that the
// served directory exists.
type BuildStatus struct {
	mu       sync.RWMutex
	recorded bool
	ok       bool
	at       time.Time
	errMsg   string
	duration time.Duration
	files    int
	dirs     int
	builds   int64
	failures int64
}

// Record stores the outcome of one build. err nil means it succeeded.
func (b *BuildStatus) Record(err error, duration time.Duration, files, dirs int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.recorded = true
	b.ok = err == nil
	b.at = time.Now()
	b.duration = duration
	b.files = files
	b.dirs = dirs
	b.builds++
	b.errMsg = ""
	if err != nil {
		b.errMsg = err.Error()
		b.failures++
	}
}

type buildSnapshot struct {
	recorded bool
	ok       bool
	at       time.Time
	errMsg   string
	duration time.Duration
	files    int
	dirs     int
	builds   int64
	failures int64
}

func (b *BuildStatus) snapshot() buildSnapshot {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return buildSnapshot{
		recorded: b.recorded, ok: b.ok, at: b.at, errMsg: b.errMsg,
		duration: b.duration, files: b.files, dirs: b.dirs,
		builds: b.builds, failures: b.failures,
	}
}

const (
	statusOK       = "ok"
	statusDegraded = "degraded"
)

// healthResponse is /healthz's JSON body.
type healthResponse struct {
	Status    string       `json:"status"`
	Reason    string       `json:"reason,omitempty"`
	LastBuild *healthBuild `json:"last_build,omitempty"`
}

type healthBuild struct {
	OK         bool   `json:"ok"`
	At         string `json:"at"`
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"duration_ms"`
	Files      int    `json:"files"`
	Dirs       int    `json:"dirs"`
}

// handleHealthz answers liveness/readiness probes. A missing served
// directory is degraded regardless of Health, since nothing above this can
// be trusted once it is gone; past that, an unset Health means there is no
// build loop to be wrong, and a set one speaks for itself.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	if _, err := os.Stat(s.Dir); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, healthResponse{
			Status: statusDegraded, Reason: "served directory: " + err.Error(),
		})
		return
	}

	if s.Health == nil {
		writeJSON(w, http.StatusOK, healthResponse{Status: statusOK})
		return
	}

	snap := s.Health.snapshot()
	if !snap.recorded {
		writeJSON(w, http.StatusOK, healthResponse{Status: statusOK, Reason: "no build has completed yet"})
		return
	}

	build := &healthBuild{
		OK: snap.ok, At: snap.at.UTC().Format(time.RFC3339), Error: snap.errMsg,
		DurationMs: snap.duration.Milliseconds(), Files: snap.files, Dirs: snap.dirs,
	}
	if snap.ok {
		writeJSON(w, http.StatusOK, healthResponse{Status: statusOK, LastBuild: build})
		return
	}
	writeJSON(w, http.StatusServiceUnavailable, healthResponse{
		Status: statusDegraded, Reason: "the last build failed", LastBuild: build,
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
