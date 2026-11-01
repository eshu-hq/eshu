// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package intake

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/webhook"
)

// This file holds the #8876 box 7 cancellation proofs for the intake API:
// a client that disconnects mid-body or after the headers, or cancels while
// the store is working, must never leave a partial or phantom row, and a
// retry of the same delivery must converge to exactly one row.

const (
	cancelToken = "core-intake-token"
	cancelPath  = "/api/v1/webhook/intake/repository"
	cancelBody  = `{"api_version":"v1","provider":"github","delivery_id":"d1","event_kind":"push","decision":"accepted","repository_external_id":"123","repository_full_name":"octo/repo","default_branch":"main","target_sha":"abc123"}`
)

// coalescingRepoStore is an in-memory repository store with the production
// refresh-key coalescing (one row per provider|repo|branch|sha, replays bump
// duplicate_count). It honors context like the Postgres store: when stall is
// set it holds the write until the caller's context ends and then commits
// nothing.
type coalescingRepoStore struct {
	mu       sync.Mutex
	rows     map[string]*webhook.StoredTrigger
	calls    int
	stall    bool
	entered  chan struct{}
	returned chan error
}

func newCoalescingRepoStore() *coalescingRepoStore {
	return &coalescingRepoStore{
		rows:     map[string]*webhook.StoredTrigger{},
		entered:  make(chan struct{}, 8),
		returned: make(chan error, 8),
	}
}

func (s *coalescingRepoStore) StoreTrigger(ctx context.Context, trigger webhook.Trigger, _ time.Time) (webhook.StoredTrigger, error) {
	s.mu.Lock()
	s.calls++
	stall := s.stall
	s.mu.Unlock()
	s.entered <- struct{}{}
	if stall {
		select {
		case <-ctx.Done():
			s.returned <- ctx.Err()
			return webhook.StoredTrigger{}, ctx.Err()
		case <-time.After(2 * time.Second):
			// A store that outlives a dead caller commits the row; the
			// assertions below treat that as the defect it is.
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := string(trigger.Provider) + "|" + trigger.RepositoryExternalID + "|" + trigger.DefaultBranch + "|" + trigger.TargetSHA
	if row, ok := s.rows[key]; ok {
		row.DuplicateCount++
		return *row, nil
	}
	row := &webhook.StoredTrigger{
		Trigger:   trigger,
		TriggerID: fmt.Sprintf("cancel-trigger-%d", len(s.rows)+1),
		Status:    webhook.TriggerStatusQueued,
	}
	s.rows[key] = row
	return *row, nil
}

func (s *coalescingRepoStore) rowCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rows)
}

func (s *coalescingRepoStore) storeCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *coalescingRepoStore) duplicates() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, row := range s.rows {
		return row.DuplicateCount
	}
	return -1
}

// statusRecorder records the status the handler chose even when the peer is
// already gone, and can hold the response header until released so a test
// can cancel the client between "stored" and "acknowledged".
type statusRecorder struct {
	http.ResponseWriter
	status int
	hold   <-chan struct{}
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.hold != nil {
		<-r.hold
	}
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

type cancelHarness struct {
	server   *httptest.Server
	store    *coalescingRepoStore
	finished chan int
	hold     chan struct{}
}

// newCancelHarness serves the real intake API behind a wrapper that reports
// each finished request's status code on h.finished.
func newCancelHarness(t *testing.T) *cancelHarness {
	t.Helper()
	h := &cancelHarness{store: newCoalescingRepoStore(), finished: make(chan int, 8)}
	mux := NewServer(ServerConfig{Token: cancelToken}, Stores{Repository: h.store}).Mux()
	h.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, hold: h.hold}
		mux.ServeHTTP(rec, r)
		h.finished <- rec.status
	}))
	t.Cleanup(h.server.Close)
	return h
}

func (h *cancelHarness) waitFinished(t *testing.T) int {
	t.Helper()
	select {
	case status := <-h.finished:
		return status
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the intake handler to finish")
		return 0
	}
}

