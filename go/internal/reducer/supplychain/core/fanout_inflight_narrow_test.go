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
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// narrowTestInstruments returns instruments over a manual reader.
func narrowTestInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	return inst, reader
}

// changedCVEWriter is a writer whose commit changed one CVE, so the pass fans
// out and reads the in-flight set.
func changedCVEWriter() *changeWriter {
	return &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
}

// TestInFlightClaimedAfterTheSourceLoadIsNotFlagged is the D4b contract
// (#10972): a pass claimed after the source's evidence load ended (in set B,
// not in set A) loads its own evidence later and sees everything the source
// saw, so it is not flagged. A pass that was already claimed at the load end
// (in both sets with the same claim) is still flagged.
func TestInFlightClaimedAfterTheSourceLoadIsNotFlagged(t *testing.T) {
	t.Parallel()

	inst, reader := narrowTestInstruments(t)
	port := &stubFanOut{
		inFlight: []string{"before-load", "after-load"},
		early:    []InFlightClaim{{ScopeID: "before-load", ClaimedAt: stubInFlightClaimTime}},
		earlySet: true,
	}
	var logged bytes.Buffer
	handler := fanOutHandler(port, changedCVEWriter(), inst)
	handler.Logger = slog.New(slog.NewTextHandler(&logged, nil))
	if _, err := handler.Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
		t.Fatal(err)
	}
	if want := []string{"before-load"}; !reflect.DeepEqual(port.reopened, want) {
		t.Fatalf("reopened = %v, want %v: only the claim in both sets is flagged", port.reopened, want)
	}
	if port.earlyCalls != 1 {
		t.Fatalf("load-end claim reads = %d, want one per genuine pass", port.earlyCalls)
	}
	counts := fanOutCount(t, reader)
	if counts[fanOutOutcomeInFlightNarrowed] != 1 || counts[fanOutOutcomeInFlightKept] != 1 {
		t.Fatalf("narrowing outcomes = %v, want in_flight_narrowed=1 in_flight_kept=1", counts)
	}
	for _, want := range []string{"in_flight_narrowed=1", "in_flight_kept=1", "in_flight_early_read=ok"} {
		if !strings.Contains(logged.String(), want) {
			t.Fatalf("fan-out log line lacks %q:\n%s", want, logged.String())
		}
	}
}

// TestInFlightReclaimedAfterTheSourceLoadIsNotFlagged: the scope was in flight
// at the load end, acknowledged, and was claimed again before the fan-out. The
// new claim has a different identity, so it is not matched; the row holders
// are still reopened as before.
func TestInFlightReclaimedAfterTheSourceLoadIsNotFlagged(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{
		holders:  []string{"row-holder"},
		inFlight: []string{"reclaimed"},
		early:    []InFlightClaim{{ScopeID: "reclaimed", ClaimedAt: stubInFlightClaimTime.Add(-time.Second)}},
		earlySet: true,
	}
	if _, err := fanOutHandler(port, changedCVEWriter(), nil).Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
		t.Fatal(err)
	}
	if want := []string{"row-holder"}; !reflect.DeepEqual(port.reopened, want) {
		t.Fatalf("reopened = %v, want %v: the old claim must not match the new one", port.reopened, want)
	}
}

// TestInFlightClaimWithNoIdentityIsAlwaysFlagged: a claim with no recorded
// identity cannot be proven new, so it is flagged even when set A names it.
func TestInFlightClaimWithNoIdentityIsAlwaysFlagged(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{
		inFlight:  []string{"unknown"},
		claimedAt: map[string]time.Time{"unknown": {}},
		early:     []InFlightClaim{{ScopeID: "unknown"}},
		earlySet:  true,
	}
	if _, err := fanOutHandler(port, changedCVEWriter(), nil).Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
		t.Fatal(err)
	}
	if want := []string{"unknown"}; !reflect.DeepEqual(port.reopened, want) {
		t.Fatalf("reopened = %v, want %v", port.reopened, want)
	}
}

