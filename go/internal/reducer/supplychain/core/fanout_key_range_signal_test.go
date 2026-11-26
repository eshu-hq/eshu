// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// keyRangeSignalEvent is one holder_key_range_statement span event or log
// line with every closed field an operator reads.
type keyRangeSignalEvent struct {
	Kind       string
	Range      int64
	RangeCount int64
	NonCVE     bool
	Fallback   bool
	Outcome    string
	Seconds    float64
}

// keyRangeSignalCall is one noteKeyRangeStatement call: the statement record,
// the lookup ids it ran for, and whether the read fell back to the shipped
// statement.
type keyRangeSignalCall struct {
	kind     string
	index    int
	count    int
	ids      []string
	duration time.Duration
	err      error
	fallback bool
}

// noteRangeStatementSignals runs each call against one fan-out span and
// returns the span's key-range events and the warn log lines. The arbiter
// 10643l decision 3 contract (a failed, slow, or fallback statement reports;
// a fast success stays quiet) is pinned here directly against the helper,
// alongside the end-to-end routed pin in
// fanout_hot_key_range_reroute_test.go.
func noteRangeStatementSignals(t *testing.T, calls []keyRangeSignalCall) (events, logs []keyRangeSignalEvent) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	var buf bytes.Buffer
	handler := fanOutHandler(&stubFanOut{}, &changeWriter{}, nil)
	handler.Tracer = provider.Tracer("fanout")
	handler.Logger = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, span := handler.Tracer.Start(context.Background(), "supply_chain_impact.fanout")
	for _, call := range calls {
		done := HolderReadStatement{Kind: call.kind, Range: call.index, Duration: call.duration, Err: call.err}
		handler.noteKeyRangeStatement(ctx, done, call.count, lookupHoldsNonCVEID(call.ids), call.fallback)
	}
	span.End()
	return keyRangeSpanEvents(recorder), keyRangeWarnLogs(t, &buf)
}

// keyRangeSpanEvents returns the fan-out span's key-range events.
func keyRangeSpanEvents(recorder *tracetest.SpanRecorder) (events []keyRangeSignalEvent) {
	for _, span := range recorder.Ended() {
		if span.Name() != "supply_chain_impact.fanout" {
			continue
		}
		for _, event := range span.Events() {
			if event.Name != "holder_key_range_statement" {
				continue
			}
			var got keyRangeSignalEvent
			for _, kv := range event.Attributes {
				switch kv.Key {
				case "kind":
					got.Kind = kv.Value.AsString()
				case "range_index":
					got.Range = kv.Value.AsInt64()
				case "range_count":
					got.RangeCount = kv.Value.AsInt64()
				case "non_cve_lookup":
					got.NonCVE = kv.Value.AsBool()
				case "fallback":
					got.Fallback = kv.Value.AsBool()
				case "outcome":
					got.Outcome = kv.Value.AsString()
				case "duration_seconds":
					got.Seconds = kv.Value.AsFloat64()
				}
			}
			events = append(events, got)
		}
	}
	return events
}

// keyRangeWarnLogs returns the key-range warn log lines.
func keyRangeWarnLogs(t *testing.T, buf *bytes.Buffer) (logs []keyRangeSignalEvent) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var rec map[string]any
		if line == "" || json.Unmarshal([]byte(line), &rec) != nil || rec["msg"] != keyRangeStatementLogMessage {
			continue
		}
		if rec["level"] != "WARN" {
			t.Fatalf("key-range statement log level = %v, want WARN: %s", rec["level"], line)
		}
		var got keyRangeSignalEvent
		got.Kind, _ = rec["kind"].(string)
		index, _ := rec["range_index"].(float64)
		count, _ := rec["range_count"].(float64)
		got.Range, got.RangeCount = int64(index), int64(count)
		got.NonCVE, _ = rec["non_cve_lookup"].(bool)
		got.Fallback, _ = rec["fallback"].(bool)
		got.Outcome, _ = rec["outcome"].(string)
		got.Seconds, _ = rec["duration_seconds"].(float64)
		logs = append(logs, got)
	}
	return logs
}

