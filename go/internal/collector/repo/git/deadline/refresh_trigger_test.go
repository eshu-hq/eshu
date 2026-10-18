// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadline

import (
	"context"
	"testing"
	"time"
)

// TestShouldRefreshAddressesTable is the closed refresh-trigger set of
// #8092: the structured deadline class first, then one row per libcurl
// wording. Never an HTTP status, auth, or pack answer, and never the plain
// local "operation timed out" text the connect_failed class also matches
// (note N4 on #8091) — only curl's "Operation timed out after" wording.
func TestShouldRefreshAddressesTable(t *testing.T) {
	cases := []struct {
		name   string
		class  string
		stderr string
		want   bool
	}{
		{"deadline class", ClassDeadlineExceeded, "", true},
		{"could not resolve host", ClassDNSResolve, "fatal: unable to access 'https://github.com/o/r.git/': Could not resolve host: github.com", true},
		{"resolving timed out", ClassDNSResolve, "fatal: unable to access 'https://github.com/o/r.git/': Resolving timed out after 300000 milliseconds", true},
		{"failed to connect", ClassConnect, "fatal: unable to access 'https://github.com/o/r.git/': Failed to connect to github.com port 443 after 21064 ms", true},
		{"connection timed out", ClassConnect, "fatal: unable to access 'https://github.com/o/r.git/': Connection timed out after 30001 milliseconds", true},
		{"connection reset", ClassConnectionReset, "error: RPC failed; curl 56 Recv failure: Connection reset by peer", true},
		{"curl operation timed out after", ClassConnect, "error: RPC failed; curl 28 Operation timed out after 30000 milliseconds with 0 bytes received", true},
		{"plain operation timed out is local", ClassConnect, "fatal: unable to connect: operation timed out", false},
		{"auth answer", ClassAuth, "fatal: Authentication failed for 'https://github.com/o/r.git/'", false},
		{"auth answer beside a resolve line", ClassAuth, "Could not resolve host: github.com\nfatal: Authentication failed", false},
		{"http 4xx answer", ClassHTTPClient, "fatal: unable to access: The requested URL returned error: 404", false},
		{"not found answer", ClassNotFound, "remote: Repository not found.", false},
		{"rate limited answer", ClassRateLimited, "remote: too many requests", false},
		{"pack answer", ClassPackObject, "fatal: bad object HEAD", false},
		{"http2 framing is not an address problem", ClassHTTP2Framing, "error: RPC failed; curl 16 Error in the HTTP2 framing layer", false},
		{"tls is not an address problem", ClassTLSHandshake, "fatal: unable to access: SSL_connect: SSL_ERROR_SYSCALL", false},
		{"unknown text", ClassOther, "fatal: something the table does not name", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldRefreshAddresses(tc.class, tc.stderr); got != tc.want {
				t.Fatalf("shouldRefreshAddresses(%q, %q) = %v, want %v", tc.class, tc.stderr, got, tc.want)
			}
		})
	}
}

// TestWaitForRefreshReportsShutdownWhenCancelRacesTheRefresh is the N6 note
// carried from the #8091 review: when the refresh channel closes at the same
// moment the context ends, select may pick the refresh case. The wait must
// still report shutdown so no retry (and no retries_total increment or span
// event) starts on a dead context.
func TestWaitForRefreshReportsShutdownWhenCancelRacesTheRefresh(t *testing.T) {
	refreshed := make(chan struct{})
	close(refreshed)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i := 0; i < 50; i++ {
		if !waitForRefresh(ctx, refreshed) {
			t.Fatalf("waitForRefresh(cancelled ctx, closed chan) = false on pass %d, want shutdown", i)
		}
	}
}

// TestSleepContextReportsShutdownWhenCancelRacesTheTimer is the backoff half
// of N6: a timer that fires as the context ends must not read as a completed
// sleep.
func TestSleepContextReportsShutdownWhenCancelRacesTheTimer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i := 0; i < 50; i++ {
		if err := sleepContext(ctx, time.Nanosecond); err == nil {
			t.Fatalf("sleepContext(cancelled ctx) = nil on pass %d, want the context error", i)
		}
	}
}
