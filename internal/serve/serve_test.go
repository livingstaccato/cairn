// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Tests for the server's life: what address it takes, what it refuses, and
// what it leaves behind when it stops.

package serve

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/livingstaccato/cairn/internal/obs"
)

// anyPort asks the kernel for a free port so a suite running in parallel with
// anything else on the machine never fights over one. It is the only address in
// these tests: a literal port here would be the same bet the rule against
// hardcoded ports exists to refuse.
const anyPort = "127.0.0.1:0"

// waitFor bounds how long a test blocks on the network. Generous: the Windows
// and macOS runners are markedly slower than Linux, and a tight timeout is a
// test that fails for the wrong reason.
const waitFor = 20 * time.Second

// client keeps no connections. A pooled idle connection outliving a test is a
// connection the next server has to wait on during shutdown.
var client = &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}

// tree writes a directory shaped like something cairn built: an index at the
// root, an index in a subdirectory, a subdirectory with no index at all, and a
// file outside the served root that no request may reach.
func tree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, "secret.txt", "not yours\n")

	out := filepath.Join(root, "site")
	write(t, out, "index.html", "<h1>root index</h1>\n")
	write(t, out, "index.json", `{"entries":[]}`)
	write(t, out, "index.csv", "name,size\n")
	write(t, out, "index.txt", "bootstrap/\n")
	write(t, out, "tree.json", `{"entries":[]}`)
	write(t, out, "search-index.json", `[]`)
	write(t, out, "Data.JSON", `{}`)
	write(t, out, "SHA256SUMS", "d41d8cd98f00b204e9800998ecf8427e  index.txt\n") // pragma: allowlist secret
	write(t, out, "cairn.css", "body{}\n")
	write(t, out, "cairn.js", "export{}\n")
	write(t, out, "icons.svg", "<svg/>\n")
	write(t, out, "release.bin", "\x00\x01\x02binary\n")
	write(t, out, "docs/index.html", "<h1>docs index</h1>\n")
	write(t, out, "bare/notes.md", "# no index here\n")
	return out
}

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// start runs a server on a kernel-chosen port and returns it with its base URL.
// Cleanup cancels it and fails the test if Run does not come back clean, so
// every test in the file also asserts the shutdown path.
func start(t *testing.T, dir string) (*Server, string) {
	t.Helper()
	s := &Server{Dir: dir, Addr: anyPort, Log: obs.Discard(), Ready: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	select {
	case <-s.Ready:
	case err := <-done:
		cancel()
		t.Fatalf("the server stopped before it was ready: %v", err)
	case <-time.After(waitFor):
		cancel()
		t.Fatal("the server never reported itself ready")
	}

	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run returned %v, want nil on a cancelled context", err)
			}
		case <-time.After(waitFor):
			t.Fatal("Run did not return after its context was cancelled")
		}
	})
	return s, "http://" + s.BoundAddr()
}

// runOnce calls Run and insists it comes back on its own, so a Run that took a
// job it was supposed to refuse says so instead of hanging the suite.
func runOnce(t *testing.T, s *Server) error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	select {
	case err := <-done:
		return err
	case <-time.After(waitFor):
		t.Fatal("Run never returned; it accepted a job it had to refuse")
		return nil
	}
}

func TestDefaultAddrIsLoopbackOnly(t *testing.T) {
	host, port, err := net.SplitHostPort(DefaultAddr)
	if err != nil {
		t.Fatalf("DefaultAddr %q does not parse: %v", DefaultAddr, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		t.Errorf("DefaultAddr host is %q, want a loopback address: a viewer of a "+
			"local build must not be published to the network by default", host)
	}
	if port == "0" || port == "" {
		t.Errorf("DefaultAddr port is %q, want a fixed port so the URL is predictable", port)
	}
}

func TestRunServesAndStopsClean(t *testing.T) {
	_, base := start(t, tree(t))

	resp, body := get(t, base+"/index.html")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "root index") {
		t.Errorf("body %q does not hold the served file", body)
	}
}

func TestBoundAddrReportsTheKernelsChoice(t *testing.T) {
	unstarted := &Server{}
	if got := unstarted.BoundAddr(); got != "" {
		t.Errorf("BoundAddr before Run is %q, want empty", got)
	}

	s, _ := start(t, tree(t))
	bound := s.BoundAddr()
	if bound == anyPort || bound == "" {
		t.Fatalf("BoundAddr is %q, want the port the kernel actually chose", bound)
	}
	_, port, err := net.SplitHostPort(bound)
	if err != nil || port == "0" {
		t.Errorf("BoundAddr is %q, want a real port: a caller that asked for :0 has "+
			"no other way to learn where to point a browser", bound)
	}
}

func TestRunRefusesAPortAlreadyInUse(t *testing.T) {
	held, err := net.Listen("tcp", anyPort)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()
	addr := held.Addr().String()

	s := &Server{Dir: tree(t), Addr: addr, Log: obs.Discard()}
	err = runOnce(t, s)
	if err == nil {
		t.Fatal("Run returned nil for a port in use; it must never quietly move to another port")
	}
	if !strings.Contains(err.Error(), addr) {
		t.Errorf("error %q does not name the address %q the operator has to free", err, addr)
	}
	if got := s.BoundAddr(); got != "" {
		t.Errorf("BoundAddr is %q after a failed bind, want empty", got)
	}
}

