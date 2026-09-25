// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package confluence

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

// TestHTTPClientDoesNotRetryEmptySuccessBody pins that a 200 with an empty
// body (a proxy, WAF, or misrouted base URL) is a persistent content problem,
// not a transient transport error: json decode returns a bare io.EOF there.
func TestHTTPClientDoesNotRetryEmptySuccessBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, BearerToken: "token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v", err)
	}

	_, err = client.GetPage(context.Background(), "123")
	if err == nil {
		t.Fatal("GetPage() error = nil, want a decode error for an empty 200 body")
	}
	if errors.Is(err, ErrRetryable) {
		t.Fatalf("GetPage() error = %v, an empty 200 body must not be retried as a transport error", err)
	}
}

// TestHTTPClientRetriesBodyTruncatedMidResponse pins that a connection dropped
// after the headers and part of the body is still a transient transport error.
func TestHTTPClientRetriesBodyTruncatedMidResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "4096")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"123","title":"Pay`))
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer server.Close()
	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, BearerToken: "token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v", err)
	}

	_, err = client.GetPage(context.Background(), "123")
	if !errors.Is(err, ErrRetryable) {
		t.Fatalf("GetPage() error = %v, want errors.Is(ErrRetryable) for a mid-body drop", err)
	}
}

// refusedSource builds a Source whose provider refuses every connection, the
// shape of a wrong host or port. now is advanced by the caller so each Next
// call is past the previous backoff window.
func refusedSource(t *testing.T, now *time.Time, logger *slog.Logger) *Source {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	baseURL := "http://" + listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: baseURL, BearerToken: "token"})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v", err)
	}
	return &Source{
		Client: client,
		Config: SourceConfig{BaseURL: baseURL, SpaceID: "100", Now: func() time.Time { return *now }},
		Logger: logger,
	}
}

func TestSourceEscalatesPersistentTransportFailureToFatal(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 25, 10, 0, 0, 0, time.UTC)
	source := refusedSource(t, &now, nil)

	limit := sdk.MaxConsecutiveTransportFailures
	for attempt := 1; attempt < limit; attempt++ {
		now = now.Add(time.Hour)
		if _, ok, err := source.Next(context.Background()); err != nil || ok {
			t.Fatalf("attempt %d: Next() = ok %v err %v, want a retried idle poll below the ceiling", attempt, ok, err)
		}
	}
	now = now.Add(time.Hour)
	_, ok, err := source.Next(context.Background())
	if err == nil || ok {
		t.Fatalf("attempt %d: Next() = ok %v err %v, want a fatal error at the consecutive-failure ceiling", limit, ok, err)
	}
	if errors.Is(err, ErrRetryable) {
		t.Fatalf("Next() error = %v, the escalated failure must not be retryable again", err)
	}
	if !strings.Contains(err.Error(), "consecutive") {
		t.Fatalf("Next() error = %q, want it to name the consecutive-failure ceiling", err)
	}
}

func TestSourceRetryLogCarriesAttemptAndCauseClass(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	now := time.Date(2026, time.September, 25, 10, 0, 0, 0, time.UTC)
	source := refusedSource(t, &now, logger)

	for range 2 {
		now = now.Add(time.Hour)
		if _, _, err := source.Next(context.Background()); err != nil {
			t.Fatalf("Next() error = %v", err)
		}
	}
	out := logs.String()
	for _, want := range []string{`"attempt":2`, `"cause_class":"refused"`, `"consecutive_transport_failures":2`} {
		if !strings.Contains(out, want) {
			t.Fatalf("retry log missing %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, "127.0.0.1") {
		t.Fatalf("retry log leaks the provider address:\n%s", out)
	}
}
