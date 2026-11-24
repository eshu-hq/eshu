// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"reflect"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// storeShapedOutOfStampLoader answers the #10415 out-of-stamp check the way the
// production probe does (storage/postgres/supply_chain_impact_out_of_stamp.go):
// true when the holder's stored finding rows name any CVE the stamp does not.
// The answer comes from the rows, not from a test flag, so a fixture that gives
// the holder several CVEs gets the answer the real store would give it.
type storeShapedOutOfStampLoader struct {
	*scopedSeedLoader
	rows   []ScopedFindingPackage
	probes int
}

func (l *storeShapedOutOfStampLoader) HasOutOfStampActiveSupplyChainImpactRows(_ context.Context, _, _ string, cveIDs []string) (bool, error) {
	l.probes++
	stamp := make(map[string]struct{}, len(cveIDs))
	for _, id := range cveIDs {
		stamp[id] = struct{}{}
	}
	for _, row := range l.rows {
		if _, ok := stamp[row.CVEID]; !ok && row.CVEID != "" {
			return true, nil
		}
	}
	return false, nil
}

// multiCVEHolderRows is a holder's stored finding rows for two CVEs: the
// stamped CVE-2026-9001 and the unrelated CVE-2026-9002 it has held since its
// last full pass.
func multiCVEHolderRows() []ScopedFindingPackage {
	return []ScopedFindingPackage{
		{CVEID: "CVE-2026-9001", PackageID: "pkg:npm/a@1", PURL: "pkg:npm/a@1.0.0", PackageName: "a", Ecosystem: "npm"},
		{CVEID: "CVE-2026-9002", PackageID: "pkg:npm/b@1", PURL: "pkg:npm/b@1.0.0", PackageName: "b", Ecosystem: "npm"},
	}
}

// markerPayload is a one-CVE stamp on an item whose full-pass marker names
// generation, a scoped-rerun count, and the time of its last full pass. This
// helper is the single place the tests build a marker.
func markerPayload(generation string, scopedSinceFull int, fullAt time.Time, cves ...string) map[string]any {
	payload := scopedStampPayload(cves...)
	payload[PayloadKeyFullPassMarker] = map[string]any{
		FullPassMarkerGenerationKey:      generation,
		FullPassMarkerScopedSinceFullKey: scopedSinceFull,
		FullPassMarkerFullAtKey:          fullAt.UTC().Format(time.RFC3339Nano),
	}
	return payload
}

// markerPassResult is what one store-shaped pass did.
type markerPassResult struct {
	writer *recordingSupplyChainImpactWriter
	reader *sdkmetric.ManualReader
	port   *stubFanOut
	loader *storeShapedOutOfStampLoader
}

// lastMarker returns the full-pass marker the pass's last fan-out state
// update carried, or nil.
func (r markerPassResult) lastMarker(t *testing.T) map[string]any {
	t.Helper()
	if len(r.port.stateCalls) == 0 {
		t.Fatal("the pass ran no fan-out state update")
	}
	return r.port.stateCalls[len(r.port.stateCalls)-1].FullPassMarker
}

// runMarkerPass runs one stamped one-hop pass of the multi-CVE holder through
// the store-shaped loader with the given payload.
func runMarkerPass(t *testing.T, payload map[string]any) markerPassResult {
	t.Helper()
	scopeFacts, active := differentialFixture()
	rows := multiCVEHolderRows()
	pool := append([]facts.Envelope(nil), active...)
	loader := &storeShapedOutOfStampLoader{
		scopedSeedLoader: &scopedSeedLoader{
			stubSupplyChainImpactFactLoader: &stubSupplyChainImpactFactLoader{
				scopeFacts: scopeFacts,
				activeForFilter: func(filter SupplyChainImpactFactFilter) []facts.Envelope {
					var out []facts.Envelope
					for _, e := range pool {
						if differentialEnvelopeMatchesFilter(e, filter) {
							out = append(out, e)
						}
					}
					return out
				},
			},
			seeds: rows,
		},
		rows: rows,
	}
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	result := markerPassResult{writer: &recordingSupplyChainImpactWriter{}, reader: reader, port: &stubFanOut{payload: map[string]any{}}, loader: loader}
	handler := &SupplyChainImpactHandler{
		FencingTokenIssuer: newTestImpactFencingTokenIssuer(),
		FactLoader:         loader,
		Writer:             result.writer,
		FanOut:             result.port,
		Instruments:        inst,
	}
	intent := fanOutIntent(1, payload)
	intent.Payload = payload
	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	return result
}

