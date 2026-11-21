// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestFanOutDeadLetterOutcomesCarryPriorClass proves the #11147 operator
// signals: a reopened dead letter and a skipped one each count one
// fan-out datapoint with the prior failure class and write one info line
// naming it, and a holder with no active generation counts its own outcome
// instead of pending_noop.
func TestFanOutDeadLetterOutcomesCarryPriorClass(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{
		holders: []string{"holder-dead", "holder-unsafe", "holder-gone", "holder-ok"},
		outcomes: map[string]FanOutOutcome{
			"holder-dead":   FanOutDeadLetterReopened,
			"holder-unsafe": FanOutDeadLetterSkipped,
			"holder-gone":   FanOutNoActiveGeneration,
		},
		priorClasses: map[string]string{
			"holder-dead":   SupplyChainImpactFanOutFailureClass,
			"holder-unsafe": "input_invalid",
		},
	}
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	var logs bytes.Buffer
	handler := fanOutHandler(port, &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}, inst)
	handler.Logger = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if _, err := handler.Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	classes := fanOutClassesByOutcome(t, reader)
	if got := classes[string(FanOutDeadLetterReopened)]; len(got) != 1 || got[SupplyChainImpactFanOutFailureClass] != 1 {
		t.Fatalf("dead_letter_reopened series = %v, want one at %s", got, SupplyChainImpactFanOutFailureClass)
	}
	if got := classes[string(FanOutDeadLetterSkipped)]; len(got) != 1 || got["input_invalid"] != 1 {
		t.Fatalf("dead_letter_skipped series = %v, want one at input_invalid", got)
	}
	if got := classes[string(FanOutNoActiveGeneration)]; got[""] != 1 {
		t.Fatalf("no_active_generation series = %v, want one with no class", got)
	}
	if got := classes[string(FanOutPendingNoop)]; len(got) != 0 {
		t.Fatalf("pending_noop series = %v, want none (no holder was queued or flagged)", got)
	}
	if got := classes[string(FanOutReopened)]; got[""] != 1 {
		t.Fatalf("reopened series = %v, want one", got)
	}

	var deadLines []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		if _, ok := record["prior_failure_class"]; ok {
			deadLines = append(deadLines, record)
		}
	}
	if len(deadLines) != 2 {
		t.Fatalf("dead-letter log lines = %d, want 2: %s", len(deadLines), logs.String())
	}
	for _, record := range deadLines {
		if record["level"] != "INFO" {
			t.Fatalf("dead-letter line level = %v, want INFO", record["level"])
		}
		want := map[string]string{"holder-dead": SupplyChainImpactFanOutFailureClass, "holder-unsafe": "input_invalid"}[record["holder_scope_id"].(string)]
		if record["prior_failure_class"] != want {
			t.Fatalf("line %v names class %v, want %q", record, record["prior_failure_class"], want)
		}
	}
}

// fanOutClassesByOutcome returns the fan-out counter's datapoints as outcome
// to failure_class to value; a datapoint with no class keys under "".
func fanOutClassesByOutcome(t *testing.T, reader *sdkmetric.ManualReader) map[string]map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	out := map[string]map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_fanout_total" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				outcome, _ := dp.Attributes.Value("outcome")
				class, _ := dp.Attributes.Value(telemetry.MetricDimensionFailureClass)
				if out[outcome.AsString()] == nil {
					out[outcome.AsString()] = map[string]int64{}
				}
				out[outcome.AsString()][class.AsString()] += dp.Value
			}
		}
	}
	return out
}
