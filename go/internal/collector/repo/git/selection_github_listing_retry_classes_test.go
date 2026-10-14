// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/rest"
)

// Compile-time proof that the typed discovery error is the collector's
// retryable contract (#8084).
var _ collector.RetryableError = (*rest.RetryableError)(nil)

// listRetryRecorder is an injectable retry config that records every backoff
// instead of waiting it out. The clock is fixed so x-ratelimit-reset
// arithmetic is exact.
type listRetryRecorder struct {
	mu    sync.Mutex
	waits []time.Duration
	now   time.Time
}

func newListRetryRecorder() *listRetryRecorder {
	return &listRetryRecorder{now: time.Date(2026, time.October, 14, 12, 0, 0, 0, time.UTC)}
}

func (r *listRetryRecorder) config(maxAttempts int) rest.Config {
	return rest.Config{
		MaxAttempts: maxAttempts,
		BaseBackoff: time.Second,
		Sleep: func(_ context.Context, d time.Duration) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.waits = append(r.waits, d)
			return nil
		},
		Now: func() time.Time { return r.now },
	}
}

func (r *listRetryRecorder) recorded() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Duration(nil), r.waits...)
}

func listRetryPageJSON(page int, count int) []map[string]any {
	items := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		items = append(items, map[string]any{
			"id":        int64(page*1000 + i),
			"full_name": fmt.Sprintf("acme/repo-%d-%03d", page, i),
			"archived":  false,
		})
	}
	return items
}

// listRetryFlakyServer serves page 1 with one repository and page 2 empty.
// failFirst runs for the first failures requests to page 1; it must write the
// whole response itself.
func listRetryFlakyServer(t *testing.T, failures int, failFirst func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *int) {
	t.Helper()
	var mu sync.Mutex
	pageOne := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "1" {
			mu.Lock()
			pageOne++
			fail := pageOne <= failures
			mu.Unlock()
			if fail {
				failFirst(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(listRetryPageJSON(1, 1))
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}))
	t.Cleanup(server.Close)
	return server, &pageOne
}

func listRetryRun(t *testing.T, server *httptest.Server, client *http.Client, retry rest.Config) ([]GitHubRepositoryRecord, bool, error) {
	t.Helper()
	if client == nil {
		client = server.Client()
	}
	return listGitHubOrgRepositoriesFromWithRetry(context.Background(), client, server.URL, "acme", 4000, "token-1", retry)
}

func listRetryAsDiscovery(t *testing.T, err error) *rest.RetryableError {
	t.Helper()
	var typed *rest.RetryableError
	if !errors.As(err, &typed) {
		t.Fatalf("error %v is not a *rest.RetryableError", err)
	}
	if !collector.IsRetryable(err) {
		t.Fatalf("collector.IsRetryable(%v) = false, want true", err)
	}
	return typed
}

// TestListingRetryRecoversFromTransportFaults is the #8084 regression for the
// fault shapes #8109 left final: a transport error from client.Do and a body
// that dies on a 200. Each must be retried to a complete listing.
func TestListingRetryRecoversFromTransportFaults(t *testing.T) {
	t.Parallel()

	reset := func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetLinger(0) // close sends RST: ECONNRESET on the client
		}
		_ = conn.Close()
	}
	shortBody := func(w http.ResponseWriter, _ *http.Request) {
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 500\r\n\r\n[{\"id\":1,\"full_na")
		_ = buf.Flush()
		_ = conn.Close()
	}

	cases := []struct {
		name   string
		fail   func(w http.ResponseWriter, r *http.Request)
		client func(*httptest.Server) *http.Client
	}{
		{name: "connection reset", fail: reset},
		{name: "EOF mid-body on a 200", fail: shortBody},
		{
			name: "client timeout",
			fail: func(_ http.ResponseWriter, r *http.Request) {
				select {
				case <-r.Context().Done():
				case <-time.After(5 * time.Second):
				}
			},
			client: func(s *httptest.Server) *http.Client {
				c := *s.Client()
				c.Timeout = 50 * time.Millisecond
				return &c
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server, pageOne := listRetryFlakyServer(t, 1, tc.fail)
			var client *http.Client
			if tc.client != nil {
				client = tc.client(server)
			}
			rec := newListRetryRecorder()
			records, complete, err := listRetryRun(t, server, client, rec.config(4))
			if err != nil {
				t.Fatalf("listing error = %v, want recovery", err)
			}
			if len(records) != 1 || !complete {
				t.Fatalf("listed %d complete %v, want 1 complete true", len(records), complete)
			}
			if *pageOne != 2 {
				t.Fatalf("page-1 attempts = %d, want 2", *pageOne)
			}
			if got := rec.recorded(); len(got) != 1 || got[0] != time.Second {
				t.Fatalf("waits = %v, want [1s]", got)
			}
		})
	}
}