// retryStoresExactlyOne replays the full delivery twice over a healthy
// connection and asserts one row, then duplicate_count 1 on the replay.
func (h *cancelHarness) retryStoresExactlyOne(t *testing.T) {
	t.Helper()
	for i := 0; i < 2; i++ {
		req, err := http.NewRequest(http.MethodPost, h.server.URL+cancelPath, strings.NewReader(cancelBody))
		if err != nil {
			t.Fatalf("build retry request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+cancelToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("retry %d: %v", i, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("retry %d: status = %d, want 202", i, resp.StatusCode)
		}
		if got := h.waitFinished(t); got != http.StatusAccepted {
			t.Fatalf("retry %d: handler status = %d, want 202", i, got)
		}
	}
	if got := h.store.rowCount(); got != 1 {
		t.Fatalf("rows after retry = %d, want exactly 1", got)
	}
	if got := h.store.duplicates(); got != 1 {
		t.Fatalf("duplicate_count after one replay = %d, want 1", got)
	}
}

// rawPost opens a raw connection, writes a request that declares
// declaredLength body bytes, sends only sent, and hangs up without reading a
// response — a client that dies mid-request.
func rawPost(t *testing.T, h *cancelHarness, declaredLength int, sent string) {
	t.Helper()
	conn, err := net.Dial("tcp", h.server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial intake: %v", err)
	}
	head := fmt.Sprintf("POST %s HTTP/1.1\r\nHost: intake\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n",
		cancelPath, cancelToken, declaredLength)
	if _, err := io.WriteString(conn, head+sent); err != nil {
		t.Fatalf("write partial request: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close raw connection: %v", err)
	}
}

// TestIntakeClientDisconnectBeforeBodyCompleteStoresNothing proves a client
// that disconnects after the headers, after half the body, or after a
// complete JSON value but short of its declared Content-Length never leaves
// a row: the handler refuses with 400 before the store is touched, and the
// retry stores exactly one row. The third shape is the sharp one: the bytes
// received already decode as a full trigger, so only the read-error check
// stands between a truncated request and a committed row.
func TestIntakeClientDisconnectBeforeBodyCompleteStoresNothing(t *testing.T) {
	t.Parallel()

	shapes := []struct {
		name     string
		declared int
		sent     string
	}{
		{"headers only", len(cancelBody), ""},
		{"half the body", len(cancelBody), cancelBody[:len(cancelBody)/2]},
		{"complete json short of content-length", len(cancelBody) + 64, cancelBody},
	}
	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()
			h := newCancelHarness(t)
			rawPost(t, h, shape.declared, shape.sent)
			if got := h.waitFinished(t); got != http.StatusBadRequest {
				t.Fatalf("handler status = %d, want 400 for an incomplete body", got)
			}
			if got := h.store.storeCalls(); got != 0 {
				t.Fatalf("store calls = %d, want 0 (nothing reaches storage)", got)
			}
			if got := h.store.rowCount(); got != 0 {
				t.Fatalf("rows after incomplete body = %d, want 0", got)
			}
			h.retryStoresExactlyOne(t)
		})
	}
}

// TestIntakeCanceledRequestContextReachesStore proves the request context is
// the store's context: when the client cancels while the store is working,
// the store sees the cancellation, commits nothing, and the handler answers
// 500 (a redelivery signal). The retry then stores exactly one row.
func TestIntakeCanceledRequestContextReachesStore(t *testing.T) {
	t.Parallel()

	h := newCancelHarness(t)
	h.store.stall = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.server.URL+cancelPath, bytes.NewReader([]byte(cancelBody)))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+cancelToken)
	clientDone := make(chan error, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
		clientDone <- err
	}()

	select {
	case <-h.store.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("request never reached the store")
	}
	cancel()
	if err := <-clientDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("client error = %v, want context.Canceled", err)
	}
	select {
	case err := <-h.store.returned:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("store saw ctx error %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("store never observed the cancellation; request context is not passed through")
	}
	if got := h.waitFinished(t); got != http.StatusInternalServerError {
		t.Fatalf("handler status = %d, want 500 after a cancelled store write", got)
	}
	if got := h.store.rowCount(); got != 0 {
		t.Fatalf("rows after cancelled store write = %d, want 0", got)
	}

	h.store.mu.Lock()
	h.store.stall = false
	h.store.mu.Unlock()
	h.retryStoresExactlyOne(t)
}

// TestIntakeLostAcknowledgementRetryCoalesces proves the ambiguous case: the
// store committed, but the client cancelled before it read the answer, so
// it cannot know. The row exists exactly once, and the retry reports
// duplicate_count 1 instead of writing a second row or losing the first.
func TestIntakeLostAcknowledgementRetryCoalesces(t *testing.T) {
	t.Parallel()

	h := newCancelHarness(t)
	h.hold = make(chan struct{})
	// The handler parks on h.hold, and httptest.Server.Close (registered by
	// newCancelHarness) waits for handlers. Register the release after it so
	// LIFO cleanup frees the handler first, and an early t.Fatal fails fast
	// instead of hanging until the go test timeout.
	var releaseOnce sync.Once
	releaseHold := func() { releaseOnce.Do(func() { close(h.hold) }) }
	t.Cleanup(releaseHold)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.server.URL+cancelPath, strings.NewReader(cancelBody))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+cancelToken)
	clientDone := make(chan error, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
		clientDone <- err
	}()

	select {
	case <-h.store.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("request never reached the store")
	}
	for deadline := time.Now().Add(5 * time.Second); h.store.rowCount() == 0; {
		if time.Now().After(deadline) {
			t.Fatal("store never committed the row")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-clientDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("client error = %v, want context.Canceled", err)
	}
	releaseHold()
	h.waitFinished(t)
	if got := h.store.rowCount(); got != 1 {
		t.Fatalf("rows after lost acknowledgement = %d, want 1 (fact not lost)", got)
	}
	if got := h.store.duplicates(); got != 0 {
		t.Fatalf("duplicate_count before retry = %d, want 0", got)
	}

	req, err = http.NewRequest(http.MethodPost, h.server.URL+cancelPath, strings.NewReader(cancelBody))
	if err != nil {
		t.Fatalf("build retry: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+cancelToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	_ = resp.Body.Close()
	h.waitFinished(t)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("retry status = %d, want 202", resp.StatusCode)
	}
	if got := h.store.rowCount(); got != 1 {
		t.Fatalf("rows after retry = %d, want exactly 1 (no second row)", got)
	}
	if got := h.store.duplicates(); got != 1 {
		t.Fatalf("duplicate_count after retry = %d, want 1", got)
	}
}
