// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// Seeded values that must never appear in any operator signal: a config
// secret, a fact payload value, and a grant scope value.
const (
	seedConfigSecret  = "SEED-CONFIG-TOKEN-9f3a"
	seedPayloadSecret = "SEED-PAYLOAD-VALUE-77c1"
	seedScopeSecret   = "SEED-GRANT-SCOPE-b204"
)

// grantSignalHarness wires the real adapter to in-memory trace, log, and
// metric sinks so tests observe exactly what an operator would.
type grantSignalHarness struct {
	adapter *GrantTelemetry
	spans   *tracetest.SpanRecorder
	tracer  *sdktrace.TracerProvider
	logs    *bytes.Buffer
	reader  *sdkmetric.ManualReader
}

func newGrantSignalHarness(t *testing.T) *grantSignalHarness {
	t.Helper()
	spans := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(mp.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return &grantSignalHarness{
		adapter: NewGrantTelemetry(inst, logger),
		spans:   spans, tracer: tp, logs: logs, reader: reader,
	}
}

func metricPoints(t *testing.T, reader *sdkmetric.ManualReader) []map[string]string {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	var points []map[string]string
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != contract.MetricProducerGrantDecisions {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				labels := map[string]string{}
				for _, kv := range dp.Attributes.ToSlice() {
					labels[string(kv.Key)] = kv.Value.AsString()
				}
				points = append(points, labels)
			}
		}
	}
	return points
}

func logRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line %q is not JSON: %v", line, err)
		}
		records = append(records, record)
	}
	return records
}

func eventAttrs(t *testing.T, spans *tracetest.SpanRecorder, name string) []map[string]string {
	t.Helper()
	var out []map[string]string
	for _, span := range spans.Ended() {
		for _, event := range span.Events() {
			if event.Name != name {
				continue
			}
			attrs := map[string]string{}
			for _, kv := range event.Attributes {
				attrs[string(kv.Key)] = kv.Value.AsString()
			}
			out = append(out, attrs)
		}
	}
	return out
}

// Allowed keys per surface. Any key outside these fails the test, so adding a
// field to a decision signal is a deliberate contract change.
var (
	allowedMetricKeys = []string{"decision", "stage", "reason", "fact_kind"}
	allowedEventKeys  = []string{
		"decision", "stage", "reason", "fact_kind",
		contract.SpanAttrProducerGrantProducerID, contract.SpanAttrProducerGrantVersion,
	}
	allowedLogKeys = []string{
		"time", "level", "msg",
		contract.LogKeyProducerGrantProducerID, contract.LogKeyProducerGrantVersion,
		contract.LogKeyProducerGrantDecision, contract.LogKeyProducerGrantStage,
		contract.LogKeyProducerGrantReason, contract.LogKeyProducerGrantFactKind,
	}
)

func assertNoSeed(t *testing.T, surface, value string) {
	t.Helper()
	for _, seed := range []string{seedConfigSecret, seedPayloadSecret, seedScopeSecret} {
		if strings.Contains(value, seed) {
			t.Fatalf("%s value %q contains seeded secret %q", surface, value, seed)
		}
	}
}

func seededDenyAndAllowSource(t *testing.T, observer component.GrantObserver, deny bool) (*Source, func() error) {
	t.Helper()
	manifest, grants := grantedCoreKindManifest()
	live := grants
	if deny {
		wrong := grants[0]
		wrong.Scope = seedScopeSecret
		live = []component.ProducerGrant{wrong}
	}
	item := testWorkItem()
	fact := testSDKFact(item)
	fact.Kind, fact.SchemaVersion = "aws_resource", "1.0.0"
	fact.Payload = map[string]any{"note": seedPayloadSecret}
	config := observedSourceConfig(
		manifest, grants,
		func() ([]component.ProducerGrant, error) { return live, nil },
		&recordingRunner{result: completeResult(item, fact)}, observer,
	)
	config.Config = map[string]any{"token": seedConfigSecret}
	source, err := NewSource(config)
	if err != nil {
		t.Fatalf("NewSource() error = %v, want nil", err)
	}
	return source, func() error {
		_, _, err := source.NextClaimed(t.Context(), item)
		return err
	}
}

