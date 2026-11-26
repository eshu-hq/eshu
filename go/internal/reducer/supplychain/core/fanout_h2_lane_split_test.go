// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Failing-first consumer tests for #11520 H2: hot-id holder-read isolation
// into hot and cold lanes. Each test pins one H2 acceptance item:
//
// (a) cold packs complete while a hot singleton burns toward the guard,
// under the SAME total bound of 4 over both lanes;
// (b) the isolated (hot singleton + cold pack) union equals the shipped
// single-statement set over the same admitted ids;
// (c) the first error still fails the whole pass: no partial union commit;
// (d) per-lane read kinds and counters are emitted so hot vs cold is visible.
//
// RED on base: every test fails because no lanes exist. The union and
// no-partial-commit behavior already hold on the shared path; what is
// missing is the lane structure, so each test's lane assertion is the
// failing one. Binding constraints pinned throughout: total parallelism
// stays fanOutHolderReadParallelism (4), every id lands in exactly one
// statement, one mass probe per chunk (no extra probe statements), and
// the statement text is the shipped one.
//
// Lane contract under test (literals, so no new exported symbols collide
// with the producer): hot-lane holder reads report kind "holders_hot",
// cold-lane packed reads report kind "holders_cold", and the pass
// sub-signals carry per-lane statement counts.
const (
	h2HotHolderReadKind  = "holders_hot"
	h2ColdHolderReadKind = "holders_cold"

	h2HotStatementsSubSignal  = "fanout_holder_statements_hot"
	h2ColdStatementsSubSignal = "fanout_holder_statements_cold"
)

// h2LaneStub varies holder reads by statement the way the H2 lanes will:
// per-id holder sets (so union identity is meaningful), an optional gate
// that holds the hot singleton open (simulating a burn toward the guard),
// and an optional hot-statement error. It records every call through the
// embedded stub port, so statement partitioning and probe counts assert
// exactly like the shipped path.
type h2LaneStub struct {
	*stubFanOut
	holdersByID map[string][]string
	hotID       string
	hotErr      error
	gateHot     bool

	hotEntered  chan struct{}
	releaseHot  chan struct{}
	enterOnce   sync.Once
	coldDone    chan struct{}
	coldOnce    sync.Once
	hotReturned atomic.Bool

	current atomic.Int32
	peak    atomic.Int32
}

// FindHolderScopes records peak concurrency, then serves per-id holder
// sets. The hot singleton gates or fails as configured; every other
// statement returns the union of its ids' sets.
func (p *h2LaneStub) FindHolderScopes(ctx context.Context, ownScopeID string, lookupIDs []string) ([]string, error) {
	n := p.current.Add(1)
	for {
		peak := p.peak.Load()
		if n <= peak || p.peak.CompareAndSwap(peak, n) {
			break
		}
	}
	defer p.current.Add(-1)
	if _, err := p.stubFanOut.FindHolderScopes(ctx, ownScopeID, lookupIDs); err != nil {
		return nil, err
	}
	if p.hotID != "" && len(lookupIDs) == 1 && lookupIDs[0] == p.hotID {
		if p.hotErr != nil {
			return nil, p.hotErr
		}
		if p.gateHot {
			p.enterOnce.Do(func() { close(p.hotEntered) })
			select {
			case <-p.releaseHot:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			p.hotReturned.Store(true)
		}
		return append([]string(nil), p.holdersByID[p.hotID]...), nil
	}
	var out []string
	for _, id := range lookupIDs {
		out = append(out, p.holdersByID[id]...)
	}
	if p.coldDone != nil {
		p.coldOnce.Do(func() { close(p.coldDone) })
	}
	return out, nil
}

// h2ChunkMasses routes hot over the hot-mass threshold and every cold id
// far under it, so the planned split is one hot singleton plus one cold
// pack with combined mass under budget.
func h2ChunkMasses(hot string, colds []string) map[string]int64 {
	masses := make(map[string]int64, len(colds)+1)
	masses[hot] = int64(fanOutHolderHotMassRows + 1)
	for i, id := range colds {
		masses[id] = int64(10 * (i + 1))
	}
	return masses
}

// h2ReadSamples groups the fan-out read-duration histogram samples by read
// kind and outcome.
func h2ReadSamples(t *testing.T, reader *sdkmetric.ManualReader) map[string]map[string]uint64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	out := map[string]map[string]uint64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_fanout_read_duration_seconds" {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("read-duration metric data = %T, want Histogram[float64]", m.Data)
			}
			for _, dp := range hist.DataPoints {
				kind, _ := dp.Attributes.Value(telemetry.MetricDimensionKind)
				outcome, _ := dp.Attributes.Value("outcome")
				k, o := kind.AsString(), outcome.AsString()
				if out[k] == nil {
					out[k] = map[string]uint64{}
				}
				out[k][o] += dp.Count
			}
		}
	}
	return out
}

