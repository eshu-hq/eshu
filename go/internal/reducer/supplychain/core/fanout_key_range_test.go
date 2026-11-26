// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// rangeStubFanOut is a stubFanOut that also serves the #10643 key-range
// holder read the way the store does: one statement per range, each carrying
// the foreign-member token arm (PR 2), stopping at the first failing
// statement.
type rangeStubFanOut struct {
	*stubFanOut
	// rangeHolders maps a key range to the holders its statement returns.
	rangeHolders map[HolderKeyRange][]string
	rangeErr     map[HolderKeyRange]error
	// rangeDuration is the duration each range's statement reports.
	rangeDuration map[HolderKeyRange]time.Duration
	rangeCalls    []HolderKeyRange
	rangeIDs      [][]string
	// fallback makes the read one shipped holder statement, as the store
	// runs it for a lookup id over 1,024 bytes (#10643 PR 2, ruling 10643m):
	// range 1 of 1, with the duration and error of the first range.
	fallback bool
}

func (s *rangeStubFanOut) FindHolderScopesByKeyRange(_ context.Context, _ string, lookupIDs []string, ranges []HolderKeyRange) (HolderKeyRangeRead, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var read HolderKeyRangeRead
	if s.fallback {
		read.Fallback = true
		done := HolderReadStatement{Kind: telemetry.FanOutReadKindHolders, Range: 1, Duration: s.rangeDuration[ranges[0]], Err: s.rangeErr[ranges[0]]}
		read.Statements = append(read.Statements, done)
		if done.Err != nil {
			return read, done.Err
		}
		read.Scopes = append(read.Scopes, s.rangeHolders[ranges[0]]...)
		return read, nil
	}
	set := map[string]struct{}{}
	for i, keyRange := range ranges {
		s.rangeCalls = append(s.rangeCalls, keyRange)
		s.rangeIDs = append(s.rangeIDs, append([]string(nil), lookupIDs...))
		if err := s.rangeErr[keyRange]; err != nil {
			read.Statements = append(read.Statements, HolderReadStatement{Kind: telemetry.FanOutReadKindHoldersKeyRange, Range: i + 1, Duration: s.rangeDuration[keyRange], Err: err})
			return read, err
		}
		read.Statements = append(read.Statements, HolderReadStatement{Kind: telemetry.FanOutReadKindHoldersKeyRange, Range: i + 1, Duration: s.rangeDuration[keyRange]})
		for _, id := range s.rangeHolders[keyRange] {
			set[id] = struct{}{}
		}
	}
	for id := range set {
		read.Scopes = append(read.Scopes, id)
	}
	sort.Strings(read.Scopes)
	return read, nil
}

// TestHolderKeyRangesPartitionTheKeySpace proves the ranges cover every
// fact_id exactly once: the first is unbounded below, the last unbounded
// above, each upper bound is the next lower bound, and the bounds increase.
func TestHolderKeyRangesPartitionTheKeySpace(t *testing.T) {
	t.Parallel()

	ranges := HolderKeyRanges()
	if len(ranges) != fanOutHolderKeyRangeCount {
		t.Fatalf("ranges = %d, want %d", len(ranges), fanOutHolderKeyRangeCount)
	}
	if ranges[0].Lower != "" || ranges[len(ranges)-1].Upper != "" {
		t.Fatalf("outer bounds = %q..%q, want unbounded on both ends", ranges[0].Lower, ranges[len(ranges)-1].Upper)
	}
	for i := 0; i+1 < len(ranges); i++ {
		if ranges[i].Upper != ranges[i+1].Lower {
			t.Fatalf("range %d upper %q != range %d lower %q: a gap or overlap", i, ranges[i].Upper, i+1, ranges[i+1].Lower)
		}
		if i > 0 && ranges[i].Upper <= ranges[i].Lower {
			t.Fatalf("range %d is empty or inverted: %+v", i, ranges[i])
		}
	}
	for _, id := range []string{
		"", "a", "reducer_supply_chain_impact_finding:0", "reducer_supply_chain_impact_finding:7f",
		"reducer_supply_chain_impact_finding:f", "reducer_supply_chain_impact_finding:ffff", "zzz",
	} {
		hits := 0
		for _, r := range ranges {
			if id >= r.Lower && (r.Upper == "" || id < r.Upper) {
				hits++
			}
		}
		if hits != 1 {
			t.Fatalf("fact id %q falls in %d ranges, want exactly 1", id, hits)
		}
	}
}

// TestHolderKeyRangeCountDividesTheDigits pins the range count HolderKeyRanges
// can build: a count outside 1..16 or not dividing 16 leaves a zero step (an
// endless loop) or an uneven last range.
func TestHolderKeyRangeCountDividesTheDigits(t *testing.T) {
	t.Parallel()

	digits := len(holderKeyRangeHexDigits)
	if fanOutHolderKeyRangeCount < 1 || fanOutHolderKeyRangeCount > digits || digits%fanOutHolderKeyRangeCount != 0 {
		t.Fatalf("fanOutHolderKeyRangeCount = %d, want a divisor of %d in 1..%d", fanOutHolderKeyRangeCount, digits, digits)
	}
}

