// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestHotSingletonAboveRoutingMassGateReadsAsKeyRanges is the #10972 H2 A2
// routing pin: a probe-hot singleton whose routing mass sits above
// HolderKeyRangeMassGate reads as one statement per fact_id key range
// (FindHolderScopesByKeyRange under its pinned custom plan), not as the
// shipped single holder statement. The mass probe saturates at
// HolderMassProbeCap, so the gate needs the routing-mass count: a 157k-row
// advisory id (A1 warm ~1.3s isolated but p95 3.45s at 4-parallel, over the
// 2s bar) must take the key-range read A2 proved at 323ms p95 concurrent.
// It fails until the re-route lands.
func TestHotSingletonAboveRoutingMassGateReadsAsKeyRanges(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	ranges := HolderKeyRanges()
	rangeHolders := make(map[HolderKeyRange][]string, len(ranges))
	for _, r := range ranges {
		rangeHolders[r] = []string{"holder-a"}
	}
	port := &rangeStubFanOut{
		stubFanOut: &stubFanOut{
			holders:       []string{"holder-a"},
			masses:        map[string]int64{"GHSA-7r86-cg39-jmmj": HolderMassProbeCap},
			routingMasses: map[string]int64{"GHSA-7r86-cg39-jmmj": HolderKeyRangeMassGate + 1},
		},
		rangeHolders: rangeHolders,
	}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"GHSA-7r86-cg39-jmmj"}}}
	result, err := fanOutHandler(port, writer, inst).Handle(context.Background(), fanOutIntent(1, nil))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !reflect.DeepEqual(port.rangeCalls, ranges) {
		t.Fatalf("ranged statements = %d, want all %d key ranges in order", len(port.rangeCalls), len(ranges))
	}
	if len(port.findCalls) != 0 {
		t.Fatalf("shipped statements = %v, want none: the above-gate singleton reads as key ranges", port.findCalls)
	}
	if !reflect.DeepEqual(port.reopened, []string{"holder-a"}) {
		t.Fatalf("reopened = %v, want the key-range union [holder-a]", port.reopened)
	}
	if got := result.SubSignals["fanout_key_ranged_ids"]; got != 1 {
		t.Fatalf("fanout_key_ranged_ids = %v, want 1", got)
	}
	if got := result.SubSignals["fanout_holder_key_range_statements"]; got != float64(len(ranges)) {
		t.Fatalf("fanout_holder_key_range_statements = %v, want %d", got, len(ranges))
	}
	if got := result.SubSignals["fanout_holder_statements"]; got != float64(len(ranges)) {
		t.Fatalf("fanout_holder_statements = %v, want %d", got, len(ranges))
	}
	if got := result.SubSignals["fanout_max_id_mass_rows"]; got != float64(HolderKeyRangeMassGate+1) {
		t.Fatalf("fanout_max_id_mass_rows = %v, want the resolved routing mass %d", got, HolderKeyRangeMassGate+1)
	}
	// Telemetry confirmation: one mass-planning sample per probe and exact
	// count, one whole-read sample, and one per-statement sample under the
	// holders_key_range kind.
	got := fanOutReadDurationCounts(t, reader)
	want := map[string]uint64{
		telemetry.FanOutReadKindMassProbe + "/ok":           2,
		telemetry.FanOutReadKindHoldersKeyRangeRead + "/ok": 1,
		telemetry.FanOutReadKindHoldersKeyRange + "/ok":     uint64(len(ranges)),
		telemetry.FanOutReadKindHolderPrefilter + "/ok":     1,
		telemetry.FanOutReadKindInFlightEarly + "/ok":       1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("read-duration samples = %v, want %v", got, want)
	}
}