// h2FlattenFindCalls returns every lookup id the port saw, asserting the
// admitted set is untouched: every id in exactly one statement.
func h2FlattenFindCalls(t *testing.T, port *stubFanOut, want []string) {
	t.Helper()
	var flat []string
	for _, call := range port.findCalls {
		flat = append(flat, call...)
	}
	if len(flat) != len(want) {
		t.Fatalf("holder statements cover %d ids %v, want %d ids %v: every id lands in exactly one statement", len(flat), flat, len(want), want)
	}
	seen := map[string]int{}
	for _, id := range flat {
		seen[id]++
	}
	for _, id := range want {
		if seen[id] != 1 {
			t.Fatalf("id %q appears %d times in holder statements %v, want exactly once", id, seen[id], port.findCalls)
		}
	}
}

// TestH2ColdLaneCompletesWhileHotBurns is H2 item (a): while the hot
// singleton burns toward the guard, the cold pack of the same pass still
// completes, and total concurrent holder reads never exceed the shared
// bound of 4 over both lanes. RED on base: decoupling holds on the shared
// semaphore, but no hot/cold lane kinds are emitted.
func TestH2ColdLaneCompletesWhileHotBurns(t *testing.T) {
	t.Parallel()

	hot := "CVE-2026-h2hot"
	colds := []string{"CVE-2026-h2c1", "CVE-2026-h2c2", "CVE-2026-h2c3", "CVE-2026-h2c4"}
	chunk := append([]string{hot}, colds...)
	holdersByID := map[string][]string{hot: {"holder-hot"}}
	for _, id := range colds {
		holdersByID[id] = []string{"holder-" + id[len("CVE-2026-"):]}
	}
	port := &h2LaneStub{
		stubFanOut:  &stubFanOut{masses: h2ChunkMasses(hot, colds)},
		holdersByID: holdersByID,
		hotID:       hot,
		gateHot:     true,
		hotEntered:  make(chan struct{}),
		releaseHot:  make(chan struct{}),
		coldDone:    make(chan struct{}),
	}
	release := sync.OnceFunc(func() { close(port.releaseHot) })
	defer release()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	h := fanOutHandler(port, &changeWriter{}, inst)

	type h2ReadResult struct {
		set   map[string]struct{}
		stats fanOutSplitStats
		err   error
	}
	done := make(chan h2ReadResult, 1)
	go func() {
		set, stats, err := h.readHolderScopesSplit(context.Background(), "test", "scope-own", chunk)
		done <- h2ReadResult{set: set, stats: stats, err: err}
	}()

	select {
	case <-port.hotEntered:
	case <-time.After(10 * time.Second):
		t.Fatalf("hot singleton statement never started: want the hot id isolated alone")
	}
	select {
	case <-port.coldDone:
	case <-time.After(10 * time.Second):
		t.Fatalf("cold pack did not complete while the hot singleton burned: lanes do not decouple (no lanes exist)")
	}
	if port.hotReturned.Load() {
		t.Fatal("hot singleton returned before release: the burn simulation is broken")
	}
	if peak := port.peak.Load(); peak > int32(fanOutHolderReadParallelism) {
		t.Fatalf("peak concurrent holder reads = %d, over the shared bound %d: lanes must share one budget of 4", peak, fanOutHolderReadParallelism)
	}

	release()
	var res h2ReadResult
	select {
	case res = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("holder read did not finish after releasing the hot singleton")
	}
	if res.err != nil {
		t.Fatalf("readHolderScopesSplit() error = %v, want nil", res.err)
	}
	wantSet := map[string]struct{}{"holder-hot": {}, "holder-h2c1": {}, "holder-h2c2": {}, "holder-h2c3": {}, "holder-h2c4": {}}
	if !reflect.DeepEqual(res.set, wantSet) {
		t.Fatalf("holder union = %v, want %v", res.set, wantSet)
	}
	h2FlattenFindCalls(t, port.stubFanOut, chunk)

	kinds := h2ReadSamples(t, reader)
	if kinds[h2ColdHolderReadKind]["ok"] < 1 {
		t.Fatalf("no %q ok read sample: cold-lane reads are invisible (no lanes exist); kinds = %v", h2ColdHolderReadKind, kinds)
	}
	if kinds[h2HotHolderReadKind]["ok"] < 1 {
		t.Fatalf("no %q ok read sample: hot-lane reads are invisible (no lanes exist); kinds = %v", h2HotHolderReadKind, kinds)
	}
}

