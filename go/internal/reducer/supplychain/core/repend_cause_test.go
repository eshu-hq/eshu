// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestRependCauseOfDerivesEachCauseFromItsPayloadShape pins the #10972 cause
// table: every cause is read from what the claim already loaded (attempt
// count, the fanout_origin stamp, the owed marker, the continuation list and
// the enqueue/cycle timestamps), never from a new query.
func TestRependCauseOfDerivesEachCauseFromItsPayloadShape(t *testing.T) {
	t.Parallel()

	enqueued := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	stamp := map[string]any{"scope_id": "s", "generation_id": "g", "intent_id": "i", "cves": []any{"CVE-1"}}
	cases := []struct {
		name    string
		attempt int
		payload map[string]any
		enq     time.Time
		cycle   time.Time
		want    string
	}{
		{"never reopened first claim", 1, nil, enqueued, enqueued, telemetry.SupplyChainImpactRependCauseFirst},
		{"first claim with an unrelated payload key", 1, map[string]any{"x": 1}, enqueued, enqueued, telemetry.SupplyChainImpactRependCauseFirst},
		{"zero timestamps cannot prove a first claim", 1, nil, time.Time{}, time.Time{}, telemetry.SupplyChainImpactRependCauseUnknown},
		{"second attempt is a retry", 2, nil, enqueued, enqueued, telemetry.SupplyChainImpactRependCauseRetry},
		{"retry wins over a stamp", 3, map[string]any{PayloadKeyFanOutOrigin: stamp}, enqueued, enqueued.Add(time.Minute), telemetry.SupplyChainImpactRependCauseRetry},
		{"stamped first attempt after a reopen is a fan-out rerun", 1, map[string]any{PayloadKeyFanOutOrigin: stamp}, enqueued, enqueued.Add(time.Minute), telemetry.SupplyChainImpactRependCauseFanOutRerun},
		{"stamp alone proves a fan-out rerun without timestamps", 1, map[string]any{PayloadKeyFanOutOrigin: stamp}, time.Time{}, time.Time{}, telemetry.SupplyChainImpactRependCauseFanOutRerun},
		{"continuation list is a drain", 1, map[string]any{PayloadKeyFanOutRemaining: []any{"holder-a"}}, enqueued, enqueued.Add(time.Minute), telemetry.SupplyChainImpactRependCauseDrain},
		{"drain wins over the stamp", 1, map[string]any{PayloadKeyFanOutRemaining: []any{"holder-a"}, PayloadKeyFanOutOrigin: stamp}, enqueued, enqueued.Add(time.Minute), telemetry.SupplyChainImpactRependCauseDrain},
		{"a retry of a drain is a retry", 2, map[string]any{PayloadKeyFanOutRemaining: []any{"holder-a"}}, enqueued, enqueued.Add(time.Minute), telemetry.SupplyChainImpactRependCauseRetry},
		{"empty continuation list is not a drain", 1, map[string]any{PayloadKeyFanOutRemaining: []any{}}, enqueued, enqueued, telemetry.SupplyChainImpactRependCauseFirst},
		{"re-pended without a stamp", 1, nil, enqueued, enqueued.Add(time.Minute), telemetry.SupplyChainImpactRependCauseOtherRepend},
		{"owed marker with a stamp is a genuine pass, re-pended", 1, map[string]any{PayloadKeyFanOutOrigin: stamp, PayloadKeyFanOutOwed: true}, enqueued, enqueued.Add(time.Minute), telemetry.SupplyChainImpactRependCauseOtherRepend},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			intent := reducercontract.Intent{
				Domain:         reducercontract.DomainSupplyChainImpact,
				AttemptCount:   tc.attempt,
				Payload:        tc.payload,
				EnqueuedAt:     tc.enq,
				CycleStartedAt: tc.cycle,
			}
			if got := rependCauseOf(intent); got != tc.want {
				t.Fatalf("rependCauseOf() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestPassChangeOfClassifiesTheWriteResult pins the changed and change_class
// labels. change_class stays none for an unchanged pass and unknown for a
// changed one: telling an enrichment-only change from another one needs the
// stored row's fields, which the writer's digest read does not return, and
// this change adds no read to the hot path.
func TestPassChangeOfClassifiesTheWriteResult(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		write       SupplyChainImpactWriteResult
		wantChanged string
		wantClass   string
	}{
		{"nothing changed", SupplyChainImpactWriteResult{FactsWritten: 5}, telemetry.SupplyChainImpactPassUnchanged, telemetry.SupplyChainImpactChangeClassNone},
		{"a CVE changed", SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-1"}}, telemetry.SupplyChainImpactPassChanged, telemetry.SupplyChainImpactChangeClassUnknown},
		{"only a group was touched", SupplyChainImpactWriteResult{TouchedGroupIDs: []string{"grp"}}, telemetry.SupplyChainImpactPassChanged, telemetry.SupplyChainImpactChangeClassUnknown},
	}
	for _, tc := range cases {
		changed, class := passChangeOf(tc.write)
		if changed != tc.wantChanged || class != tc.wantClass {
			t.Fatalf("%s: passChangeOf() = (%q, %q), want (%q, %q)", tc.name, changed, class, tc.wantChanged, tc.wantClass)
		}
	}
}

type passOutcomeSeries struct {
	mode, cause, changed, class, reason string
}

// passOutcomeSeriesOf reads every eshu_dp_supply_chain_impact_pass_outcome_total
// point and the label keys each carries.
func passOutcomeSeriesOf(t *testing.T, reader *sdkmetric.ManualReader) (map[passOutcomeSeries]int64, [][]string) {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	out := map[passOutcomeSeries]int64{}
	var keySets [][]string
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_pass_outcome_total" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				get := func(key string) string {
					v, _ := dp.Attributes.Value(attribute.Key(key))
					return v.AsString()
				}
				out[passOutcomeSeries{get("mode"), get("repend_cause"), get("changed"), get("change_class"), get("pass_reason")}] += dp.Value
				var keys []string
				for _, kv := range dp.Attributes.ToSlice() {
					keys = append(keys, string(kv.Key))
				}
				sort.Strings(keys)
				keySets = append(keySets, keys)
			}
		}
	}
	return out, keySets
}

