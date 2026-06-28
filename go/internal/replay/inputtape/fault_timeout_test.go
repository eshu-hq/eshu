// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inputtape

// Timeout-classification proof for the fault-injection tape (R-11, #4120).
//
// A real network timeout is recognized by SDKs and collectors through TWO
// independent paths: errors.Is(err, context.DeadlineExceeded) and the net.Error
// Timeout() bool interface (reached via (*url.Error).Timeout() / os.IsTimeout
// after the HTTP stack wraps a RoundTrip error in *url.Error). An injected
// timeout fault must satisfy both, or a collector that gates retries on
// Timeout() will not retry it even though the tape scripts a timeout. This file
// exercises the *url.Error level by driving the fault through a full
// *http.Client.
//
// Skill active: golang-engineering.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"testing"
)

// TestFaultTimeoutClassifiedThroughHTTPClient drives the timeout fault through a
// full *http.Client (not a bare RoundTrip), so the returned error is wrapped in
// *url.Error by the HTTP stack — the shape collectors actually see. It asserts
// the wrapped error is still classified as a timeout by every path real code
// uses: os.IsTimeout, (*url.Error).Timeout(), the net.Error Timeout() interface
// via errors.As, and errors.Is(context.DeadlineExceeded).
func TestFaultTimeoutClassifiedThroughHTTPClient(t *testing.T) {
	t.Parallel()

	server, _ := newFakeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	recorder := New(Config{})
	recClient := &http.Client{Transport: recorder}
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/data", nil)
	if _, err := recClient.Do(req); err != nil {
		t.Fatalf("record: %v", err)
	}
	tape := recorder.Tape("test")
	tape.Interactions[0].Fault = &Fault{Kind: FaultKindTimeout}
	server.Close()

	replayer, err := NewReplayer(tape, Config{})
	if err != nil {
		t.Fatalf("new replayer: %v", err)
	}
	client := &http.Client{Transport: replayer}
	rreq, _ := http.NewRequest(http.MethodGet, server.URL+"/data", nil)
	_, doErr := client.Do(rreq)
	if doErr == nil {
		t.Fatalf("want timeout error from client.Do, got nil")
	}

	// os.IsTimeout walks the error chain looking for a Timeout() bool == true.
	if !os.IsTimeout(doErr) {
		t.Fatalf("os.IsTimeout should classify the injected timeout, got %v", doErr)
	}

	// The HTTP stack wraps RoundTrip errors in *url.Error; (*url.Error).Timeout()
	// delegates to the immediate wrapped error's Timeout method, which our typed
	// timeout error implements.
	var urlErr *url.Error
	if errors.As(doErr, &urlErr) {
		if !urlErr.Timeout() {
			t.Fatalf("(*url.Error).Timeout() should be true for injected timeout, got %v", doErr)
		}
	}

	// The net.Error Timeout() interface must be reachable via errors.As.
	var timeoutClassifier interface{ Timeout() bool }
	if !errors.As(doErr, &timeoutClassifier) || !timeoutClassifier.Timeout() {
		t.Fatalf("want Timeout() bool == true through *http.Client, got %v", doErr)
	}

	// And the context-sentinel path must still match.
	if !errors.Is(doErr, context.DeadlineExceeded) {
		t.Fatalf("want errors.Is(doErr, context.DeadlineExceeded) true, got %v", doErr)
	}
}