// TestH2IsolatedUnionMatchesShipped is H2 item (b): the isolated run (hot
// singleton plus cold pack) reopens exactly the shipped single-statement
// set over the same admitted ids. RED on base: the union matches, but no
// per-lane statement counters exist.
func TestH2IsolatedUnionMatchesShipped(t *testing.T) {
	t.Parallel()

	hot := "CVE-2026-h2hot"
	colds := []string{"CVE-2026-h2c1", "CVE-2026-h2c2", "CVE-2026-h2c3", "CVE-2026-h2c4"}
	ids := append([]string{hot}, colds...)
	holdersByID := map[string][]string{hot: {"holder-hot"}}
	for _, id := range colds {
		holdersByID[id] = []string{"holder-" + id[len("CVE-2026-"):]}
	}
	port := &h2LaneStub{
		stubFanOut:  &stubFanOut{masses: h2ChunkMasses(hot, colds)},
		holdersByID: holdersByID,
		hotID:       hot,
	}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: ids}}
	result, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, nil))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	want := []string{"holder-h2c1", "holder-h2c2", "holder-h2c3", "holder-h2c4", "holder-hot"}
	got := append([]string(nil), port.reopened...)
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reopened = %v, want the isolated union %v", got, want)
	}
	if len(port.massCalls) != 1 {
		t.Fatalf("mass probe calls = %d, want 1 per 5-id chunk: isolation adds scheduling, never extra probe statements", len(port.massCalls))
	}
	h2FlattenFindCalls(t, port.stubFanOut, ids)
	// The shipped shape over the same admitted set is one statement; its
	// answer must equal the isolated union. This direct call runs after
	// the partition assertions so its extra recorded call cannot pollute
	// the every-id-once check above.
	shipped, err := port.FindHolderScopes(context.Background(), "scope-own", ids)
	if err != nil {
		t.Fatalf("shipped FindHolderScopes() error = %v", err)
	}
	sort.Strings(shipped)
	if !reflect.DeepEqual(shipped, want) {
		t.Fatalf("shipped single-statement holders = %v, want the isolated union %v", shipped, want)
	}
	if got := result.SubSignals[h2HotStatementsSubSignal]; got != 1 {
		t.Fatalf("SubSignals[%q] = %v, want 1 hot-lane statement (no lanes exist)", h2HotStatementsSubSignal, got)
	}
	if got := result.SubSignals[h2ColdStatementsSubSignal]; got != 1 {
		t.Fatalf("SubSignals[%q] = %v, want 1 cold-lane statement (no lanes exist)", h2ColdStatementsSubSignal, got)
	}
}

