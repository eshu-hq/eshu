// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestAdaptiveSplitIsolatesHotIDsAtHandler proves the #10643 wiring: one
// 5-id chunk with a hot id reaches the port as a hot singleton plus one cold
// statement, the holders union is de-duplicated across statements, and the
// sub-signals report what split.
func TestAdaptiveSplitIsolatesHotIDsAtHandler(t *testing.T) {
	t.Parallel()

	hot := int64(fanOutHolderHotMassRows + 1)
	port := &stubFanOut{
		holders: []string{"holder-a", "holder-b"},
		masses:  map[string]int64{"CVE-2026-hot": hot},
	}
	ids := []string{"CVE-2026-hot", "CVE-2026-c1", "CVE-2026-c2", "CVE-2026-c3", "CVE-2026-c4"}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: ids}}
	result, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, nil))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(port.massCalls) != 1 {
		t.Fatalf("mass probe calls = %d, want 1 per 5-id chunk", len(port.massCalls))
	}
	if len(port.findCalls) != 2 {
		t.Fatalf("holder read statements = %d, want 2 (hot singleton + cold pack)", len(port.findCalls))
	}
	var sizes []int
	for _, call := range port.findCalls {
		sizes = append(sizes, len(call))
	}
	sort.Ints(sizes)
	if !reflect.DeepEqual(sizes, []int{1, 4}) {
		t.Fatalf("statement sizes = %v, want [1 4]", sizes)
	}
	for _, call := range port.findCalls {
		if len(call) == 1 && call[0] != "CVE-2026-hot" {
			t.Fatalf("singleton statement = %v, want the hot id alone", call)
		}
	}
	if !reflect.DeepEqual(port.reopened, []string{"holder-a", "holder-b"}) {
		t.Fatalf("reopened = %v, want each holder once", port.reopened)
	}
	if got := result.SubSignals["fanout_holder_statements"]; got != 2 {
		t.Fatalf("SubSignals[fanout_holder_statements] = %v, want 2", got)
	}
	if got := result.SubSignals["fanout_hot_ids"]; got != 1 {
		t.Fatalf("SubSignals[fanout_hot_ids] = %v, want 1", got)
	}
	if got := result.SubSignals["fanout_max_id_mass_rows"]; got != float64(hot) {
		t.Fatalf("SubSignals[fanout_max_id_mass_rows] = %v, want %v", got, float64(hot))
	}
}

// TestAdaptiveSplitKeepsColdHandlerCallsWhole proves the cold common case
// still reaches the port as one statement per chunk: the estimator measured
// zero everywhere, so splitting changed nothing.
func TestAdaptiveSplitKeepsColdHandlerCallsWhole(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{holders: []string{"holder-a"}}
	ids := []string{"CVE-2026-0001", "CVE-2026-0002", "CVE-2026-0003", "CVE-2026-0004", "CVE-2026-0005"}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: ids}}
	result, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, nil))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(port.findCalls) != 1 {
		t.Fatalf("holder read statements = %d, want 1 for an all-cold chunk", len(port.findCalls))
	}
	if got := result.SubSignals["fanout_holder_statements"]; got != 1 {
		t.Fatalf("SubSignals[fanout_holder_statements] = %v, want 1", got)
	}
	if got := result.SubSignals["fanout_hot_ids"]; got != 0 {
		t.Fatalf("SubSignals[fanout_hot_ids] = %v, want 0", got)
	}
}

// TestAdaptiveSplitProbeErrorFailsClosed proves a failed mass probe fails the
// pass like a failed holder read: the counter records read_error on the
// holders site and no holder reopens.
func TestAdaptiveSplitProbeErrorFailsClosed(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	port := &stubFanOut{
		holders: []string{"holder-a"},
		massErr: errors.New("boom"),
	}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
	if _, err := fanOutHandler(port, writer, inst).Handle(context.Background(), fanOutIntent(1, nil)); err == nil {
		t.Fatal("Handle() error = nil, want the probe failure")
	}
	if len(port.reopened) != 0 {
		t.Fatalf("reopened = %v, want no reopen on probe failure", port.reopened)
	}
	if got := fanOutCount(t, reader)["read_error"]; got != 1 {
		t.Fatalf("read_error count = %d, want 1", got)
	}
}

// TestAdaptiveSplitProbeTimeoutCountsReadTimeout proves the probe shares the
// holder read's timeout classification: a timed-out probe counts
// read_timeout, not a generic error.
func TestAdaptiveSplitProbeTimeoutCountsReadTimeout(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	port := &stubFanOut{massErr: ErrFanOutReadTimeout}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
	if _, err := fanOutHandler(port, writer, inst).Handle(context.Background(), fanOutIntent(1, nil)); err == nil {
		t.Fatal("Handle() error = nil, want the probe timeout")
	}
	if got := fanOutCount(t, reader)["read_timeout"]; got != 1 {
		t.Fatalf("read_timeout count = %d, want 1", got)
	}
}

// concurrentFanOut wraps the stub port to observe statement overlap: each
// holder read holds a concurrency slot briefly, recording the peak.
type concurrentFanOut struct {
	*stubFanOut
	current atomic.Int32
	peak    atomic.Int32
}

// FindHolderScopes records the peak concurrent holder reads.
func (p *concurrentFanOut) FindHolderScopes(ctx context.Context, ownScopeID string, lookupIDs []string) ([]string, error) {
	n := p.current.Add(1)
	for {
		peak := p.peak.Load()
		if n <= peak || p.peak.CompareAndSwap(peak, n) {
			break
		}
	}
	time.Sleep(10 * time.Millisecond)
	defer p.current.Add(-1)
	return p.stubFanOut.FindHolderScopes(ctx, ownScopeID, lookupIDs)
}

// TestAdaptiveSplitRunsStatementsWithBoundedParallelism proves split
// statements overlap (this is not serialization: the pass must not drop to
// one statement at a time) while the observed peak never exceeds the bound.
func TestAdaptiveSplitRunsStatementsWithBoundedParallelism(t *testing.T) {
	t.Parallel()

	hot := int64(fanOutHolderHotMassRows + 1)
	stub := &stubFanOut{
		holders: []string{"holder-a"},
		masses: map[string]int64{
			"CVE-2026-h1": hot,
			"CVE-2026-h2": hot,
			"CVE-2026-h3": hot,
			"CVE-2026-h4": hot,
			"CVE-2026-h5": hot,
		},
	}
	port := &concurrentFanOut{stubFanOut: stub}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{
		"CVE-2026-h1", "CVE-2026-h2", "CVE-2026-h3", "CVE-2026-h4", "CVE-2026-h5",
	}}}
	if _, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(stub.findCalls) != 5 {
		t.Fatalf("holder read statements = %d, want 5 singletons", len(stub.findCalls))
	}
	peak := port.peak.Load()
	t.Logf("adaptive split peak concurrent holder reads: %d (bound %d)", peak, fanOutHolderReadParallelism)
	if peak > int32(fanOutHolderReadParallelism) {
		t.Fatalf("peak concurrent holder reads = %d, over the bound %d", peak, fanOutHolderReadParallelism)
	}
}
