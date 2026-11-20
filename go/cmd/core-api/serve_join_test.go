// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// joinTestBody is large enough that a truncated in-flight response cannot
// match it byte for byte.
const joinTestBody = "commit-record-0123456789abcdef"

func joinTestPayload() string {
	return strings.Repeat(joinTestBody, 2048)
}

// joinTestAddrs starts a loopback listener pair for join tests.
func joinTestAddrs(t *testing.T) (primaryLn, commitLn net.Listener) {
	t.Helper()
	var err error
	if primaryLn, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
		t.Fatalf("listen primary: %v", err)
	}
	if commitLn, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
		_ = primaryLn.Close()
		t.Fatalf("listen commit: %v", err)
	}
	t.Cleanup(func() {
		_ = primaryLn.Close()
		_ = commitLn.Close()
	})
	return primaryLn, commitLn
}

// joinTestCommitDriver mirrors runCommitListener's drain contract: serve
// until ctx ends, then shut down gracefully so in-flight commits drain
// instead of truncating. It reports the drain result on commitErrCh.
func joinTestCommitDriver(
	ctx context.Context,
	commit *http.Server,
	commitLn net.Listener,
	commitErrCh chan<- error,
	timeout time.Duration,
) {
	serveErr := make(chan error, 1)
	go func() { serveErr <- commit.Serve(commitLn) }()
	select {
	case <-ctx.Done():
		shutCtx, shutCancel := context.WithTimeout(context.Background(), timeout)
		defer shutCancel()
		commitErrCh <- commit.Shutdown(shutCtx)
	case err := <-serveErr:
		commitErrCh <- err
	}
}

// joinTestGet reads one full response body off a test server.
func joinTestGet(t *testing.T, url string) ([]byte, error) {
	t.Helper()
	resp, err := http.Get(url) // #nosec G107 -- loopback test URL only.
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(resp.Body)
}

// joinTestServeExited asserts a server's Serve loop returned, proving the
// join shut it down. It reads the buffered error channel the Serve
// goroutine reports on, so it never blocks once the join has returned.
// A dial-refused check would be racy here: parallel tests reuse loopback
// ports, so a succeeding dial cannot prove this server is still up.
func joinTestServeExited(t *testing.T, name string, errCh <-chan error) {
	t.Helper()
	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("%s Serve error = %v, want ErrServerClosed (clean shutdown drain)", name, err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("%s Serve never returned, want shutdown drain", name)
	}
}

// TestJoinCoreServersDrainsBothOnCancel is the F1 RED test: with an
// in-flight slow response on each server, cancelling ctx must shut down
// the primary, await the commit listener, return nil, and deliver both
// bodies byte-complete. The old select returned on the first ready
// branch, abandoning the other server mid-flight.
func TestJoinCoreServersDrainsBothOnCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const timeout = 10 * time.Second
	want := joinTestPayload()
	var entered atomic.Int64
	slow := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		entered.Add(1)
		time.Sleep(300 * time.Millisecond)
		_, _ = io.WriteString(w, want)
	})
	primaryLn, commitLn := joinTestAddrs(t)
	primary := &http.Server{Handler: slow, ReadHeaderTimeout: 5 * time.Second}
	commit := &http.Server{Handler: slow, ReadHeaderTimeout: 5 * time.Second}
	primaryErrCh := make(chan error, 1)
	go func() { primaryErrCh <- primary.Serve(primaryLn) }()
	commitErrCh := make(chan error, 1)
	go joinTestCommitDriver(ctx, commit, commitLn, commitErrCh, timeout)

	type getResult struct {
		body []byte
		err  error
	}
	primaryRes := make(chan getResult, 1)
	commitRes := make(chan getResult, 1)
	go func() {
		body, err := joinTestGet(t, "http://"+primaryLn.Addr().String())
		primaryRes <- getResult{body: body, err: err}
	}()
	go func() {
		body, err := joinTestGet(t, "http://"+commitLn.Addr().String())
		commitRes <- getResult{body: body, err: err}
	}()

	deadline := time.Now().Add(10 * time.Second)
	for entered.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if entered.Load() < 2 {
		cancel()
		t.Fatal("slow handlers never went in-flight before cancel")
	}
	cancel()

	if err := joinCoreServers(ctx, cancel, primary, primaryErrCh, commitErrCh, timeout); err != nil {
		t.Fatalf("joinCoreServers() error = %v, want nil (graceful drain)", err)
	}
	for name, ch := range map[string]<-chan getResult{"primary": primaryRes, "commit": commitRes} {
		select {
		case res := <-ch:
			if res.err != nil {
				t.Fatalf("%s in-flight error = %v, want full body", name, res.err)
			}
			if string(res.body) != want {
				t.Fatalf("%s body = %d bytes, want %d (truncated)", name, len(res.body), len(want))
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s in-flight response never completed", name)
		}
	}
	// The join returned nil, so the commit side already reported a clean
	// drain; the primary Serve exit below proves it shut down too.
	joinTestServeExited(t, "primary", primaryErrCh)
}