// TestH2FirstErrorFailsWholePass is H2 item (c): a failing hot singleton
// fails the pass even though the cold pack succeeded, and no holder
// reopens from the partial union. RED on base: the pass already fails
// closed, but the failure names no lane.
func TestH2FirstErrorFailsWholePass(t *testing.T) {
	t.Parallel()

	hot := "CVE-2026-h2hot"
	colds := []string{"CVE-2026-h2c1", "CVE-2026-h2c2", "CVE-2026-h2c3", "CVE-2026-h2c4"}
	ids := append([]string{hot}, colds...)
	holdersByID := map[string][]string{}
	for _, id := range colds {
		holdersByID[id] = []string{"holder-" + id[len("CVE-2026-"):]}
	}
	port := &h2LaneStub{
		stubFanOut:  &stubFanOut{masses: h2ChunkMasses(hot, colds)},
		holdersByID: holdersByID,
		hotID:       hot,
		hotErr:      ErrFanOutReadTimeout,
	}
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: ids}}
	_, err = fanOutHandler(port, writer, inst).Handle(context.Background(), fanOutIntent(1, nil))
	if err == nil {
		t.Fatal("Handle() error = nil, want the hot-lane failure to fail the pass")
	}
	var retryable interface{ Retryable() bool }
	if !errors.As(err, &retryable) || !retryable.Retryable() {
		t.Fatalf("error %v does not report Retryable() == true: the queue would dead-letter the pass", err)
	}
	if len(port.reopened) != 0 {
		t.Fatalf("reopened = %v, want no reopen: the partial union must never commit", port.reopened)
	}
	kinds := h2ReadSamples(t, reader)
	if kinds[h2HotHolderReadKind]["read_timeout"] < 1 {
		t.Fatalf("no %q read_timeout sample: the failed statement names no lane (no lanes exist); kinds = %v", h2HotHolderReadKind, kinds)
	}
}

// TestH2PerLaneReadKindsEmitted is H2 item (d): one hot-plus-cold chunk
// records a hot-lane and a cold-lane read sample while the mass probe
// still runs exactly once per chunk. RED on base: only the unlaned
// "holders" kind is emitted.
func TestH2PerLaneReadKindsEmitted(t *testing.T) {
	t.Parallel()

	hot := "CVE-2026-h2hot"
	colds := []string{"CVE-2026-h2c1", "CVE-2026-h2c2", "CVE-2026-h2c3", "CVE-2026-h2c4"}
	ids := append([]string{hot}, colds...)
	holdersByID := map[string][]string{hot: {"holder-hot"}}
	for _, id := range colds {
		holdersByID[id] = []string{"holder-" + id[len("CVE-2026-"):]}
	}
	port := &h2LaneStub{
		stubFanOut:  &stubFanOut{masses: h2ChunkMasses(hot, colds)},
		holdersByID: holdersByID,
		hotID:       hot,
	}
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: ids}}
	if _, err := fanOutHandler(port, writer, inst).Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(port.massCalls) != 1 {
		t.Fatalf("mass probe calls = %d, want 1 per 5-id chunk: isolation adds scheduling, never extra probe statements", len(port.massCalls))
	}
	kinds := h2ReadSamples(t, reader)
	if kinds[h2HotHolderReadKind]["ok"] < 1 {
		t.Fatalf("no %q ok sample: hot-lane reads are invisible (no lanes exist); kinds = %v", h2HotHolderReadKind, kinds)
	}
	if kinds[h2ColdHolderReadKind]["ok"] < 1 {
		t.Fatalf("no %q ok sample: cold-lane reads are invisible (no lanes exist); kinds = %v", h2ColdHolderReadKind, kinds)
	}
	if kinds[telemetry.FanOutReadKindMassProbe]["ok"] < 1 {
		t.Fatalf("no %q ok sample: the planning probe must stay observable; kinds = %v", telemetry.FanOutReadKindMassProbe, kinds)
	}
}
