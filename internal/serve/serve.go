// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Package serve puts a built tree in front of a browser, so what cairn wrote
// can be looked at without Hugo, nginx or a container standing in the way.
//
// It is the partner to `cairn watch`: the watcher rebuilds the directories a
// change touched and this hands the result to a refresh, so the distance
// between editing a tree and seeing the index it produces is one keystroke.
//
// It is a viewer, not a deployment. Nothing here negotiates content, terminates
// TLS, authenticates anyone or writes to the tree. A directory it cannot answer
// is a 404 rather than a listing invented by the standard library, because what
// a directory listing says is the whole thing cairn exists to decide.
package serve

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

// DefaultAddr is the address cairn serves on when none is given.
//
// Loopback, never every interface. This hands out whatever is in a directory,
// and a half-built mirror is nobody else's to read; defaulting to 0.0.0.0 would
// publish it to the conference wifi on behalf of someone who only asked to look
// at their own build. Serving wider than the machine is a thing to ask for in
// writing, by passing an address.
//
// 22476 is CAIRN on a phone keypad, which is the only part of a port number
// anybody remembers. The rest is arithmetic: it falls inside IANA's unassigned
// 22352-22536 block, and it sits below the ephemeral range on both Linux
// (32768+) and macOS (49152+), so the kernel never hands it to something else
// while cairn is not looking. The low numbers are unusable anyway — 1313 is
// Hugo's, and 3000, 5000, 8000 and 8080 are occupied on any machine doing web
// work.
//
// A port cairn does not choose for itself is still the safer habit. Two cairns
// on one machine collide here whatever the number is, and --addr is the answer
// to that rather than a different default.
const DefaultAddr = "127.0.0.1:22476"

// ShutdownGrace bounds the wait for requests still in flight when the context
// is cancelled. A browser pulling a large artifact out of a mirror should not
// be cut off the instant Ctrl-C lands, and one that wandered off should not
// keep the process alive.
const ShutdownGrace = 5 * time.Second

// Read-side timeouts. A viewer bound to loopback is not facing the internet,
// but a connection that opens and then says nothing pins a goroutine for as
// long as it likes, and the cost of bounding that is nothing.
//
// There is deliberately no write timeout. A mirror serves multi-gigabyte
// artifacts, and a deadline on the response is a deadline on the download: the
// clock would cut off exactly the transfer it was meant to protect.
const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	idleTimeout       = 60 * time.Second
)

// listen opens the socket, indirected so a test can hand Run a listener that
// fails after it is already serving. That class of failure — a descriptor limit
// reached, an interface withdrawn — cannot be produced on demand from a real
// one, and it is the path that decides whether Run reports or swallows it.
var listen = net.Listen

// grace is ShutdownGrace, indirected for the same reason: a test that waits the
// real five seconds to watch a deadline expire is a test nobody runs, and the
// deadline is the whole difference between a graceful stop and a hung one.
var grace = ShutdownGrace

// Server serves one directory over HTTP.
//
// The zero value is not useful — Dir has to name a built tree — but every other
// field has an answer it falls back on, so a caller that has only a directory
// gets a working server.
type Server struct {
	// Dir is the tree to serve, and the boundary of what can be reached. It
	// must exist and be a directory before the socket is opened.
	Dir string
	// Addr is where to listen, host and port. Empty means DefaultAddr. A port
	// of 0 asks the kernel to choose, which BoundAddr then reports.
	Addr string
	// Log receives what the server did. Nil is a logger that drops everything,
	// so a caller that has not set telemetry up still gets a server.
	Log *slog.Logger

	// Ready, when set, is closed once the socket is open and BoundAddr has an
	// answer. A caller that has to point something at the server waits on it
	// rather than guessing how long a bind takes.
	Ready chan struct{}

	mu    sync.RWMutex
	bound string
}