// TestJoinCoreServersCancelsCommitOnMainError pins the #10338 half of F1:
// an unexpected primary failure must cancel the listener context
// unconditionally (never gated on anything else) and report the primary
// error after the commit side drains.
func TestJoinCoreServersCancelsCommitOnMainError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const timeout = 10 * time.Second
	commitDrained := make(chan struct{})
	commitErrCh := make(chan error, 1)
	go func() {
		<-ctx.Done()
		close(commitDrained)
		commitErrCh <- nil
	}()
	primary := &http.Server{}
	boom := errors.New("listen tcp :8080: bind: address already in use")
	primaryErrCh := make(chan error, 1)
	primaryErrCh <- boom

	if err := joinCoreServers(ctx, cancel, primary, primaryErrCh, commitErrCh, timeout); !errors.Is(err, boom) {
		t.Fatalf("joinCoreServers() error = %v, want %v", err, boom)
	}
	select {
	case <-commitDrained:
	case <-time.After(10 * time.Second):
		t.Fatal("commit side never observed cancel after main error")
	}
}

// TestJoinCoreServersShutsDownPrimaryOnCommitError pins the mirror half
// of F1: a commit-listener failure must shut down the primary and report
// the commit error first, leaving the primary drained.
func TestJoinCoreServersShutsDownPrimaryOnCommitError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const timeout = 10 * time.Second
	primaryLn, _ := joinTestAddrs(t)
	primary := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}), ReadHeaderTimeout: 5 * time.Second}
	primaryErrCh := make(chan error, 1)
	go func() { primaryErrCh <- primary.Serve(primaryLn) }()
	commitBoom := errors.New("commit listener: probe failed")
	commitErrCh := make(chan error, 1)
	commitErrCh <- commitBoom

	if err := joinCoreServers(ctx, cancel, primary, primaryErrCh, commitErrCh, timeout); !errors.Is(err, commitBoom) {
		t.Fatalf("joinCoreServers() error = %v, want %v", err, commitBoom)
	}
	joinTestServeExited(t, "primary", primaryErrCh)
}

// TestJoinCoreServersDarkLaneDrainsOnCancel proves the dark lane needs no
// commit channel: with a nil commitErrCh the ctx-done branch shuts down the
// primary, skips the commit await, and returns nil.
func TestJoinCoreServersDarkLaneDrainsOnCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const timeout = 10 * time.Second
	primaryLn, _ := joinTestAddrs(t)
	primary := &http.Server{
		Handler:           http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	primaryErrCh := make(chan error, 1)
	go func() { primaryErrCh <- primary.Serve(primaryLn) }()
	cancel()

	if err := joinCoreServers(ctx, cancel, primary, primaryErrCh, nil, timeout); err != nil {
		t.Fatalf("joinCoreServers(dark) error = %v, want nil (graceful drain)", err)
	}
	joinTestServeExited(t, "primary", primaryErrCh)
}

// TestJoinCoreServersDarkLaneReportsPrimaryError proves a primary failure
// still cancels the listener scope and reports the primary error when the
// lane is dark: the nil commit await is a no-op, never a block.
func TestJoinCoreServersDarkLaneReportsPrimaryError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const timeout = 10 * time.Second
	primary := &http.Server{}
	boom := errors.New("listen tcp :8080: bind: address already in use")
	primaryErrCh := make(chan error, 1)
	primaryErrCh <- boom

	if err := joinCoreServers(ctx, cancel, primary, primaryErrCh, nil, timeout); !errors.Is(err, boom) {
		t.Fatalf("joinCoreServers(dark) error = %v, want %v", err, boom)
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("listener scope not cancelled after primary error on the dark lane")
	}
}

// TestCoreServeFailedMapsCleanShutdown pins the fatal-error predicate the
// join uses: a clean ErrServerClosed is never fatal, anything else is.
func TestCoreServeFailedMapsCleanShutdown(t *testing.T) {
	t.Parallel()

	if coreServeFailed(nil) {
		t.Fatal("coreServeFailed(nil) = true, want false (nothing failed)")
	}
	if coreServeFailed(http.ErrServerClosed) {
		t.Fatal("coreServeFailed(ErrServerClosed) = true, want false (clean shutdown)")
	}
	if !coreServeFailed(errors.New("listen tcp :8089: bind: address already in use")) {
		t.Fatal("coreServeFailed(unexpected) = false, want true (fail the process)")
	}
}