// TestHotSingletonRoutingMassGateBoundary pins both sides of the A2 gate: a
// probe-hot singleton reads the shipped single statement at or under the
// gate and the key-range read above it. Every routing mass here is a value
// production can return: unsaturated summed per-arm counts at or under
// the gate, saturating at gate+1 above it (the member-heavy and
// hottest-advisory classes saturate to the same report, so they share the
// row). The gate is the largest A1-passing shim singleton mass halved:
// 156870/2 = 78435.
func TestHotSingletonRoutingMassGateBoundary(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		routingMass int64
		wantRanged  bool
	}{
		{"mid-band saturation stays single", 5000, false},
		{"A1-passing direct-only stays single", 42388, false},
		{"at the gate stays single", HolderKeyRangeMassGate, false},
		{"one row over the gate ranges", HolderKeyRangeMassGate + 1, true},
		{"member-heavy mix saturates to gate+1 and ranges", HolderKeyRangeMassGate + 1, true},
		{"hottest advisory saturates to gate+1 and ranges", HolderKeyRangeMassGate + 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ranges := HolderKeyRanges()
			rangeHolders := make(map[HolderKeyRange][]string, len(ranges))
			for _, r := range ranges {
				rangeHolders[r] = []string{"holder-a"}
			}
			port := &rangeStubFanOut{
				stubFanOut: &stubFanOut{
					holders:       []string{"holder-a"},
					masses:        map[string]int64{"CVE-2026-hot": HolderMassProbeCap},
					routingMasses: map[string]int64{"CVE-2026-hot": tc.routingMass},
				},
				rangeHolders: rangeHolders,
			}
			writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-hot"}}}
			result, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, nil))
			if err != nil {
				t.Fatalf("Handle() error = %v", err)
			}
			if tc.wantRanged {
				if len(port.rangeCalls) != len(ranges) {
					t.Fatalf("ranged statements = %d, want %d: routing mass %d is above the gate", len(port.rangeCalls), len(ranges), tc.routingMass)
				}
				if len(port.findCalls) != 0 {
					t.Fatalf("shipped statements = %v, want none", port.findCalls)
				}
				if got := result.SubSignals["fanout_key_ranged_ids"]; got != 1 {
					t.Fatalf("fanout_key_ranged_ids = %v, want 1", got)
				}
				return
			}
			if len(port.rangeCalls) != 0 {
				t.Fatalf("ranged statements = %v, want none: routing mass %d is at or under the gate", port.rangeCalls, tc.routingMass)
			}
			if !reflect.DeepEqual(port.findCalls, [][]string{{"CVE-2026-hot"}}) {
				t.Fatalf("shipped statements = %v, want the singleton alone", port.findCalls)
			}
			if got := result.SubSignals["fanout_key_ranged_ids"]; got != 0 {
				t.Fatalf("fanout_key_ranged_ids = %v, want 0", got)
			}
		})
	}
}

// TestMultiHotChunkCountsRoutingMassOnce is the #10972 H2 Lane-1b one-batch
// pin: a chunk holding two probe-hot singletons resolves both with a single
// batched routing-mass count carrying exactly the hot set (per-id splitting
// is a forbidden fallback). Both hot ids read as key ranges while the cold
// ids share one shipped statement, so the statement totals are 32 key-range
// plus 1 shipped. It fails if the router ever splits the count per id or
// ranges an id the count did not resolve.
func TestMultiHotChunkCountsRoutingMassOnce(t *testing.T) {
	t.Parallel()

	ranges := HolderKeyRanges()
	rangeHolders := make(map[HolderKeyRange][]string, len(ranges))
	for _, r := range ranges {
		rangeHolders[r] = []string{"holder-a"}
	}
	hotA, hotB := "CVE-2026-hot-a", "CVE-2026-hot-b"
	cold := []string{"CVE-2026-c1", "CVE-2026-c2"}
	port := &rangeStubFanOut{
		stubFanOut: &stubFanOut{
			holders: []string{"holder-a"},
			masses: map[string]int64{
				hotA:    HolderMassProbeCap,
				hotB:    HolderMassProbeCap,
				cold[0]: 10,
				cold[1]: 20,
			},
			routingMasses: map[string]int64{
				hotA: HolderKeyRangeMassGate + 1,
				hotB: HolderKeyRangeMassGate + 1,
			},
		},
		rangeHolders: rangeHolders,
	}
	ids := append([]string{hotA, hotB}, cold...)
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: ids}}
	result, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, nil))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !reflect.DeepEqual(port.routingMassCalls, [][]string{{hotA, hotB}}) {
		t.Fatalf("routing-mass calls = %v, want exactly one batched call with the hot set [%s %s]", port.routingMassCalls, hotA, hotB)
	}
	if len(port.rangeCalls) != 2*len(ranges) {
		t.Fatalf("ranged statements = %d, want %d (both hot ids ranged)", len(port.rangeCalls), 2*len(ranges))
	}
	if !reflect.DeepEqual(port.findCalls, [][]string{cold}) {
		t.Fatalf("shipped statements = %v, want the cold ids sharing one statement", port.findCalls)
	}
	if got := result.SubSignals["fanout_key_ranged_ids"]; got != 2 {
		t.Fatalf("fanout_key_ranged_ids = %v, want 2", got)
	}
	if got := result.SubSignals["fanout_holder_key_range_statements"]; got != float64(2*len(ranges)) {
		t.Fatalf("fanout_holder_key_range_statements = %v, want %d", got, 2*len(ranges))
	}
	if got := result.SubSignals["fanout_holder_statements"]; got != float64(2*len(ranges)+1) {
		t.Fatalf("fanout_holder_statements = %v, want %d", got, 2*len(ranges)+1)
	}
	if got := result.SubSignals["fanout_max_id_mass_rows"]; got != float64(HolderKeyRangeMassGate+1) {
		t.Fatalf("fanout_max_id_mass_rows = %v, want the resolved routing mass %d", got, HolderKeyRangeMassGate+1)
	}
}

