// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// rangeStubFanOut is a stubFanOut that also serves the #10643 key-range
// holder read, recording each ranged statement and each member-arm call.
type rangeStubFanOut struct {
	*stubFanOut
	// rangeHolders maps a key range to the holders its statement returns.
	rangeHolders map[HolderKeyRange][]string
	// memberHolders is what the member-arm statement returns.
	memberHolders []string
	rangeErr      map[HolderKeyRange]error
	rangeCalls    []HolderKeyRange
	rangeIDs      [][]string
	memberCalls   [][]string
}

func (s *rangeStubFanOut) FindHolderScopesInKeyRange(_ context.Context, _ string, lookupIDs []string, keyRange HolderKeyRange) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rangeCalls = append(s.rangeCalls, keyRange)
	s.rangeIDs = append(s.rangeIDs, append([]string(nil), lookupIDs...))
	if err := s.rangeErr[keyRange]; err != nil {
		return nil, err
	}
	return append([]string(nil), s.rangeHolders[keyRange]...), nil
}

func (s *rangeStubFanOut) FindMemberHolderScopes(_ context.Context, _ string, lookupIDs []string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.memberCalls = append(s.memberCalls, append([]string(nil), lookupIDs...))
	return append([]string(nil), s.memberHolders...), nil
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

// TestHotSingletonHolderReadRunsAsKeyRanges is the #10643 regression: a hot
// singleton id (mass over the split threshold) must not run as one holder
// statement over all its rows. It runs as one bounded statement per fact_id
// key range plus one member-arm statement, the union of their holders is
// what the pass reopens, and cold ids keep the single packed statement.
func TestHotSingletonHolderReadRunsAsKeyRanges(t *testing.T) {
	t.Parallel()

	ranges := HolderKeyRanges()
	port := &rangeStubFanOut{
		stubFanOut: &stubFanOut{
			holders: []string{"cold-holder"},
			masses:  map[string]int64{"CVE-2026-hot": HolderMassProbeCap},
		},
		rangeHolders: map[HolderKeyRange][]string{
			ranges[0]:             {"holder-a"},
			ranges[len(ranges)-1]: {"holder-lone"},
			ranges[len(ranges)/2]: {"holder-a", "holder-b"},
		},
		memberHolders: []string{"holder-member"},
	}
	ids := []string{"CVE-2026-hot", "CVE-2026-c1", "CVE-2026-c2"}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: ids}}
	result, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, nil))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	for _, call := range port.findCalls {
		for _, id := range call {
			if id == "CVE-2026-hot" {
				t.Fatalf("hot id ran as an unbounded holder statement %v", call)
			}
		}
	}
	if len(port.findCalls) != 1 || !reflect.DeepEqual(port.findCalls[0], []string{"CVE-2026-c1", "CVE-2026-c2"}) {
		t.Fatalf("cold statements = %v, want one packed [c1 c2]", port.findCalls)
	}
	got := append([]HolderKeyRange(nil), port.rangeCalls...)
	sort.Slice(got, func(i, j int) bool { return got[i].Lower < got[j].Lower })
	if !reflect.DeepEqual(got, ranges) {
		t.Fatalf("ranged statements = %v, want every key range once %v", got, ranges)
	}
	for _, call := range port.rangeIDs {
		if !reflect.DeepEqual(call, []string{"CVE-2026-hot"}) {
			t.Fatalf("ranged statement ids = %v, want the hot id alone", call)
		}
	}
	if !reflect.DeepEqual(port.memberCalls, [][]string{{"CVE-2026-hot"}}) {
		t.Fatalf("member-arm statements = %v, want one for the hot id", port.memberCalls)
	}
	want := []string{"cold-holder", "holder-a", "holder-b", "holder-lone", "holder-member"}
	if !reflect.DeepEqual(port.reopened, want) {
		t.Fatalf("reopened = %v, want the union %v", port.reopened, want)
	}
	if got := result.SubSignals["fanout_holder_key_range_statements"]; got != float64(len(ranges)) {
		t.Fatalf("fanout_holder_key_range_statements = %v, want %d", got, len(ranges))
	}
	if got := result.SubSignals["fanout_holder_statements"]; got != float64(1+len(ranges)+1) {
		t.Fatalf("fanout_holder_statements = %v, want cold + ranges + member arm = %d", got, 1+len(ranges)+1)
	}
}

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

// TestHotSingletonKeyRangeFailureFailsClosed proves a timed-out ranged
// statement fails the pass with the retryable fan-out class, counts as a
// holders-site read timeout, and reopens nothing.
func TestHotSingletonKeyRangeFailureFailsClosed(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	ranges := HolderKeyRanges()
	port := &rangeStubFanOut{
		stubFanOut: &stubFanOut{
			holders: []string{"holder-a"},
			masses:  map[string]int64{"CVE-2026-hot": HolderMassProbeCap},
		},
		rangeErr: map[HolderKeyRange]error{ranges[3]: ErrFanOutReadTimeout},
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
		t.Fatalf("reopened = %v after a failed ranged read, want none", port.reopened)
	}
	if got := fanOutCount(t, reader)["read_timeout"]; got != 1 {
		t.Fatalf("read_timeout count = %d, want 1", got)
	}
}