// TestPassOutcomeCounterIsBoundedAcrossManyScopesAndCVEs runs many passes,
// each with its own scope, generation, intent and CVE ids, through every
// cause shape, then asserts the series set stayed inside the closed enums
// and carried only the five bounded label keys: no id reached a label.
func TestPassOutcomeCounterIsBoundedAcrossManyScopesAndCVEs(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	enqueued := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	shapes := []func(i int) reducercontract.Intent{
		func(i int) reducercontract.Intent { // first
			return reducercontract.Intent{AttemptCount: 1, EnqueuedAt: enqueued, CycleStartedAt: enqueued}
		},
		func(i int) reducercontract.Intent { // retry
			return reducercontract.Intent{AttemptCount: 2 + i%3, EnqueuedAt: enqueued, CycleStartedAt: enqueued}
		},
		func(i int) reducercontract.Intent { // fan-out rerun
			return reducercontract.Intent{AttemptCount: 1, Payload: scopedStampPayload(fmt.Sprintf("CVE-2026-%04d", i))}
		},
		func(i int) reducercontract.Intent { // drain
			return reducercontract.Intent{AttemptCount: 1, Payload: map[string]any{PayloadKeyFanOutRemaining: []any{fmt.Sprintf("holder-%d", i)}}}
		},
		func(i int) reducercontract.Intent { // other re-pend
			return reducercontract.Intent{AttemptCount: 1, EnqueuedAt: enqueued, CycleStartedAt: enqueued.Add(time.Duration(i+1) * time.Second)}
		},
	}
	const passes = 60
	for i := 0; i < passes; i++ {
		loader := &scopedSeedLoader{stubSupplyChainImpactFactLoader: &stubSupplyChainImpactFactLoader{}}
		handler, _, writer := scopedPassHandler(loader, inst)
		if i%2 == 0 {
			writer.changed = []string{fmt.Sprintf("CVE-2026-%04d", i)}
		}
		intent := shapes[i%len(shapes)](i)
		intent.IntentID = fmt.Sprintf("intent-%d", i)
		intent.ScopeID = fmt.Sprintf("scope:%d", i)
		intent.GenerationID = fmt.Sprintf("gen-%d", i)
		intent.Domain = reducercontract.DomainSupplyChainImpact
		if _, err := handler.Handle(context.Background(), intent); err != nil {
			t.Fatalf("Handle(%d) error = %v", i, err)
		}
	}

	series, keySets := passOutcomeSeriesOf(t, reader)
	if len(series) == 0 {
		t.Fatal("no pass-outcome series recorded")
	}
	var total int64
	for key, value := range series {
		total += value
		if !slices.Contains(telemetry.SupplyChainImpactRependCauses(), key.cause) {
			t.Fatalf("cause %q outside the closed set", key.cause)
		}
		if !slices.Contains(telemetry.SupplyChainImpactPassChanges(), key.changed) {
			t.Fatalf("changed %q outside the closed set", key.changed)
		}
		if !slices.Contains(telemetry.SupplyChainImpactChangeClasses(), key.class) {
			t.Fatalf("change_class %q outside the closed set", key.class)
		}
		if !slices.Contains(telemetry.SupplyChainImpactPassReasons(), key.reason) {
			t.Fatalf("pass_reason %q outside the closed set", key.reason)
		}
		if !slices.Contains([]string{passModeFull, passModeScoped, passModeScopedFallbackSentinel, passModeScopedFallbackOutOfStamp}, key.mode) {
			t.Fatalf("mode %q outside the closed set", key.mode)
		}
	}
	if total != passes {
		t.Fatalf("recorded %d passes, want %d (one per completed pass)", total, passes)
	}
	bound := len(telemetry.SupplyChainImpactRependCauses()) * len(telemetry.SupplyChainImpactPassChanges()) * len(telemetry.SupplyChainImpactPassReasons()) * 4
	if len(series) > bound {
		t.Fatalf("series = %d, want at most %d", len(series), bound)
	}
	want := []string{"change_class", "changed", "domain", "mode", "pass_reason", "repend_cause"}
	for _, keys := range keySets {
		if !slices.Equal(keys, want) {
			t.Fatalf("label keys = %v, want %v", keys, want)
		}
	}
}

