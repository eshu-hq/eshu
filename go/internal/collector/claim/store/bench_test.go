// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/collector/claim/store"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// BenchmarkClaimPlaneHeartbeat measures one heartbeat round trip through the
// client store.New builds, with and without the request counter and duration
// histogram bound to real OTEL instruments, to bound the cost of the Observe
// hook on the same request.
func BenchmarkClaimPlaneHeartbeat(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewManualReader())).Meter("bench"))
	if err != nil {
		b.Fatalf("NewInstruments() error = %v", err)
	}
	for _, variant := range []struct {
		name        string
		instruments *telemetry.Instruments
	}{{"unobserved", nil}, {"observed", instruments}} {
		b.Run(variant.name, func(b *testing.B) {
			selection, err := store.New(context.Background(), store.Options{
				Service:     "collector-bench",
				Getenv:      func(string) string { return server.URL },
				Postgres:    &postgresStoreStub{},
				HTTPClient:  server.Client(),
				Instruments: variant.instruments,
			})
			if err != nil {
				b.Fatalf("New() error = %v", err)
			}
			ctx := context.Background()
			b.ReportAllocs()
			for b.Loop() {
				if err := selection.Store.HeartbeatClaim(ctx, workflow.ClaimMutation{}); err != nil {
					b.Fatalf("HeartbeatClaim() error = %v", err)
				}
			}
		})
	}
}
