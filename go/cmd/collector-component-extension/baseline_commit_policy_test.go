// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestClaimCommitPolicyBaselineRefusalEmitsCounterAndWarn pins the collector
// command wiring of the NVD baseline commit policy (#10762 F6). It builds the
// policy through claimCommitPolicy, the same call the command makes, and
// drives a baseline generation that carries a window advance verdict, which
// the baseline policy must refuse. The refusal must reach the operator as the
// refused advance counter and the WARN line. If claimCommitPolicy dropped the
// baseline policy, or built it without the instruments or the logger, the
// host would still refuse through its built-in fallback but emit neither
// signal, which is the original F6 failure; this test fails for each.
func TestClaimCommitPolicyBaselineRefusalEmitsCounterAndWarn(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	policy, err := claimCommitPolicy(scope.CollectorVulnerabilityIntelligence, "vuln-intel-cx", instruments, logger)
	if err != nil || policy == nil {
		t.Fatalf("claimCommitPolicy(vulnerability_intelligence) = (%v, %v), want a policy", policy, err)
	}

	item := workflow.WorkItem{
		WorkItemID: "work-baseline-1",
		ScopeID:    "vuln-intel://nvd/_baseline/baseline-2026-11/2002/1009843200000000",
	}
	envelopes := []facts.Envelope{{
		FactKind:      "vulnerability.source_snapshot",
		StableFactKey: "vulnerability.source_snapshot:nvd",
		Payload: map[string]any{
			"coverage_descriptor":           map[string]any{"version": "nvd.v1"},
			"nvd_window_advance_to_unix_us": int64(1767398400000000),
			"nvd_window_head_unix_us":       int64(1767398400000000),
			"nvd_window_should_advance":     true,
		},
	}}
	decision, err := policy.CommitPolicy(item, time.Now().UTC(), envelopes)
	var refusal *extensionhost.CommitPolicyRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("CommitPolicy() error = %v (decision %+v), want a *CommitPolicyRefusal from the wired baseline policy", err, decision)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var refused int64
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metric := range scopeMetrics.Metrics {
			if metric.Name != "eshu_dp_nvd_baseline_advance_total" {
				continue
			}
			data, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("advance metric data type = %T, want Sum[int64]", metric.Data)
			}
			for _, point := range data.DataPoints {
				for _, attr := range point.Attributes.ToSlice() {
					if string(attr.Key) == "outcome" && attr.Value.AsString() == "refused" {
						refused += point.Value
					}
				}
			}
		}
	}
	if refused != 1 {
		t.Fatalf("advance outcome refused = %d, want 1 through claimCommitPolicy", refused)
	}

	const refusalMessage = "nvd baseline generation refused by commit policy, nothing committed"
	var sawRefusal bool
	for _, line := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var record struct {
			Level string `json:"level"`
			Msg   string `json:"msg"`
		}
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("log line is not JSON: %q: %v", line, err)
		}
		if record.Msg != refusalMessage {
			continue
		}
		sawRefusal = true
		if record.Level != slog.LevelWarn.String() {
			t.Fatalf("refusal log level = %q, want %q", record.Level, slog.LevelWarn.String())
		}
	}
	if !sawRefusal {
		t.Fatalf("refusal WARN missing from logs: %q", logs.String())
	}
}