// TestKeyRangeStatementSignalsNameTheRangeAndLookupKind is the arbiter
// 10643l decision 3 contract (#10643 PR 2) with ruling 10643m's fallback: the
// member arm rides inside each key-range statement and cannot be timed by
// arm, so a key-range read statement that failed, ran at or over the 2 s
// bar, or was the shipped-statement fallback for a lookup id over 1,024
// bytes adds one span event on the fan-out span and one warn log line. Both
// carry the statement's kind, range index and range count, whether the
// lookup set held a non-CVE id (the token arm in play), whether it was the
// fallback, the outcome and the duration. A statement under the bar that
// succeeds adds neither; at exactly 2 s it reports. Routing sends no
// key-range statements since #11430, so the cases drive the helper directly.
func TestKeyRangeStatementSignalsNameTheRangeAndLookupKind(t *testing.T) {
	t.Parallel()

	const ranged, holders = "holders_key_range", "holders"
	for _, tc := range []struct {
		name  string
		calls []keyRangeSignalCall
		want  []keyRangeSignalEvent
	}{
		{
			"alias timeout at range 5",
			[]keyRangeSignalCall{{kind: ranged, index: 5, count: 16, ids: []string{"GHSA-hot-alias"}, err: ErrFanOutReadTimeout}},
			[]keyRangeSignalEvent{{ranged, 5, 16, true, false, "read_timeout", 0}},
		},
		{
			"cve slow at range 2",
			[]keyRangeSignalCall{{kind: ranged, index: 2, count: 16, ids: []string{"CVE-2026-hot"}, duration: 3 * time.Second}},
			[]keyRangeSignalEvent{{ranged, 2, 16, false, false, "ok", 3}},
		},
		{
			"cve at exactly the 2 s bar",
			[]keyRangeSignalCall{{kind: ranged, index: 16, count: 16, ids: []string{"CVE-2026-hot"}, duration: 2 * time.Second}},
			[]keyRangeSignalEvent{{ranged, 16, 16, false, false, "ok", 2}},
		},
		{
			"cve just under the bar",
			[]keyRangeSignalCall{{kind: ranged, index: 7, count: 16, ids: []string{"CVE-2026-hot"}, duration: 2*time.Second - time.Millisecond}},
			nil,
		},
		{
			"alias slow at 3 then timeout at 9",
			[]keyRangeSignalCall{
				{kind: ranged, index: 3, count: 16, ids: []string{"GHSA-hot-alias"}, duration: 3 * time.Second},
				{kind: ranged, index: 9, count: 16, ids: []string{"GHSA-hot-alias"}, err: ErrFanOutReadTimeout},
			},
			[]keyRangeSignalEvent{{ranged, 3, 16, true, false, "ok", 3}, {ranged, 9, 16, true, false, "read_timeout", 0}},
		},
		{
			"fast fallback still reports",
			[]keyRangeSignalCall{{kind: holders, index: 1, count: 1, ids: []string{"GHSA-long"}, duration: time.Millisecond, fallback: true}},
			[]keyRangeSignalEvent{{holders, 1, 1, true, true, "ok", 0.001}},
		},
		{
			"fallback timeout",
			[]keyRangeSignalCall{{kind: holders, index: 1, count: 1, ids: []string{"CVE-long"}, err: ErrFanOutReadTimeout, fallback: true}},
			[]keyRangeSignalEvent{{holders, 1, 1, false, true, "read_timeout", 0}},
		},
		{"quiet read", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			events, logs := noteRangeStatementSignals(t, tc.calls)
			if fmt.Sprint(events) != fmt.Sprint(tc.want) {
				t.Fatalf("span events = %v, want %v", events, tc.want)
			}
			if fmt.Sprint(logs) != fmt.Sprint(tc.want) {
				t.Fatalf("warn logs = %v, want %v", logs, tc.want)
			}
		})
	}
}

// TestHotSingletonEmitsNoKeyRangeSignal proves the below-gate routing: a hot
// singleton pass whose routing mass is unknown (zero) emits no
// holder_key_range_statement span event and no key-range warn log, because it
// runs the shipped single statement. The long lookup id is included: the
// token function never stores one over 1,024 bytes, and the shipped statement
// answers it directly, so the fallback is preserved by resting on the shipped
// shape.
func TestHotSingletonEmitsNoKeyRangeSignal(t *testing.T) {
	t.Parallel()

	for _, hotID := range []string{"CVE-2026-hot", strings.Repeat("G", 2000)} {
		recorder := tracetest.NewSpanRecorder()
		provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
		var buf bytes.Buffer
		port := &rangeStubFanOut{
			stubFanOut: &stubFanOut{
				holders: []string{"cold-holder"},
				masses:  map[string]int64{hotID: HolderMassProbeCap},
			},
		}
		handler := fanOutHandler(port, &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{hotID, "CVE-2026-c1"}}}, nil)
		handler.Tracer = provider.Tracer("fanout")
		handler.Logger = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
		if _, err := handler.Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
			t.Fatalf("hot id %q: Handle() error = %v", hotID, err)
		}
		if events := keyRangeSpanEvents(recorder); len(events) != 0 {
			t.Fatalf("hot id %q: span events = %v, want none", hotID, events)
		}
		if logs := keyRangeWarnLogs(t, &buf); len(logs) != 0 {
			t.Fatalf("hot id %q: warn logs = %v, want none", hotID, logs)
		}
		if len(port.rangeCalls) != 0 {
			t.Fatalf("hot id %q: ranged statements = %v, want none", hotID, port.rangeCalls)
		}
	}
}

// TestHotSingletonCountsHolderStatements proves the pass's statement counts
// follow the below-gate read: a hot singleton with unknown (zero) routing mass —
// short or long lookup id — adds holder statements only, no key-range
// statement and no key-ranged id, so the accounting keys keep their shape
// with zero key-range counts.
func TestHotSingletonCountsHolderStatements(t *testing.T) {
	t.Parallel()

	for _, hotID := range []string{"CVE-2026-hot", strings.Repeat("G", 2000)} {
		port := &stubFanOut{
			holders: []string{"holder-a"},
			masses:  map[string]int64{hotID: HolderMassProbeCap},
		}
		result, err := fanOutHandler(port, &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{hotID, "CVE-2026-c1"}}}, nil).Handle(context.Background(), fanOutIntent(1, nil))
		if err != nil {
			t.Fatalf("hot id %q: Handle() error = %v", hotID, err)
		}
		if got := result.SubSignals["fanout_holder_statements"]; got != 2 {
			t.Fatalf("hot id %q: holder statements %v, want 2 (hot singleton plus cold id)", hotID, got)
		}
		if got := result.SubSignals["fanout_holder_key_range_statements"]; got != 0 {
			t.Fatalf("hot id %q: key-range statements %v, want 0", hotID, got)
		}
		if got := result.SubSignals["fanout_key_ranged_ids"]; got != 0 {
			t.Fatalf("hot id %q: key-ranged ids %v, want 0", hotID, got)
		}
	}
}