// TestListingRetryRecoversFromHTTP2StreamReset proves the real net/http
// HTTP/2 body failure (the 2026-10-13 stream CANCEL shape). The predicate
// sdk.IsTransientTransportError does NOT accept it: the bundled
// http2StreamError is unexported and wraps neither io.ErrUnexpectedEOF nor a
// net.Error. This test pins the explicit type check that closes that gap.
func TestListingRetryRecoversFromHTTP2StreamReset(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	pageOne := 0
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			_ = json.NewEncoder(w).Encode([]map[string]any{})
			return
		}
		mu.Lock()
		pageOne++
		fail := pageOne == 1
		mu.Unlock()
		if fail {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"id":1,"full_na`))
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler) // RST_STREAM mid-body
		}
		_ = json.NewEncoder(w).Encode(listRetryPageJSON(1, 1))
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)

	rec := newListRetryRecorder()
	records, complete, err := listGitHubOrgRepositoriesFromWithRetry(
		context.Background(), server.Client(), server.URL, "acme", 100, "token-1", rec.config(4),
	)
	if err != nil {
		t.Fatalf("listing error = %v, want recovery from the HTTP/2 stream reset", err)
	}
	if len(records) != 1 || !complete {
		t.Fatalf("listed %d complete %v, want 1 complete true", len(records), complete)
	}
	if got := rec.recorded(); len(got) != 1 {
		t.Fatalf("waits = %v, want one retry", got)
	}
}

// TestListingRetryFinalErrorsDoNotRetry pins the classes that never clear on
// retry: one attempt, no sleep, and not a retryable error.
func TestListingRetryFinalErrorsDoNotRetry(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"401", func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "{}", http.StatusUnauthorized) }},
		{"403 without a rate-limit signal", func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "{}", http.StatusForbidden) }},
		{"403 with remaining above zero", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-RateLimit-Remaining", "12")
			http.Error(w, "{}", http.StatusForbidden)
		}},
		{"404", func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "{}", http.StatusNotFound) }},
		{"422", func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "{}", http.StatusUnprocessableEntity) }},
		{"3xx", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotModified) }},
		{"invalid JSON on a full body", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("not-json")) }},
		{"empty 200 body", func(w http.ResponseWriter, _ *http.Request) {}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			attempts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				attempts++
				mu.Unlock()
				tc.handler(w, r)
			}))
			t.Cleanup(server.Close)
			rec := newListRetryRecorder()
			_, complete, err := listRetryRun(t, server, nil, rec.config(4))
			if err == nil || complete {
				t.Fatalf("= complete %v, err %v; want an error and incomplete", complete, err)
			}
			if collector.IsRetryable(err) {
				t.Fatalf("error %v is retryable, want final", err)
			}
			mu.Lock()
			defer mu.Unlock()
			if attempts != 1 {
				t.Fatalf("attempts = %d, want 1", attempts)
			}
			if got := rec.recorded(); len(got) != 0 {
				t.Fatalf("waits = %v, want none", got)
			}
		})
	}
}

