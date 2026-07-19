// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestSSESession_SendAfterShutdownDoesNotPanic is the issue #5168 review P1
// regression: handleHTTPMessage captures the session pointer BEFORE dispatch
// (to run the principal check), then reuses it to deliver AFTER a
// potentially slow tools/call. If the SSE client disconnects during that
// window, handleSSE's teardown closes the session; a bare `sess.ch <- msg`
// would then panic on a closed channel (select+default does not save it -- a
// closed channel is always ready). This test closes the session first, then
// posts to its sessionId, and asserts the delivery is a graceful drop (202,
// no panic) rather than a crash.
func TestSSESession_SendAfterShutdownDoesNotPanic(t *testing.T) {
	s := testServer()

	sess := &sseSession{ch: make(chan []byte, 4)}
	s.sessMu.Lock()
	s.sessions["closed-session"] = sess
	s.sessMu.Unlock()

	// Simulate the SSE client having disconnected mid-dispatch: the reader
	// goroutine's deferred teardown already ran.
	sess.shutdown()

	body := `{"jsonrpc":"2.0","id":42,"method":"ping"}`
	req := httptest.NewRequest(http.MethodPost, "/mcp/message?sessionId=closed-session", strings.NewReader(body))
	rec := httptest.NewRecorder()

	// Must not panic.
	s.handleHTTPMessage(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (message dropped to a closed session)", rec.Code)
	}
}

// TestSSESession_ShutdownIsIdempotent proves calling shutdown twice (e.g. a
// teardown racing an explicit close) does not double-close the channel and
// panic.
func TestSSESession_ShutdownIsIdempotent(t *testing.T) {
	sess := &sseSession{ch: make(chan []byte, 1)}
	sess.shutdown()
	sess.shutdown() // must not panic on a second close
	if sess.send([]byte("x")) {
		t.Fatal("send() after shutdown = true, want false")
	}
}

// TestSSESession_ConcurrentSendAndShutdownNoPanic hammers the guarded
// send/shutdown pair from many goroutines while a reader drains the channel,
// modeling many concurrent POST /mcp/message deliveries racing the SSE
// client's disconnect. It must complete without a send-on-closed-channel
// panic. Run under -race to also confirm the mutex covers the closed flag and
// the channel op together.
func TestSSESession_ConcurrentSendAndShutdownNoPanic(t *testing.T) {
	sess := &sseSession{ch: make(chan []byte, 8)}

	var drain sync.WaitGroup
	drain.Add(1)
	go func() {
		defer drain.Done()
		for range sess.ch { //nolint:revive // drain until shutdown closes the channel
		}
	}()

	var senders sync.WaitGroup
	for i := 0; i < 64; i++ {
		senders.Add(1)
		go func() {
			defer senders.Done()
			for j := 0; j < 50; j++ {
				sess.send([]byte("payload"))
			}
		}()
	}

	// Close the session while senders are still firing.
	sess.shutdown()

	senders.Wait()
	drain.Wait()

	// Every post-shutdown send must be a graceful no-op.
	if sess.send([]byte("late")) {
		t.Fatal("send() after shutdown = true, want false")
	}
}