// Run serves until ctx is cancelled, and returns nil when it stopped because it
// was asked to.
//
// Everything that can refuse the job refuses before the first request: a Dir
// that is not a directory, and an address already taken. Reporting those late
// means reporting them to a browser instead of to the person who typed the
// command.
func (s *Server) Run(ctx context.Context) error {
	ln, err := s.bind()
	if err != nil {
		return err
	}

	srv := s.server()
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	if s.Ready != nil {
		close(s.Ready)
	}

	select {
	case err := <-served:
		// Serve owns the listener now and has closed it on the way out, so
		// there is nothing left to unwind here.
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serving %s: %w", s.BoundAddr(), err)
	case <-ctx.Done():
		return s.stop(ctx, srv, served)
	}
}

// BoundAddr reports the address the server actually listens on.
//
// That is not always the address that was asked for: a caller may pass port 0
// to have the kernel choose a free one — the one exception to cairn never
// picking a port on someone's behalf, because it is the caller asking for it —
// and then has no other way to learn where to point a browser. Empty until the
// socket is open, and safe to call from another goroutine while Run is
// running, which is exactly when a caller needs it.
func (s *Server) BoundAddr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bound
}

// bind fills in the defaults, checks the tree exists and opens the socket.
//
// A port already in use is an error and stays one. Quietly moving to the next
// free port would leave the operator reading a URL that no longer says where
// the server is, and would hide a second cairn already running on the tree they
// think they are looking at.
func (s *Server) bind() (net.Listener, error) {
	if s.Addr == "" {
		s.Addr = DefaultAddr
	}
	if s.Log == nil {
		s.Log = slog.New(slog.DiscardHandler)
	}

	fi, err := os.Stat(s.Dir)
	if err != nil {
		return nil, fmt.Errorf("serve %s: %w", s.Dir, err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("serve %s: not a directory", s.Dir)
	}

	ln, err := listen("tcp", s.Addr)
	if err != nil {
		return nil, fmt.Errorf("cannot listen on %s: %w; cairn will not choose a "+
			"different port for you — free that one or name another", s.Addr, err)
	}

	addr := ln.Addr().String()
	s.mu.Lock()
	s.bound = addr
	s.mu.Unlock()

	s.Log.Info("serving", "dir", s.Dir, "addr", addr, "url", "http://"+addr+"/")
	return ln, nil
}

// server builds the http.Server. Its own error log is routed through slog:
// left alone it writes unstructured lines straight to stderr, which is the
// diagnostic-by-print problem arriving through a side door.
func (s *Server) server() *http.Server {
	return &http.Server{
		Handler:           &files{root: http.Dir(s.Dir), log: s.Log},
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		IdleTimeout:       idleTimeout,
		ErrorLog:          slog.NewLogLogger(s.Log.Handler(), slog.LevelWarn),
	}
}

// stop shuts the server down within the grace period and waits for Serve to
// return, so nothing this function started is still running when it does.
func (s *Server) stop(ctx context.Context, srv *http.Server, served <-chan error) error {
	// WithoutCancel, because the parent is already cancelled: deriving the
	// grace period from it would expire it before the first connection had a
	// chance to finish, which is a hard stop wearing a graceful one's name.
	deadline, cancel := context.WithTimeout(context.WithoutCancel(ctx), grace)
	defer cancel()

	err := srv.Shutdown(deadline)
	// Shutdown closes the listener first, so Serve has already returned; the
	// receive is what makes "no goroutine outlives Run" true rather than
	// likely.
	<-served

	if err != nil {
		// The deadline passed with connections still open. Cutting them is the
		// only way to leave nothing running behind this call.
		if cerr := srv.Close(); cerr != nil {
			s.Log.Warn("could not close the server", "err", cerr)
		}
		return fmt.Errorf("shutting down %s: %w", s.BoundAddr(), err)
	}
	s.Log.Info("stopped", "addr", s.BoundAddr())
	return nil
}