// TestGrantTelemetryEmissionDenyEmitsAllThreeSignalsWithoutSecrets proves a
// denied emission (still failing closed) is visible on the metric, the span
// event, and the log, each with only allowlisted keys and no seeded secret.
func TestGrantTelemetryEmissionDenyEmitsAllThreeSignalsWithoutSecrets(t *testing.T) {
	h := newGrantSignalHarness(t)
	source, _ := seededDenyAndAllowSource(t, h.adapter, true)
	runCtx, runSpan := h.tracer.Tracer("test").Start(context.Background(), "claimed-run")
	if _, ok, err := source.NextClaimed(runCtx, testWorkItem()); err == nil || ok {
		t.Fatalf("NextClaimed() ok=%v err=%v, want terminal fail-closed deny", ok, err)
	}
	runSpan.End()

	points := metricPoints(t, h.reader)
	wantLabels := map[string]string{
		"decision": "deny", "stage": "emission", "reason": "scope_mismatch", "fact_kind": "aws_resource",
	}
	if !hasPoint(points, wantLabels) {
		t.Fatalf("metric points = %v, want one with %v", points, wantLabels)
	}
	for _, point := range points {
		for key, value := range point {
			if !slices.Contains(allowedMetricKeys, key) {
				t.Fatalf("metric label key %q outside allowlist", key)
			}
			assertNoSeed(t, "metric label "+key, value)
		}
	}

	events := eventAttrs(t, h.spans, contract.SpanEventProducerGrantDecision)
	var denyEvent map[string]string
	for _, event := range events {
		if event["decision"] == "deny" {
			denyEvent = event
		}
	}
	if denyEvent == nil {
		t.Fatalf("span events = %v, want a deny decision event", events)
	}
	for key, value := range denyEvent {
		if !slices.Contains(allowedEventKeys, key) {
			t.Fatalf("span event attribute key %q outside allowlist", key)
		}
		assertNoSeed(t, "span event "+key, value)
	}
	if denyEvent[contract.SpanAttrProducerGrantProducerID] != "dev.eshu.examples.scorecard" ||
		denyEvent["reason"] != "scope_mismatch" || denyEvent["stage"] != "emission" {
		t.Fatalf("deny event = %v, want producer, stage emission, reason scope_mismatch", denyEvent)
	}

	var denyLog map[string]any
	for _, record := range logRecords(t, h.logs) {
		for key, value := range record {
			if !slices.Contains(allowedLogKeys, key) {
				t.Fatalf("log key %q outside allowlist", key)
			}
			if str, ok := value.(string); ok {
				assertNoSeed(t, "log "+key, str)
			}
		}
		if record[contract.LogKeyProducerGrantDecision] == "deny" {
			denyLog = record
		}
	}
	if denyLog == nil || denyLog["level"] != "WARN" ||
		denyLog[contract.LogKeyProducerGrantReason] != "scope_mismatch" {
		t.Fatalf("deny log = %v, want a WARN record with reason scope_mismatch", denyLog)
	}
}

// TestGrantTelemetryAllowLogsAtActivationNotPerEmission proves the log-flood
// guard: an allow logs at activation (once) but the per-emission allow does
// not log, while the metric still counts it.
func TestGrantTelemetryAllowLogsAtActivationNotPerEmission(t *testing.T) {
	h := newGrantSignalHarness(t)
	_, run := seededDenyAndAllowSource(t, h.adapter, false)
	activationLogs := len(logRecords(t, h.logs))
	if activationLogs != 1 {
		t.Fatalf("log records after activation = %d, want 1 allow record", activationLogs)
	}
	if err := run(); err != nil {
		t.Fatalf("NextClaimed() error = %v, want admitted", err)
	}
	if got := len(logRecords(t, h.logs)); got != activationLogs {
		t.Fatalf("log records after an allowed emission = %d, want still %d (no per-emission allow log)", got, activationLogs)
	}
	for _, want := range []map[string]string{
		{"decision": "allow", "stage": "activation", "reason": "granted", "fact_kind": "aws_resource"},
		{"decision": "allow", "stage": "emission", "reason": "granted", "fact_kind": "aws_resource"},
	} {
		if !hasPoint(metricPoints(t, h.reader), want) {
			t.Fatalf("metric points = %v, want one with %v", metricPoints(t, h.reader), want)
		}
	}
}

func hasPoint(points []map[string]string, want map[string]string) bool {
	for _, point := range points {
		match := len(point) == len(want)
		for key, value := range want {
			if point[key] != value {
				match = false
			}
		}
		if match {
			return true
		}
	}
	return false
}

// TestGrantTelemetryVocabularyMatchesContract pins the component enums to the
// telemetry contract strings so the two closed vocabularies cannot drift.
func TestGrantTelemetryVocabularyMatchesContract(t *testing.T) {
	t.Parallel()

	pairs := map[string]string{
		string(component.GrantStageInstall):           contract.ProducerGrantStageInstall,
		string(component.GrantStageReadback):          contract.ProducerGrantStageReadback,
		string(component.GrantStageActivation):        contract.ProducerGrantStageActivation,
		string(component.GrantStageEmission):          contract.ProducerGrantStageEmission,
		string(component.GrantReasonGranted):          contract.ProducerGrantReasonGranted,
		string(component.GrantReasonNoMatchingGrant):  contract.ProducerGrantReasonNoMatchingGrant,
		string(component.GrantReasonRevoked):          contract.ProducerGrantReasonRevoked,
		string(component.GrantReasonExpired):          contract.ProducerGrantReasonExpired,
		string(component.GrantReasonScopeMismatch):    contract.ProducerGrantReasonScopeMismatch,
		string(component.GrantReasonSchemaNotCovered): contract.ProducerGrantReasonSchemaNotCovered,
		string(component.GrantReasonGrantsUnreadable): contract.ProducerGrantReasonGrantsUnreadable,
	}
	for got, want := range pairs {
		if got != want {
			t.Fatalf("component value %q != contract value %q", got, want)
		}
	}
	if grantDecisionLabel(true) != contract.ProducerGrantDecisionAllow ||
		grantDecisionLabel(false) != contract.ProducerGrantDecisionDeny {
		t.Fatal("grantDecisionLabel does not map to the contract decision values")
	}
}