func TestRunRefusesSomethingThatIsNotADirectory(t *testing.T) {
	root := t.TempDir()
	write(t, root, "index.html", "<h1>a file, not a tree</h1>\n")

	for _, dir := range []string{
		filepath.Join(root, "index.html"),
		filepath.Join(root, "never-built"),
	} {
		s := &Server{Dir: dir, Addr: anyPort, Log: obs.Discard()}
		err := runOnce(t, s)
		if err == nil {
			t.Errorf("Run(%q) returned nil; serving a path that is not a directory answers "+
				"nothing but 404 and reads as a broken build", dir)
			continue
		}
		if !strings.Contains(err.Error(), filepath.Base(dir)) {
			t.Errorf("error %q does not name the path", err)
		}
	}
}

func TestRunFallsBackToDefaultAddr(t *testing.T) {
	// The Dir check runs before the bind, so this reaches the address default
	// without taking a fixed port on the machine running the suite.
	s := &Server{Dir: filepath.Join(t.TempDir(), "never-built"), Log: obs.Discard()}
	if err := runOnce(t, s); err == nil {
		t.Fatal("want an error for a missing directory")
	}
	if s.Addr != DefaultAddr {
		t.Errorf("Addr is %q after Run with none given, want %q", s.Addr, DefaultAddr)
	}
}

func TestRunSurvivesANilLogger(t *testing.T) {
	// A caller that has not set telemetry up should get a working server, not a
	// nil dereference on the first log line.
	s := &Server{Dir: tree(t), Addr: anyPort, Ready: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	<-s.Ready
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// brokenListener accepts nothing. It stands in for the class of failure that
// arrives after the socket is open — the file-descriptor limit, an interface
// going away — which a real listener cannot be made to produce on demand.
type brokenListener struct{ addr net.Addr }

var errBroken = errors.New("the listener broke")

func (b brokenListener) Accept() (net.Conn, error) { return nil, errBroken }
func (b brokenListener) Close() error              { return nil }
func (b brokenListener) Addr() net.Addr            { return b.addr }

func TestRunReportsAFailureWhileServing(t *testing.T) {
	real := listen
	t.Cleanup(func() { listen = real })
	listen = func(_, _ string) (net.Listener, error) {
		return brokenListener{addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}}, nil
	}

	s := &Server{Dir: tree(t), Addr: anyPort, Log: obs.Discard()}
	err := runOnce(t, s)
	if !errors.Is(err, errBroken) {
		t.Fatalf("Run returned %v, want the listener's own error wrapped", err)
	}
}

// heldListener hands out connections that report the moment the server begins
// reading one. Without it a test cannot know the server has taken the
// connection before it cancels, and the shutdown it is trying to observe races
// with the accept it depends on.
type heldListener struct {
	net.Listener
	reading chan struct{}
	once    *sync.Once
}

func (l heldListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return heldConn{Conn: c, reading: l.reading, once: l.once}, nil
}

type heldConn struct {
	net.Conn
	reading chan struct{}
	once    *sync.Once
}

func (c heldConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	c.once.Do(func() { close(c.reading) })
	return n, err
}

func TestShutdownIsBoundedWhenAClientHangsOn(t *testing.T) {
	realListen, realGrace := listen, grace
	t.Cleanup(func() { listen, grace = realListen, realGrace })
	grace = 250 * time.Millisecond

	reading := make(chan struct{})
	once := &sync.Once{}
	listen = func(network, address string) (net.Listener, error) {
		ln, err := realListen(network, address)
		if err != nil {
			return nil, err
		}
		return heldListener{Listener: ln, reading: reading, once: once}, nil
	}

	s := &Server{Dir: tree(t), Addr: anyPort, Log: obs.Discard(), Ready: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	<-s.Ready

	conn, err := net.DialTimeout("tcp", s.BoundAddr(), waitFor)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	// One blank line short of a request, so the server reads what arrived and
	// then waits for the rest of it forever.
	if _, err := io.WriteString(conn, "GET /index.html HTTP/1.1\r\nHost: cairn.invalid\r\n"); err != nil {
		t.Fatal(err)
	}
	<-reading

	began := time.Now()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run returned nil; a shutdown that ran out of time has to be reported, " +
				"not reported as a clean stop")
		}
		if !strings.Contains(err.Error(), s.BoundAddr()) {
			t.Errorf("error %q does not name the address", err)
		}
		if waited := time.Since(began); waited < grace/2 {
			t.Errorf("Run came back after %s, inside the %s grace period: a request still "+
				"in flight was cut off rather than given its chance to finish", waited, grace)
		}
	case <-time.After(waitFor):
		t.Fatal("Run never returned: the wait for connections to finish is not bounded")
	}
}

func TestRunReleasesThePortWhenItStops(t *testing.T) {
	// Run by hand rather than through start: the point of the test is what the
	// process looks like once Run has returned, which a deferred cleanup would
	// sequence the wrong way round.
	s := &Server{Dir: tree(t), Addr: anyPort, Log: obs.Discard(), Ready: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	<-s.Ready
	addr := s.BoundAddr()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(waitFor):
		t.Fatal("Run did not return after its context was cancelled")
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("cannot rebind %s once Run has returned: %v; something the server "+
			"started is still holding the socket", addr, err)
	}
	_ = ln.Close()
}