// TestHotSingletonHolderReadRunsAsKeyRanges was the #10643 routing pin: a
// hot singleton ran one statement per fact_id key range. #11430 reroutes the
// hot singleton to the already-shipped single holder statement, so this pin
// is superseded by TestHotSingletonHolderReadRunsAsSingleStatement
// (fanout_hot_single_statement_test.go), which proves the hot singleton, the
// cold pack, their union, and the holder statement counts.

// TestHotSingletonWithoutRangeReaderKeepsSingleton proves a port without the
// key-range seam keeps the shipped singleton statement, so a decorator or
// fake that does not forward it degrades to the old shape, not to a wrong set.
func TestHotSingletonWithoutRangeReaderKeepsSingleton(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{
		holders: []string{"holder-a"},
		masses:  map[string]int64{"CVE-2026-hot": HolderMassProbeCap},
	}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-hot"}}}
	if _, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !reflect.DeepEqual(port.findCalls, [][]string{{"CVE-2026-hot"}}) {
		t.Fatalf("statements = %v, want the shipped singleton", port.findCalls)
	}
}

// TestHotSingletonSingleStatementFailureFailsClosed proves a timed-out hot
// singleton single statement (#11430) fails the pass with the retryable
// fan-out class, counts as a holders-site read timeout, and reopens nothing.
func TestHotSingletonSingleStatementFailureFailsClosed(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	port := &stubFanOut{
		holders: []string{"holder-a"},
		masses:  map[string]int64{"CVE-2026-hot": HolderMassProbeCap},
		findErr: ErrFanOutReadTimeout,
	}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-hot"}}}
	_, err = fanOutHandler(port, writer, inst).Handle(context.Background(), fanOutIntent(1, nil))
	if !errors.Is(err, ErrFanOutReadTimeout) {
		t.Fatalf("Handle() error = %v, want ErrFanOutReadTimeout", err)
	}
	var classified interface{ FailureClass() string }
	if !errors.As(err, &classified) || classified.FailureClass() != SupplyChainImpactFanOutFailureClass {
		t.Fatalf("error %v is not the counting fan-out class", err)
	}
	if len(port.reopened) != 0 {
		t.Fatalf("reopened = %v after a failed single-statement read, want none", port.reopened)
	}
	if got := fanOutCount(t, reader)["read_timeout"]; got != 1 {
		t.Fatalf("read_timeout count = %d, want 1", got)
	}
}

// TestSingleStatementReadRecordsDurationByKind proves the read-duration
// histogram on the below-gate path: one sample per shipped holder statement
// (the hot singleton reads as one holders statement, not key-range
// statements), by kind and outcome, so an operator reads per-statement p95
// and the timeout share per read kind. The DeepEqual also pins that no
// holders_key_range sample is recorded. The mass_probe count is 2: the
// per-chunk probe plus the one batched exact count the gate needs for the
// probe-hot singleton (its true mass resolves to zero here, under the gate).
func TestSingleStatementReadRecordsDurationByKind(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	port := &stubFanOut{
		holders: []string{"cold-holder"},
		masses:  map[string]int64{"CVE-2026-hot": HolderMassProbeCap},
	}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-hot", "CVE-2026-c1"}}}
	if _, err := fanOutHandler(port, writer, inst).Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	got := fanOutReadDurationCounts(t, reader)
	want := map[string]uint64{
		telemetry.FanOutReadKindMassProbe + "/ok": 2,
		telemetry.FanOutReadKindHolders + "/ok":   2,
		// The #10972 queued-holder prefilter runs once per claim with holders.
		telemetry.FanOutReadKindHolderPrefilter + "/ok": 1,
		// The genuine pass's load-end in-flight claim read (#10972 D4b).
		telemetry.FanOutReadKindInFlightEarly + "/ok": 1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("read-duration samples = %v, want %v", got, want)
	}
}

// fanOutReadDurationCounts returns the read-duration histogram sample count
// keyed by kind/outcome.
func fanOutReadDurationCounts(t *testing.T, reader *sdkmetric.ManualReader) map[string]uint64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	out := map[string]uint64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_fanout_read_duration_seconds" {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("read-duration data is %T, want a float64 histogram", m.Data)
			}
			for _, dp := range hist.DataPoints {
				kind, _ := dp.Attributes.Value(telemetry.MetricDimensionKind)
				outcome, _ := dp.Attributes.Value("outcome")
				out[kind.AsString()+"/"+outcome.AsString()] += dp.Count
			}
		}
	}
	return out
}
