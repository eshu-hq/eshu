// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"reflect"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// fallbackSeedLoader is a scopedSeedLoader that also answers the #10415
// out-of-stamp existence check, so fallback tests control the trigger without
// a database. A loader without this seam keeps the pre-fallback scoping
// behavior.
type fallbackSeedLoader struct {
	*scopedSeedLoader
	outOfStamp    bool
	outOfStampErr error
	calls         int
}

func (l *fallbackSeedLoader) HasOutOfStampActiveSupplyChainImpactRows(_ context.Context, _, _ string, _ []string) (bool, error) {
	l.calls++
	if l.outOfStampErr != nil {
		return false, l.outOfStampErr
	}
	return l.outOfStamp, nil
}

// runFallbackPass runs one findings pass over the fixture with a loader that
// answers the out-of-stamp check, returning the recorded write and the pass
// counter reader for mode assertions.
func runFallbackPass(t *testing.T, scopeFacts, active []facts.Envelope, seeds []ScopedFindingPackage, outOfStamp bool, outOfStampErr error, payload map[string]any) (*recordingSupplyChainImpactWriter, *sdkmetric.ManualReader) {
	t.Helper()
	pool := append([]facts.Envelope(nil), active...)
	loader := &fallbackSeedLoader{
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
			seeds: seeds,
		},
		outOfStamp:    outOfStamp,
		outOfStampErr: outOfStampErr,
	}
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	port := &stubFanOut{payload: map[string]any{}}
	writer := &recordingSupplyChainImpactWriter{}
	handler := &SupplyChainImpactHandler{
		FencingTokenIssuer: newTestImpactFencingTokenIssuer(),
		FactLoader:         loader,
		Writer:             writer,
		FanOut:             port,
		Instruments:        inst,
	}
	intent := fanOutIntent(1, payload)
	intent.Payload = payload
	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	return writer, reader
}

// fallbackFixtureSeeds seeds the stamped CVE the way a real reopen would: the
// holder's finding rows for the stamped change.
func fallbackFixtureSeeds() []ScopedFindingPackage {
	return []ScopedFindingPackage{{
		CVEID: "CVE-2026-9001", PackageID: "pkg:npm/a@1", PURL: "pkg:npm/a@1.0.0",
		PackageName: "a", Ecosystem: "npm",
	}}
}

// passModeTotal reads the pass_total counter for one mode out of a test meter
// reader.
func passModeTotal(t *testing.T, reader *sdkmetric.ManualReader, mode string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var out int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_pass_total" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				value, _ := dp.Attributes.Value("mode")
				if value.AsString() == mode {
					out += dp.Value
				}
			}
		}
	}
	return out
}

// TestScopedFallbackRunsFullOnOutOfStampRows pins the #10415 trigger: a
// one-hop bounded-stamp pass whose holder still carries active finding rows
// outside the stamp runs full, so the stale rows converge instead of
// lingering. The fallback reports its own pass mode, never mode=scoped.
func TestScopedFallbackRunsFullOnOutOfStampRows(t *testing.T) {
	t.Parallel()

	scopeFacts, active := differentialFixture()
	writer, reader := runFallbackPass(t, scopeFacts, active, fallbackFixtureSeeds(), true, nil, scopedStampPayload("CVE-2026-9001"))

	if len(writer.write.ScopedCVEIDs) != 0 {
		t.Fatalf("fallback write scope = %v, want full (empty): out-of-stamp rows must be reaped", writer.write.ScopedCVEIDs)
	}
	if got := passModeTotal(t, reader, "scoped_fallback_out_of_stamp"); got != 1 {
		t.Fatalf("fallback mode count = %d, want 1", got)
	}
	if got := passModeTotal(t, reader, passModeScoped); got != 0 {
		t.Fatalf("scoped mode count = %d, want 0: the fallback is not a scoped pass", got)
	}
}

