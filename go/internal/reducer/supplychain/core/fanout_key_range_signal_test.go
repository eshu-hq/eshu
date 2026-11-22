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
// line, reduced to the fields an operator reads.
type keyRangeSignalEvent struct {
	Range   int64
	NonCVE  bool
	Outcome string
}

// keyRangeSignals runs one pass whose hot id is hotID, with range index
// slowAt (1-based) running slow and failAt failing with a timeout (0 for
// none), and returns the fan-out span's key-range events and the warn log
// lines.
func keyRangeSignals(t *testing.T, hotID string, slowAt, failAt int) (events, logs []keyRangeSignalEvent) {
	t.Helper()
	ranges := HolderKeyRanges()
	port := &rangeStubFanOut{
		stubFanOut: &stubFanOut{
			holders: []string{"cold-holder"},
			masses:  map[string]int64{hotID: HolderMassProbeCap},
		},
		rangeHolders:  map[HolderKeyRange][]string{ranges[0]: {"holder-a"}},
		rangeDuration: map[HolderKeyRange]time.Duration{},
		rangeErr:      map[HolderKeyRange]error{},
	}
	if slowAt > 0 {
		port.rangeDuration[ranges[slowAt-1]] = 3 * time.Second
	}
	if failAt > 0 {
		port.rangeErr[ranges[failAt-1]] = fmt.Errorf("holders_key_range statement %d of %d: %w", failAt, len(ranges), ErrFanOutReadTimeout)
	}
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	var buf bytes.Buffer
	handler := fanOutHandler(port, &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{hotID, "CVE-2026-c1"}}}, nil)
	handler.Tracer = provider.Tracer("fanout")
	handler.Logger = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	_, _ = handler.Handle(context.Background(), fanOutIntent(1, nil))
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
				case "range_index":
					got.Range = kv.Value.AsInt64()
				case "non_cve_lookup":
					got.NonCVE = kv.Value.AsBool()
				case "outcome":
					got.Outcome = kv.Value.AsString()
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
		index, _ := rec["range_index"].(float64)
		nonCVE, _ := rec["non_cve_lookup"].(bool)
		outcome, _ := rec["outcome"].(string)
		logs = append(logs, keyRangeSignalEvent{Range: int64(index), NonCVE: nonCVE, Outcome: outcome})
	}
	return events, logs
}

// TestKeyRangeStatementSignalsNameTheRangeAndLookupKind is the arbiter
// 10643l decision 3 contract (#10643 PR 2): the member arm rides inside each
// key-range statement and cannot be timed by arm, so a slow (at or over the
// 2 s bar) or failed key-range statement adds one span event on the fan-out
// span and one warn log line, each naming its range index (1 to 16) and
// whether the lookup set held a non-CVE id (the token arm in play). A
// statement under the bar that succeeds adds neither.
func TestKeyRangeStatementSignalsNameTheRangeAndLookupKind(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name           string
		hotID          string
		slowAt, failAt int
		want           []keyRangeSignalEvent
	}{
		{"alias timeout at range 5", "GHSA-hot-alias", 0, 5, []keyRangeSignalEvent{{5, true, "read_timeout"}}},
		{"cve slow at range 2", "CVE-2026-hot", 2, 0, []keyRangeSignalEvent{{2, false, "ok"}}},
		{"alias slow at 3 then timeout at 9", "GHSA-hot-alias", 3, 9, []keyRangeSignalEvent{{3, true, "ok"}, {9, true, "read_timeout"}}},
		{"quiet read", "CVE-2026-hot", 0, 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			events, logs := keyRangeSignals(t, tc.hotID, tc.slowAt, tc.failAt)
			if fmt.Sprint(events) != fmt.Sprint(tc.want) {
				t.Fatalf("span events = %v, want %v", events, tc.want)
			}
			if fmt.Sprint(logs) != fmt.Sprint(tc.want) {
				t.Fatalf("warn logs = %v, want %v", logs, tc.want)
			}
		})
	}
}