// wantFullReap asserts the pass reaped in full through the #10415 fallback
// and reset the marker for its generation.
func wantFullReap(t *testing.T, r markerPassResult, why string) {
	t.Helper()
	if len(r.writer.write.ScopedCVEIDs) != 0 {
		t.Errorf("write scope = %v, want full: %s", r.writer.write.ScopedCVEIDs, why)
	}
	if got := passModeTotal(t, r.reader, passModeScopedFallbackOutOfStamp); got != 1 {
		t.Errorf("fallback mode count = %d, want 1: %s", got, why)
	}
	marker := r.lastMarker(t)
	if marker[FullPassMarkerGenerationKey] != "generation-fanout" || marker[FullPassMarkerScopedSinceFullKey] != 0 {
		t.Errorf("marker after the full reap = %v, want generation-fanout with scoped_since_full 0: a full pass resets it", marker)
	}
}

// TestStampedRerunOfAHolderWithOtherRowsStaysScoped is test C of the #10972
// re-run contract. A holder with active rows for two CVEs gets a stamped
// one-hop rerun for one of them, right after a full pass at the same
// generation. Holding a row outside the stamp is the normal state of a
// multi-CVE holder, not drift, so the rerun must re-derive the stamped CVE
// only. Falling to a full pass on every such rerun is what makes a flagged
// rerun long enough to be flagged again (#10415: "not on every reopen").
func TestStampedRerunOfAHolderWithOtherRowsStaysScoped(t *testing.T) {
	t.Parallel()

	r := runMarkerPass(t, markerPayload("generation-fanout", 0, time.Now(), "CVE-2026-9001"))

	fallback := passModeTotal(t, r.reader, passModeScopedFallbackOutOfStamp)
	if !reflect.DeepEqual(r.writer.write.ScopedCVEIDs, []string{"CVE-2026-9001"}) {
		cause := "no_seed or another full path (core/scoped_seed.go:63-65): the stamp found no seed in a holder whose rows name it"
		if fallback == 1 {
			cause = "out_of_stamp (core/scoped_pass.go:80-84): the #10415 fallback fired on a row outside the stamp although a full pass is not due"
		}
		t.Errorf("write scope = %v, want [CVE-2026-9001]; cause: %s", r.writer.write.ScopedCVEIDs, cause)
	}
	if got := passModeTotal(t, r.reader, passModeScoped); got != 1 {
		t.Errorf("scoped mode count = %d, want 1", got)
	}
	if fallback != 0 {
		t.Errorf("fallback mode count = %d, want 0: a full pass is not due, so rows outside the stamp are not drift to reap", fallback)
	}
	if r.loader.probes != 0 {
		t.Errorf("out-of-stamp probes = %d, want 0: the marker decides before the store is asked", r.loader.probes)
	}
}

// TestScopedRerunCountsItselfOnTheMarker pins the counter: a scoped rerun
// keeps the full pass's time and adds one to the scoped-rerun count.
func TestScopedRerunCountsItselfOnTheMarker(t *testing.T) {
	t.Parallel()

	fullAt := time.Now().Add(-10 * time.Minute).UTC()
	r := runMarkerPass(t, markerPayload("generation-fanout", 3, fullAt, "CVE-2026-9001"))

	want := map[string]any{
		FullPassMarkerGenerationKey:      "generation-fanout",
		FullPassMarkerScopedSinceFullKey: 4,
		FullPassMarkerFullAtKey:          fullAt.Format(time.RFC3339Nano),
	}
	if got := r.lastMarker(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("marker after a scoped rerun = %v, want %v", got, want)
	}
}

// TestStampedRerunWithoutAFullPassMarkerReapsInFull pins fail-closed: an item
// with no marker (every item right after rollout) reaps in full once.
func TestStampedRerunWithoutAFullPassMarkerReapsInFull(t *testing.T) {
	t.Parallel()

	wantFullReap(t, runMarkerPass(t, scopedStampPayload("CVE-2026-9001")), "no marker means a reap is due")
}

// TestStampedRerunAtTheScopedRerunBoundReapsInFullAndResets pins N: the
// rerun that reaches the bound reaps in full and resets the count.
func TestStampedRerunAtTheScopedRerunBoundReapsInFullAndResets(t *testing.T) {
	t.Parallel()

	r := runMarkerPass(t, markerPayload("generation-fanout", fullPassReapMaxScopedReruns, time.Now(), "CVE-2026-9001"))
	wantFullReap(t, r, "the scoped-rerun bound is reached")
}