// TestScopedFallbackFindingsMatchFullFindings pins acceptance criterion (1):
// the fallback pass derives exactly what a full pass writes over the same
// fixture, so a quiet holder converges to the full-pass truth.
func TestScopedFallbackFindingsMatchFullFindings(t *testing.T) {
	t.Parallel()

	scopeFacts, active := differentialFixture()
	full, _ := runDifferentialPass(t, scopeFacts, active, fallbackFixtureSeeds(), map[string]any{})
	writer, _ := runFallbackPass(t, scopeFacts, active, fallbackFixtureSeeds(), true, nil, scopedStampPayload("CVE-2026-9001"))

	if len(full) == 0 {
		t.Fatal("full pass derived no findings: the fixture proves nothing")
	}
	if !reflect.DeepEqual(writer.write.Findings, full) {
		t.Fatalf("fallback findings differ from the full pass:\nfallback=%+v\nfull=%+v", writer.write.Findings, full)
	}
}

// TestScopedStaysScopedWithoutOutOfStampRows guards the fast path: a
// one-hop bounded-stamp pass with no rows outside the stamp still scopes, so
// the fallback fires on its stated condition only, not on every reopen.
func TestScopedStaysScopedWithoutOutOfStampRows(t *testing.T) {
	t.Parallel()

	scopeFacts, active := differentialFixture()
	writer, reader := runFallbackPass(t, scopeFacts, active, fallbackFixtureSeeds(), false, nil, scopedStampPayload("CVE-2026-9001"))

	if !reflect.DeepEqual(writer.write.ScopedCVEIDs, []string{"CVE-2026-9001"}) {
		t.Fatalf("write scope = %v, want the stamped set: a clean holder must stay scoped", writer.write.ScopedCVEIDs)
	}
	if got := passModeTotal(t, reader, passModeScoped); got != 1 {
		t.Fatalf("scoped mode count = %d, want 1", got)
	}
	if got := passModeTotal(t, reader, "scoped_fallback_out_of_stamp"); got != 0 {
		t.Fatalf("fallback mode count = %d, want 0", got)
	}
}

// TestScopedFallbackFailsClosedOnCheckError pins the failure direction: an
// unreadable store must not pin a scoped pass that leaves out-of-stamp rows
// behind, so a check error runs the pass full.
func TestScopedFallbackFailsClosedOnCheckError(t *testing.T) {
	t.Parallel()

	scopeFacts, active := differentialFixture()
	writer, _ := runFallbackPass(t, scopeFacts, active, fallbackFixtureSeeds(), false, errors.New("injected store failure"), scopedStampPayload("CVE-2026-9001"))

	if len(writer.write.ScopedCVEIDs) != 0 {
		t.Fatalf("error write scope = %v, want full (empty): the check fails closed", writer.write.ScopedCVEIDs)
	}
}

// TestScopedFallbackKeepsLegacyBehaviorWithoutSeam pins backward
// compatibility: a loader that does not implement the existence check keeps
// the pre-fallback scoping behavior, so test doubles and legacy seams never
// scope by accident and never fall back by accident either.
func TestScopedFallbackKeepsLegacyBehaviorWithoutSeam(t *testing.T) {
	t.Parallel()

	scopeFacts, active := differentialFixture()
	full, fullAdmitted := runDifferentialPass(t, scopeFacts, active, fallbackFixtureSeeds(), map[string]any{})
	scoped, scopedAdmitted := runDifferentialPass(t, scopeFacts, active, fallbackFixtureSeeds(), scopedStampPayload("CVE-2026-9001"))

	if len(findingsForCVE(full, "CVE-2026-9001")) == 0 || len(findingsForCVE(full, "CVE-2026-9002")) == 0 {
		t.Fatal("full pass derived no findings for both CVEs: the fixture proves nothing")
	}
	if scopedAdmitted >= fullAdmitted {
		t.Fatalf("scoped loads admitted %d envelopes, full %d: the scope narrowed nothing", scopedAdmitted, fullAdmitted)
	}
	if got := findingsForCVE(scoped, "CVE-2026-9001"); !reflect.DeepEqual(got, findingsForCVE(full, "CVE-2026-9001")) {
		t.Fatalf("legacy scoped findings differ from the full pass: %+v", got)
	}
}
