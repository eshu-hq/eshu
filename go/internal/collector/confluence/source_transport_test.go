// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package confluence

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// resetPageServer serves a healthy space listing but drops the TCP connection
// on every single-page GET, reproducing the ops-qa
// "read: connection reset by peer" that killed the collector.
func resetPageServer(t *testing.T, pageGets *atomic.Int64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/spaces/100"):
			_ = json.NewEncoder(w).Encode(Space{ID: "100", Key: "PLAT", Name: "Platform"})
		case strings.HasSuffix(r.URL.Path, "/spaces/100/pages"):
			_ = json.NewEncoder(w).Encode(pageListResponse{
				Results: []Page{confluencePage("123", "Payment", 1, "<p>body</p>")},
			})
		default:
			pageGets.Add(1)
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("Hijack() error = %v", err)
				return
			}
			_ = conn.Close()
		}
	}))
}

func TestSourceBacksOffOnTransportErrorFetchingOnePage(t *testing.T) {
	t.Parallel()

	var pageGets atomic.Int64
	server := resetPageServer(t, &pageGets)
	defer server.Close()
	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, BearerToken: "token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v", err)
	}
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	now := time.Date(2026, time.September, 25, 10, 13, 0, 0, time.UTC)
	source := Source{
		Client:      client,
		Config:      SourceConfig{BaseURL: server.URL, SpaceID: "100", Now: func() time.Time { return now }},
		Instruments: instruments,
	}

	_, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v, want nil transport backoff (collector must not exit)", err)
	}
	if ok {
		t.Fatal("Next() ok = true, want retry backoff after transport error")
	}
	// net/http itself may replay an idempotent GET once on a reused
	// connection, so the count after the first cycle is not pinned to one.
	getsAfterFirstCycle := pageGets.Load()
	if getsAfterFirstCycle < 1 {
		t.Fatalf("page GET count = %d, want at least 1", getsAfterFirstCycle)
	}
	rm := collectConfluenceMetrics(t, reader)
	if got := confluenceCounterValue(t, rm, "eshu_dp_confluence_sync_failures_total", map[string]string{
		telemetry.MetricDimensionFailureClass: "transport_error",
	}); got != 1 {
		t.Fatalf("transport_error sync failure counter = %d, want 1", got)
	}

	// Backoff is active: the source must not hit the provider again yet.
	if _, ok, err = source.Next(context.Background()); err != nil || ok {
		t.Fatalf("Next() during backoff = ok %v err %v, want false nil", ok, err)
	}
	if got := pageGets.Load(); got != getsAfterFirstCycle {
		t.Fatalf("page GET count during backoff = %d, want %d (no provider call)", got, getsAfterFirstCycle)
	}
}

func TestHTTPClientWrapsTransportErrorAsRetryable(t *testing.T) {
	t.Parallel()

	var pageGets atomic.Int64
	server := resetPageServer(t, &pageGets)
	defer server.Close()
	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, BearerToken: "token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v", err)
	}

	_, err = client.GetPage(context.Background(), "123")
	if !errors.Is(err, ErrRetryable) {
		t.Fatalf("GetPage() error = %v, want errors.Is(ErrRetryable)", err)
	}
}

func TestHTTPClientDoesNotClassifyCancellationAsRetryable(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer server.Close()
	defer close(release)
	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, BearerToken: "token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err = client.GetPage(ctx, "123")
	if err == nil {
		t.Fatal("GetPage() error = nil, want cancellation error")
	}
	if errors.Is(err, ErrRetryable) {
		t.Fatalf("GetPage() error = %v, cancellation must not be retryable", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("GetPage() error = %v, want errors.Is(context.Canceled)", err)
	}
}