// TestStampedRerunPastTheFullPassAgeReapsInFull pins T: a marker older than
// the age bound makes a reap due even with few scoped reruns.
func TestStampedRerunPastTheFullPassAgeReapsInFull(t *testing.T) {
	t.Parallel()

	stale := time.Now().Add(-fullPassReapMaxAge - time.Minute)
	wantFullReap(t, runMarkerPass(t, markerPayload("generation-fanout", 1, stale, "CVE-2026-9001")), "the last full pass is older than the age bound")
}

// TestStampedRerunWithAnotherGenerationsMarkerReapsInFull pins the
// generation guard: a marker written for another generation proves nothing
// about this one.
func TestStampedRerunWithAnotherGenerationsMarkerReapsInFull(t *testing.T) {
	t.Parallel()

	wantFullReap(t, runMarkerPass(t, markerPayload("generation-older", 0, time.Now(), "CVE-2026-9001")), "the marker names another generation")
}

// TestNextFullPassMarker pins the marker each pass shape leaves behind.
func TestNextFullPassMarker(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 24, 1, 30, 0, 0, time.UTC)
	fullAt := now.Add(-5 * time.Minute)
	withMarker := fanOutIntent(1, markerPayload("generation-fanout", 2, fullAt))
	scoped := supplyChainImpactLoadedEvidence{scoped: &scopedSupplyChainImpactScope{}}
	reset := map[string]any{FullPassMarkerGenerationKey: "generation-fanout", FullPassMarkerScopedSinceFullKey: 0, FullPassMarkerFullAtKey: now.Format(time.RFC3339Nano)}
	for _, tc := range []struct {
		name   string
		intent reducercontract.Intent
		loaded supplyChainImpactLoadedEvidence
		want   map[string]any
	}{
		{"complete full pass resets", withMarker, supplyChainImpactLoadedEvidence{}, reset},
		{"full pass without a marker starts one", fanOutIntent(1, nil), supplyChainImpactLoadedEvidence{}, reset},
		{"partial full pass changes nothing", withMarker, supplyChainImpactLoadedEvidence{truncation: supplyChainImpactTruncation{budget: true}}, nil},
		{"scoped pass counts", withMarker, scoped, map[string]any{FullPassMarkerGenerationKey: "generation-fanout", FullPassMarkerScopedSinceFullKey: 3, FullPassMarkerFullAtKey: fullAt.Format(time.RFC3339Nano)}},
		{"scoped pass without a marker changes nothing", fanOutIntent(1, nil), scoped, nil},
	} {
		if got := nextFullPassMarker(tc.intent, tc.loaded, now); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: marker = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestFullPassMarkerParsesQueueNumbers pins the decode of a marker that came
// back from the queue as JSON, where the count is a float64, and rejects a
// malformed one as due.
func TestFullPassMarkerParsesQueueNumbers(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	fresh := map[string]any{FullPassMarkerGenerationKey: "generation-fanout", FullPassMarkerScopedSinceFullKey: float64(2), FullPassMarkerFullAtKey: now.Format(time.RFC3339Nano)}
	if fullPassReapDue(fanOutIntent(1, map[string]any{PayloadKeyFullPassMarker: fresh}), now) {
		t.Error("a fresh JSON-decoded marker reads as due")
	}
	for name, bad := range map[string]map[string]any{
		"fractional count": {FullPassMarkerGenerationKey: "generation-fanout", FullPassMarkerScopedSinceFullKey: 1.5, FullPassMarkerFullAtKey: now.Format(time.RFC3339Nano)},
		"negative count":   {FullPassMarkerGenerationKey: "generation-fanout", FullPassMarkerScopedSinceFullKey: float64(-1), FullPassMarkerFullAtKey: now.Format(time.RFC3339Nano)},
		"bad time":         {FullPassMarkerGenerationKey: "generation-fanout", FullPassMarkerScopedSinceFullKey: float64(0), FullPassMarkerFullAtKey: "yesterday"},
		"no generation":    {FullPassMarkerScopedSinceFullKey: float64(0), FullPassMarkerFullAtKey: now.Format(time.RFC3339Nano)},
	} {
		if !fullPassReapDue(fanOutIntent(1, map[string]any{PayloadKeyFullPassMarker: bad}), now) {
			t.Errorf("%s: a malformed marker reads as not due; it must fail closed", name)
		}
	}
}

// withoutFullPassMarkers drops the full-pass marker from recorded fan-out
// state updates, for tests that pin the stamp, list and owed fields only. The
// marker carries the wall-clock time of the pass, and its own contract is
// pinned by the tests in this file.
func withoutFullPassMarkers(states []FanOutState) []FanOutState {
	out := make([]FanOutState, len(states))
	for i, state := range states {
		state.FullPassMarker = nil
		out[i] = state
	}
	return out
}