// TestInFlightEarlyReadFailureFlagsEveryInFlightPass: when the load-end read
// fails or times out, nothing proves any claim new, so every claim in set B is
// flagged, as before D4b, and the pass still succeeds.
func TestInFlightEarlyReadFailureFlagsEveryInFlightPass(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		err  error
		want string
	}{"timeout": {ErrFanOutReadTimeout, fanOutOutcomeReadTimeout}, "error": {errors.New("boom"), fanOutOutcomeReadError}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			inst, reader := narrowTestInstruments(t)
			port := &stubFanOut{inFlight: []string{"a", "b"}, earlyErr: tc.err}
			if _, err := fanOutHandler(port, changedCVEWriter(), inst).Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
				t.Fatalf("Handle() error = %v, want the pass to succeed after a failed load-end read", err)
			}
			if want := []string{"a", "b"}; !reflect.DeepEqual(port.reopened, want) {
				t.Fatalf("reopened = %v, want every in-flight claim %v", port.reopened, want)
			}
			if port.earlyCalls != 1 {
				t.Fatalf("load-end claim reads = %d, want 1", port.earlyCalls)
			}
			if got := fanOutCount(t, reader)[fanOutOutcomeInFlightEarlyReadFailed]; got != 2 {
				t.Fatalf("in_flight_early_read_failed = %d, want 2 (%v)", got, fanOutCount(t, reader))
			}
			if got := fanOutReadSiteCount(t, reader, tc.want, telemetry.FanOutReadSiteInFlightEarly); got != 1 {
				t.Fatalf("%s at site %s = %d, want 1", tc.want, telemetry.FanOutReadSiteInFlightEarly, got)
			}
		})
	}
}

// TestOneHopPassTakesNoEarlyRead: a one-hop rerun fans out only its
// suppression ids, so it does not pay for the load-end read. When it does read
// set B, every claim is kept and counted as such.
func TestOneHopPassTakesNoEarlyRead(t *testing.T) {
	t.Parallel()

	inst, reader := narrowTestInstruments(t)
	port := &stubFanOut{inFlight: []string{"a"}}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{SuppressionCVEIDs: []string{"CVE-2026-9001"}}}
	intent := fanOutIntent(1, map[string]any{PayloadKeyFanOutOrigin: map[string]any{"scope_id": "src"}})
	if _, err := fanOutHandler(port, writer, inst).Handle(context.Background(), intent); err != nil {
		t.Fatal(err)
	}
	if port.earlyCalls != 0 {
		t.Fatalf("load-end claim reads = %d, want none on a one-hop pass", port.earlyCalls)
	}
	if want := []string{"a"}; !reflect.DeepEqual(port.reopened, want) {
		t.Fatalf("reopened = %v, want %v", port.reopened, want)
	}
	if got := fanOutCount(t, reader)[fanOutOutcomeInFlightEarlyReadSkipped]; got != 1 {
		t.Fatalf("in_flight_early_read_skipped = %d, want 1 (%v)", got, fanOutCount(t, reader))
	}
}

// earlyReadOrderWriter fails the test when the load-end read has not run by
// the time the writer starts: set A must be read before any writer-phase read.
type earlyReadOrderWriter struct {
	*changeWriter
	port *stubFanOut
	t    *testing.T
}

func (w earlyReadOrderWriter) WriteSupplyChainImpactFindings(ctx context.Context, write SupplyChainImpactWrite) (SupplyChainImpactWriteResult, error) {
	w.port.mu.Lock()
	calls := w.port.earlyCalls
	w.port.mu.Unlock()
	if calls != 1 {
		w.t.Errorf("load-end claim reads at write time = %d, want 1: set A is read after the load and before the writer", calls)
	}
	return w.changeWriter.WriteSupplyChainImpactFindings(ctx, write)
}

// TestEarlyReadRunsBeforeTheWriter pins where set A is read: after the
// evidence load and before the writer's own reads.
func TestEarlyReadRunsBeforeTheWriter(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{inFlight: []string{"a"}}
	writer := earlyReadOrderWriter{changeWriter: changedCVEWriter(), port: port, t: t}
	if _, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
		t.Fatal(err)
	}
}

// fanOutReadSiteCount sums the fan-out counter points with this outcome and
// read site.
func fanOutReadSiteCount(t *testing.T, reader *sdkmetric.ManualReader, outcome, site string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_fanout_total" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				o, _ := dp.Attributes.Value("outcome")
				s, _ := dp.Attributes.Value(telemetry.MetricDimensionSite)
				if o.AsString() == outcome && s.AsString() == site {
					total += dp.Value
				}
			}
		}
	}
	return total
}