// TestPassOutcomeSignalsCarryCauseChangedAndClassOnEverySurface pins that the
// counter point, the pass-complete log line and the span attributes agree for
// one stamped re-pend that rewrote rows.
func TestPassOutcomeSignalsCarryCauseChangedAndClassOnEverySurface(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	loader := &scopedSeedLoader{stubSupplyChainImpactFactLoader: &stubSupplyChainImpactFactLoader{}}
	handler, _, writer := scopedPassHandler(loader, inst)
	writer.changed = []string{"CVE-2026-9001"}
	var logs bytes.Buffer
	handler.Logger = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	spans := tracetest.NewSpanRecorder()
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)).Tracer("test")
	ctx, span := tracer.Start(context.Background(), "reducer.run")

	intent := fanOutIntent(1, scopedStampPayload("CVE-2026-9001"))
	intent.Domain = reducercontract.DomainSupplyChainImpact
	if _, err := handler.Handle(ctx, intent); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	span.End()

	series, _ := passOutcomeSeriesOf(t, reader)
	var got passOutcomeSeries
	for key := range series {
		got = key
	}
	if len(series) != 1 || got.cause != "fanout_rerun" || got.changed != "changed" || got.class != "unknown" || got.reason != "no_seed" {
		t.Fatalf("series = %v, want one fanout_rerun/changed/unknown/no_seed point", series)
	}

	var line map[string]any
	for _, raw := range bytes.Split(logs.Bytes(), []byte("\n")) {
		var record map[string]any
		if json.Unmarshal(raw, &record) == nil && record["msg"] == "supply chain impact pass complete" {
			line = record
		}
	}
	if line == nil {
		t.Fatalf("no pass-complete log line in %s", logs.String())
	}
	for key, want := range map[string]string{"repend_cause": "fanout_rerun", "changed": "changed", "change_class": "unknown", "mode": got.mode, "pass_reason": got.reason} {
		if line[key] != want {
			t.Fatalf("log %s = %v, want %q", key, line[key], want)
		}
	}

	ended := spans.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want the caller's one span", len(ended))
	}
	attrs := map[string]string{}
	for _, kv := range ended[0].Attributes() {
		attrs[string(kv.Key)] = kv.Value.AsString()
	}
	for key, want := range map[string]string{"repend_cause": "fanout_rerun", "changed": "changed", "change_class": "unknown", "pass_reason": "no_seed"} {
		if attrs[key] != want {
			t.Fatalf("span %s = %q, want %q", key, attrs[key], want)
		}
	}
}

// TestPassOutcomeIsSilentWhenTheWriteFails pins that the outcome counts a
// completed pass only: a failed write records no outcome (the existing
// pass_total still counted the pass at its evidence load). It asserts the
// failure is the writer's own, reached once, so the test cannot pass when
// Handle stops before the writer.
func TestPassOutcomeIsSilentWhenTheWriteFails(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	writer := &failingImpactWriter{}
	handler := &SupplyChainImpactHandler{
		FencingTokenIssuer: newTestImpactFencingTokenIssuer(),
		FactLoader:         &stubSupplyChainImpactFactLoader{},
		Writer:             writer,
		Instruments:        inst,
	}
	intent := fanOutIntent(1, nil)
	_, err = handler.Handle(context.Background(), intent)
	if !errors.Is(err, errImpactWriteFailed) {
		t.Fatalf("Handle() error = %v, want the writer's %v", err, errImpactWriteFailed)
	}
	if writer.calls != 1 {
		t.Fatalf("writer calls = %d, want 1: the failure must come from the writer", writer.calls)
	}
	if series, _ := passOutcomeSeriesOf(t, reader); len(series) != 0 {
		t.Fatalf("series = %v, want none for a failed pass", series)
	}
}

var errImpactWriteFailed = errors.New("write failed")

// failingImpactWriter fails every write with errImpactWriteFailed and counts
// the calls it received.
type failingImpactWriter struct{ calls int }

func (w *failingImpactWriter) WriteSupplyChainImpactFindings(context.Context, SupplyChainImpactWrite) (SupplyChainImpactWriteResult, error) {
	w.calls++
	return SupplyChainImpactWriteResult{}, errImpactWriteFailed
}
