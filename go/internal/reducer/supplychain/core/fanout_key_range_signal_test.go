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

// keyRangeSignalRun is one pass of keyRangeSignals: the hot id, the 1-based
// range index that runs slowFor long (0 for none), the one that fails with a
// timeout (0 for none), and whether the store falls back to one shipped
// statement (its duration slowFor, its error a timeout when failAt is 1).
type keyRangeSignalRun struct {
	hotID          string
	slowAt, failAt int
	slowFor        time.Duration
	fallback       bool
}

// keyRangeSignals runs one pass and returns the fan-out span's key-range
// events, the warn log lines, and the pass's sub-signals.
func keyRangeSignals(t *testing.T, run keyRangeSignalRun) (events, logs []keyRangeSignalEvent, subSignals map[string]float64) {
	t.Helper()
	ranges := HolderKeyRanges()
	port := &rangeStubFanOut{
		stubFanOut: &stubFanOut{
			holders: []string{"cold-holder"},
			masses:  map[string]int64{run.hotID: HolderMassProbeCap},
		},
		rangeHolders:  map[HolderKeyRange][]string{ranges[0]: {"holder-a"}},
		rangeDuration: map[HolderKeyRange]time.Duration{},
		rangeErr:      map[HolderKeyRange]error{},
		fallback:      run.fallback,
	}
	if run.slowAt > 0 {
		port.rangeDuration[ranges[run.slowAt-1]] = run.slowFor
	}
	if run.failAt > 0 {
		port.rangeErr[ranges[run.failAt-1]] = fmt.Errorf("holders_key_range statement %d of %d: %w", run.failAt, len(ranges), ErrFanOutReadTimeout)
	}
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	var buf bytes.Buffer
	handler := fanOutHandler(port, &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{run.hotID, "CVE-2026-c1"}}}, nil)
	handler.Tracer = provider.Tracer("fanout")
	handler.Logger = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	result, _ := handler.Handle(context.Background(), fanOutIntent(1, nil))
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
	return events, logs, result.SubSignals
}

// TestKeyRangeStatementSignalsNameTheRangeAndLookupKind is the arbiter
// 10643l decision 3 contract (#10643 PR 2) with ruling 10643m's fallback: the
// member arm rides inside each key-range statement and cannot be timed by
// arm, so a key-range read statement that failed, ran at or over the 2 s bar,
// or was the shipped-statement fallback for a lookup id over 1,024 bytes adds
// one span event on the fan-out span and one warn log line. Both carry the
// statement's kind, range index and range count, whether the lookup set held
// a non-CVE id (the token arm in play), whether it was the fallback, the
// outcome and the duration. A statement under the bar that succeeds adds
// neither; at exactly 2 s it reports.
func TestKeyRangeStatementSignalsNameTheRangeAndLookupKind(t *testing.T) {
	t.Parallel()

	const ranged, holders = "holders_key_range", "holders"
	for _, tc := range []struct {
		name string
		run  keyRangeSignalRun
		want []keyRangeSignalEvent
	}{
		{
			"alias timeout at range 5",
			keyRangeSignalRun{hotID: "GHSA-hot-alias", failAt: 5},
			[]keyRangeSignalEvent{{ranged, 5, 16, true, false, "read_timeout", 0}},
		},
		{
			"cve slow at range 2",
			keyRangeSignalRun{hotID: "CVE-2026-hot", slowAt: 2, slowFor: 3 * time.Second},
			[]keyRangeSignalEvent{{ranged, 2, 16, false, false, "ok", 3}},
		},
		{
			"cve at exactly the 2 s bar",
			keyRangeSignalRun{hotID: "CVE-2026-hot", slowAt: 16, slowFor: 2 * time.Second},
			[]keyRangeSignalEvent{{ranged, 16, 16, false, false, "ok", 2}},
		},
		{"cve just under the bar", keyRangeSignalRun{hotID: "CVE-2026-hot", slowAt: 7, slowFor: 2*time.Second - time.Millisecond}, nil},
		{
			"alias slow at 3 then timeout at 9",
			keyRangeSignalRun{hotID: "GHSA-hot-alias", slowAt: 3, slowFor: 3 * time.Second, failAt: 9},
			[]keyRangeSignalEvent{{ranged, 3, 16, true, false, "ok", 3}, {ranged, 9, 16, true, false, "read_timeout", 0}},
		},
		{
			"fast fallback still reports",
			keyRangeSignalRun{hotID: "GHSA-long", fallback: true, slowAt: 1, slowFor: time.Millisecond},
			[]keyRangeSignalEvent{{holders, 1, 1, true, true, "ok", 0.001}},
		},
		{
			"fallback timeout",
			keyRangeSignalRun{hotID: "CVE-long", fallback: true, failAt: 1},
			[]keyRangeSignalEvent{{holders, 1, 1, false, true, "read_timeout", 0}},
		},
		{"quiet read", keyRangeSignalRun{hotID: "CVE-2026-hot"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			events, logs, _ := keyRangeSignals(t, tc.run)
			if fmt.Sprint(events) != fmt.Sprint(tc.want) {
				t.Fatalf("span events = %v, want %v", events, tc.want)
			}
			if fmt.Sprint(logs) != fmt.Sprint(tc.want) {
				t.Fatalf("warn logs = %v, want %v", logs, tc.want)
			}
		})
	}
}

// TestKeyRangeFallbackCountsWhatRan proves the pass's statement counts follow
// the read the store ran: a fallback hot id runs one shipped statement, so
// it adds one holder statement and no key-range statement or key-ranged id.
func TestKeyRangeFallbackCountsWhatRan(t *testing.T) {
	t.Parallel()

	_, _, ranged := keyRangeSignals(t, keyRangeSignalRun{hotID: "GHSA-hot-alias"})
	_, _, fallback := keyRangeSignals(t, keyRangeSignalRun{hotID: "GHSA-long", fallback: true})
	n := float64(len(HolderKeyRanges()))
	for _, tc := range []struct {
		name      string
		got       map[string]float64
		statement float64
		keyRange  float64
		keyRanged float64
	}{
		{"key-range read", ranged, 1 + n, n, 1},
		{"fallback read", fallback, 2, 0, 0},
	} {
		if tc.got["fanout_holder_statements"] != tc.statement || tc.got["fanout_holder_key_range_statements"] != tc.keyRange || tc.got["fanout_key_ranged_ids"] != tc.keyRanged {
			t.Fatalf("%s: holder statements %v, key-range statements %v, key-ranged ids %v; want %v, %v, %v", tc.name,
				tc.got["fanout_holder_statements"], tc.got["fanout_holder_key_range_statements"], tc.got["fanout_key_ranged_ids"],
				tc.statement, tc.keyRange, tc.keyRanged)
		}
	}
}