// TestRoutingMassCountFailureFailsClosed proves the routing-mass count fails
// the way the probe does: a timed-out routing-mass count fails the pass with the
// retryable fan-out class, counts one holders-site read timeout, and reopens
// nothing.
func TestRoutingMassCountFailureFailsClosed(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	port := &rangeStubFanOut{
		stubFanOut: &stubFanOut{
			holders:        []string{"holder-a"},
			masses:         map[string]int64{"CVE-2026-hot": HolderMassProbeCap},
			routingMassErr: ErrFanOutReadTimeout,
		},
	}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-hot"}}}
	_, err = fanOutHandler(port, writer, inst).Handle(context.Background(), fanOutIntent(1, nil))
	if err == nil {
		t.Fatalf("Handle() error = nil, want the timed-out routing-mass count to fail the pass")
	}
	if !errors.Is(err, ErrFanOutReadTimeout) {
		t.Fatalf("Handle() error = %v, want ErrFanOutReadTimeout", err)
	}
	if len(port.reopened) != 0 {
		t.Fatalf("reopened = %v after a failed routing-mass count, want none", port.reopened)
	}
	if len(port.rangeCalls) != 0 || len(port.findCalls) != 0 {
		t.Fatalf("statements ran after the failed count: ranges %v shipped %v, want none", port.rangeCalls, port.findCalls)
	}
	if got := fanOutCount(t, reader)["read_timeout"]; got != 1 {
		t.Fatalf("read_timeout count = %d, want 1", got)
	}
}

// TestSlowKeyRangeStatementReportsRangeEndToEnd proves the per-statement
// confirmation hooks fire through the routed read: a ranged statement at or
// over the 2s bar adds one holder_key_range_statement span event and one
// warn log line naming its kind, range, and duration.
func TestSlowKeyRangeStatementReportsRangeEndToEnd(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	var buf bytes.Buffer
	ranges := HolderKeyRanges()
	rangeHolders := make(map[HolderKeyRange][]string, len(ranges))
	rangeDuration := make(map[HolderKeyRange]time.Duration, len(ranges))
	for i, r := range ranges {
		rangeHolders[r] = []string{"holder-a"}
		if i == 4 {
			rangeDuration[r] = 3 * time.Second
		}
	}
	port := &rangeStubFanOut{
		stubFanOut: &stubFanOut{
			holders:       []string{"holder-a"},
			masses:        map[string]int64{"CVE-2026-hot": HolderMassProbeCap},
			routingMasses: map[string]int64{"CVE-2026-hot": HolderKeyRangeMassGate + 1},
		},
		rangeHolders:  rangeHolders,
		rangeDuration: rangeDuration,
	}
	handler := fanOutHandler(port, &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-hot"}}}, nil)
	handler.Tracer = provider.Tracer("fanout")
	handler.Logger = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if _, err := handler.Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	events := keyRangeSpanEvents(recorder)
	if len(events) != 1 || events[0].Kind != telemetry.FanOutReadKindHoldersKeyRange || events[0].Range != 5 {
		t.Fatalf("span events = %v, want one holders_key_range event at range 5", events)
	}
	if logs := keyRangeWarnLogs(t, &buf); len(logs) != 1 || logs[0].Range != 5 {
		t.Fatalf("warn logs = %v, want one key-range line at range 5", logs)
	}
}

// TestAboveGateLongIDFallbackCountsOneStatement pins the long-id fallback
// accounting: a probe-hot lookup id over 1,024 bytes whose routing mass
// sits above the gate ranges, and the key-range read falls back to the
// single shipped holder statement (no member token covers it), so the pass
// counts exactly one holder statement and one key-range statement. Both
// counts come from the read's Statements at the execution site, never
// pre-added per range.
func TestAboveGateLongIDFallbackCountsOneStatement(t *testing.T) {
	t.Parallel()

	longID := strings.Repeat("G", 2000)
	ranges := HolderKeyRanges()
	port := &rangeStubFanOut{
		stubFanOut: &stubFanOut{
			holders:       []string{"holder-a"},
			masses:        map[string]int64{longID: HolderMassProbeCap},
			routingMasses: map[string]int64{longID: HolderKeyRangeMassGate + 1},
		},
		rangeHolders: map[HolderKeyRange][]string{ranges[0]: {"holder-a"}},
		fallback:     true,
	}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{longID}}}
	result, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, nil))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(port.findCalls) != 0 {
		t.Fatalf("shipped statements = %v, want none: the fallback runs inside the key-range read", port.findCalls)
	}
	if !reflect.DeepEqual(port.reopened, []string{"holder-a"}) {
		t.Fatalf("reopened = %v, want the fallback read's union [holder-a]", port.reopened)
	}
	if got := result.SubSignals["fanout_key_ranged_ids"]; got != 1 {
		t.Fatalf("fanout_key_ranged_ids = %v, want 1", got)
	}
	if got := result.SubSignals["fanout_holder_key_range_statements"]; got != 1 {
		t.Fatalf("fanout_holder_key_range_statements = %v, want 1 (the fallback statement), not 16 pre-added ranges", got)
	}
	if got := result.SubSignals["fanout_holder_statements"]; got != 1 {
		t.Fatalf("fanout_holder_statements = %v, want 1 (the fallback statement)", got)
	}
}
