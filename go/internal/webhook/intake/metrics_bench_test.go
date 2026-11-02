// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package intake

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// BenchmarkIntakeRepositoryRequest measures the per-request cost of the
// request-path series (#9811): one request counter increment, one request
// duration observation, and the store operation, duration, and decision
// recorded on the stored path. Compare "uninstrumented" with "instrumented".
func BenchmarkIntakeRepositoryRequest(b *testing.B) {
	body := marshalBench(b, intakeRoutes()[0].payload)
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewManualReader()))
	inst, err := telemetry.NewInstruments(provider.Meter("intake-bench"))
	if err != nil {
		b.Fatalf("NewInstruments() error = %v", err)
	}
	for _, tc := range []struct {
		name string
		inst *telemetry.Instruments
	}{{"uninstrumented", nil}, {"instrumented", inst}} {
		b.Run(tc.name, func(b *testing.B) {
			mux := NewServer(ServerConfig{Token: "core-intake-token", Instruments: tc.inst}, testStores().stores()).Mux()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/intake/repository", bytes.NewReader(body))
				req.Header.Set("Authorization", "Bearer core-intake-token")
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)
				if rec.Code != http.StatusAccepted {
					b.Fatalf("status = %d, want 202", rec.Code)
				}
			}
		})
	}
}

// BenchmarkIntakeGCPRequest measures the same request-path cost on the GCP
// freshness route, which since #9814 also increments
// eshu_dp_gcp_freshness_events_total{kind,action,auth_path}. Compare
// "uninstrumented" with "instrumented"; the difference from
// BenchmarkIntakeRepositoryRequest is the freshness counter add.
func BenchmarkIntakeGCPRequest(b *testing.B) {
	body := marshalBench(b, intakeRoutes()[3].payload)
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewManualReader()))
	inst, err := telemetry.NewInstruments(provider.Meter("intake-bench"))
	if err != nil {
		b.Fatalf("NewInstruments() error = %v", err)
	}
	for _, tc := range []struct {
		name string
		inst *telemetry.Instruments
	}{{"uninstrumented", nil}, {"instrumented", inst}} {
		b.Run(tc.name, func(b *testing.B) {
			mux := NewServer(ServerConfig{Token: "core-intake-token", Instruments: tc.inst}, testStores().stores()).Mux()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/intake/gcp", bytes.NewReader(body))
				req.Header.Set("Authorization", "Bearer core-intake-token")
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)
				if rec.Code != http.StatusAccepted {
					b.Fatalf("status = %d, want 202", rec.Code)
				}
			}
		})
	}
}

func marshalBench(b *testing.B, payload map[string]any) []byte {
	b.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		b.Fatalf("marshal: %v", err)
	}
	return body
}
