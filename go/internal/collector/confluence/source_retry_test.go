// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package confluence

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func TestSourceBacksOffRetryableFailureWithoutTerminalError(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 18, 17, 0, 0, 0, time.UTC)
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	source := Source{
		Client: errorClient{err: RetryableHTTPError{
			StatusCode: http.StatusTooManyRequests,
			RetryAfter: 45 * time.Second,
		}},
		Config: SourceConfig{
			BaseURL: "https://example.atlassian.net/wiki",
			SpaceID: "100",
			Now:     func() time.Time { return now },
		},
		Instruments: instruments,
	}

	_, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("first Next() error = %v, want nil retry backoff", err)
	}
	if ok {
		t.Fatal("first Next() ok = true, want retry backoff")
	}
	rm := collectConfluenceMetrics(t, reader)
	if got := confluenceCounterValue(t, rm, "eshu_dp_confluence_sync_failures_total", map[string]string{
		telemetry.MetricDimensionFailureClass: "rate_limited",
	}); got != 1 {
		t.Fatalf("sync failure counter = %d, want 1", got)
	}

	source.Client = &fakeClient{
		space:      Space{ID: "100", Key: "PLAT", Name: "Platform"},
		spacePages: []Page{confluencePage("123", "Payment Service Deployment", 17, `<p>body</p>`)},
	}
	now = now.Add(44 * time.Second)
	_, ok, err = source.Next(context.Background())
	if err != nil {
		t.Fatalf("second Next() error = %v, want nil while backoff is active", err)
	}
	if ok {
		t.Fatal("second Next() ok = true, want active backoff")
	}

	now = now.Add(2 * time.Second)
	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("third Next() error = %v, want nil after backoff expires", err)
	}
	if !ok {
		t.Fatal("third Next() ok = false, want retry after backoff")
	}
	assertFactCount(t, drainFacts(t, collected.Facts), facts.DocumentationDocumentFactKind, 1)
}