// TestListingRetryExhaustionIsTypedAndBounded pins that a persistent 5xx and
// repeated short rate limits each stop at maxAttempts with the typed error.
func TestListingRetryExhaustionIsTypedAndBounded(t *testing.T) {
	t.Parallel()

	t.Run("persistent 504", func(t *testing.T) {
		t.Parallel()
		var mu sync.Mutex
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			mu.Lock()
			attempts++
			mu.Unlock()
			http.Error(w, "{}", http.StatusGatewayTimeout)
		}))
		t.Cleanup(server.Close)
		rec := newListRetryRecorder()
		_, _, err := listRetryRun(t, server, nil, rec.config(4))
		typed := listRetryAsDiscovery(t, err)
		if typed.Attempts != 4 || typed.Class != "server_error" || typed.RetryAfter != 0 {
			t.Fatalf("typed = %+v, want 4 attempts server_error RetryAfter 0", typed)
		}
		mu.Lock()
		defer mu.Unlock()
		if attempts != 4 {
			t.Fatalf("attempts = %d, want 4", attempts)
		}
		want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
		if got := rec.recorded(); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("waits = %v, want %v", got, want)
		}
	})

	t.Run("short rate limit consumes attempts", func(t *testing.T) {
		t.Parallel()
		var mu sync.Mutex
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			mu.Lock()
			attempts++
			mu.Unlock()
			w.Header().Set("Retry-After", "1")
			http.Error(w, "{}", http.StatusTooManyRequests)
		}))
		t.Cleanup(server.Close)
		rec := newListRetryRecorder()
		_, _, err := listRetryRun(t, server, nil, rec.config(4))
		typed := listRetryAsDiscovery(t, err)
		if typed.Attempts != 4 || typed.RetryAfter != time.Second {
			t.Fatalf("typed = %+v, want 4 attempts RetryAfter 1s", typed)
		}
		mu.Lock()
		defer mu.Unlock()
		if attempts != 4 {
			t.Fatalf("attempts = %d, want 4", attempts)
		}
	})

	t.Run("typed error unwraps to the cause", func(t *testing.T) {
		t.Parallel()
		server, _ := listRetryFlakyServer(t, 10, func(w http.ResponseWriter, _ *http.Request) {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
		})
		rec := newListRetryRecorder()
		_, _, err := listRetryRun(t, server, nil, rec.config(2))
		typed := listRetryAsDiscovery(t, err)
		var netErr net.Error
		if typed.Attempts != 2 || !errors.As(err, &netErr) {
			t.Fatalf("typed = %+v, want 2 attempts and a net.Error cause (errors.As)", typed)
		}
	})
}

// TestListingRetryKeepsEarlierPagesAndPageSize pins that a transient fault on
// page N > 1 retries that page only, never re-reads earlier pages, and never
// changes per_page mid-listing.
func TestListingRetryKeepsEarlierPagesAndPageSize(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	hits := map[string]int{}
	perPage := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		mu.Lock()
		hits[page]++
		n := hits[page]
		perPage[r.URL.Query().Get("per_page")] = true
		mu.Unlock()
		switch page {
		case "1":
			_ = json.NewEncoder(w).Encode(listRetryPageJSON(1, githubListingPageSize))
		case "2":
			if n == 1 {
				http.Error(w, "{}", http.StatusBadGateway)
				return
			}
			_ = json.NewEncoder(w).Encode(listRetryPageJSON(2, 1))
		default:
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		}
	}))
	t.Cleanup(server.Close)

	rec := newListRetryRecorder()
	records, complete, err := listRetryRun(t, server, nil, rec.config(4))
	if err != nil || !complete {
		t.Fatalf("= complete %v err %v, want a complete listing", complete, err)
	}
	if len(records) != githubListingPageSize+1 {
		t.Fatalf("listed %d, want %d", len(records), githubListingPageSize+1)
	}
	mu.Lock()
	defer mu.Unlock()
	if hits["1"] != 1 || hits["2"] != 2 || hits["3"] != 1 {
		t.Fatalf("hits = %v, want page 1 once, page 2 twice, page 3 once", hits)
	}
	if len(perPage) != 1 || !perPage[strconv.Itoa(githubListingPageSize)] {
		t.Fatalf("per_page values = %v, want only %d", perPage, githubListingPageSize)
	}
}
