// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// coverageSignals captures the stage log and the empty-answer counter a
// handler emits, so a test can assert the operator-facing signals of one
// classified empty answer.
type coverageSignals struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	reader *sdkmetric.ManualReader
}

func (s *coverageSignals) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

// instrument wires a JSON debug logger and a manual-reader counter into h.
func (s *coverageSignals) instrument(t *testing.T, h *Handler) *Handler {
	t.Helper()
	s.reader = sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(s.reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	instruments, err := telemetry.NewInstruments(provider.Meter("security-alert-coverage-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	h.Instruments = instruments
	h.Logger = slog.New(slog.NewJSONHandler(s, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return h
}

// collectionStateEvents returns every completion record of the
// security_alert_collection_state stage.
func (s *coverageSignals) collectionStateEvents(t *testing.T) []map[string]any {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(s.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		if record["stage"] == securityAlertCollectionStateStage && record["event_name"] == "supply_chain_query.stage_completed" {
			out = append(out, record)
		}
	}
	return out
}

// emptyAnswerCounts returns the counter value per coverage_state|reason pair.
func (s *coverageSignals) emptyAnswerCounts(t *testing.T) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := s.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	out := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_query_security_alert_empty_answers_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("counter data = %T, want Sum[int64]", m.Data)
			}
			for _, point := range sum.DataPoints {
				state, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionCoverageState))
				reason, _ := point.Attributes.Value(attribute.Key("reason"))
				out[state.AsString()+"|"+reason.AsString()] += point.Value
			}
		}
	}
	return out
}
