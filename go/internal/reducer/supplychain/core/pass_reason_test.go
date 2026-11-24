// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// reasonAuthorizedLoader is the smallest non-nil protected loader, enough to
// mark the engaged-protection path without running it.
type reasonAuthorizedLoader struct{}

func (reasonAuthorizedLoader) ListAuthorizedSupplyChainImpactEvidence(
	context.Context, reducercontract.Intent, AuthorizedStaticSupplyChainImpactExpansion,
) (AuthorizedSupplyChainImpactLoad, error) {
	return AuthorizedSupplyChainImpactLoad{}, nil
}

// TestPassReasonOfSplitsTheFullMode pins the #10972 pass reasons: each of the
// ways a pass ends up full, and scoped, reads its own value from the loaded
// evidence, the handler's protection wiring and the intent's stamp.
func TestPassReasonOfSplitsTheFullMode(t *testing.T) {
	t.Parallel()

	bounded := map[string]any{PayloadKeyFanOutOrigin: map[string]any{"cves": []any{"CVE-1"}}}
	sentinel := map[string]any{PayloadKeyFanOutOrigin: map[string]any{"cves": []any{FanOutStampSentinel}}}
	cases := []struct {
		name    string
		handler SupplyChainImpactHandler
		intent  reducercontract.Intent
		loaded  supplyChainImpactLoadedEvidence
		want    string
	}{
		{"scoped", SupplyChainImpactHandler{}, reducercontract.Intent{AttemptCount: 1, Payload: bounded}, supplyChainImpactLoadedEvidence{scoped: &scopedSupplyChainImpactScope{}}, telemetry.SupplyChainImpactPassReasonScoped},
		{"out of stamp probe", SupplyChainImpactHandler{}, reducercontract.Intent{AttemptCount: 1, Payload: bounded}, supplyChainImpactLoadedEvidence{scopedFallbackOutOfStamp: true}, telemetry.SupplyChainImpactPassReasonOutOfStamp},
		{"unstamped genuine pass", SupplyChainImpactHandler{}, reducercontract.Intent{AttemptCount: 1}, supplyChainImpactLoadedEvidence{}, telemetry.SupplyChainImpactPassReasonNoStamp},
		{"stamped retry is genuine again", SupplyChainImpactHandler{}, reducercontract.Intent{AttemptCount: 2, Payload: bounded}, supplyChainImpactLoadedEvidence{}, telemetry.SupplyChainImpactPassReasonNoStamp},
		{"unbounded stamp", SupplyChainImpactHandler{}, reducercontract.Intent{AttemptCount: 1, Payload: sentinel}, supplyChainImpactLoadedEvidence{}, telemetry.SupplyChainImpactPassReasonSentinel},
		{"bounded stamp with nothing to seed", SupplyChainImpactHandler{}, reducercontract.Intent{AttemptCount: 1, Payload: bounded}, supplyChainImpactLoadedEvidence{}, telemetry.SupplyChainImpactPassReasonNoSeed},
		// Production always wires the protected loader. When protection is not
		// engaged (the QA environment) the load returns the static result with
		// asAuthorized unset, so the pass is no_seed, not engaged_protection.
		{"protected loader wired but protection disengaged", SupplyChainImpactHandler{AuthorizedEvidenceLoader: reasonAuthorizedLoader{}}, reducercontract.Intent{AttemptCount: 1, Payload: bounded}, supplyChainImpactLoadedEvidence{}, telemetry.SupplyChainImpactPassReasonNoSeed},
		{"protection engaged in the load", SupplyChainImpactHandler{AuthorizedEvidenceLoader: reasonAuthorizedLoader{}}, reducercontract.Intent{AttemptCount: 1, Payload: bounded}, supplyChainImpactLoadedEvidence{asAuthorized: &AuthorizedSupplyChainImpactLoad{}}, telemetry.SupplyChainImpactPassReasonEngagedProtection},
		{"protected loader without a stamp stays no_stamp", SupplyChainImpactHandler{AuthorizedEvidenceLoader: reasonAuthorizedLoader{}}, reducercontract.Intent{AttemptCount: 1}, supplyChainImpactLoadedEvidence{}, telemetry.SupplyChainImpactPassReasonNoStamp},
	}
	for _, tc := range cases {
		if got := tc.handler.passReasonOf(tc.intent, tc.loaded); got != tc.want {
			t.Fatalf("%s: passReasonOf() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestPassOutcomeCarriesTheScopedAndNoSeedReasons runs two stamped passes
// through Handle: one with finding-row seeds and a local advisory (scoped) and
// one with neither (no_seed), and reads the reason off the counter.
func TestPassOutcomeCarriesTheScopedAndNoSeedReasons(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		seeds      []ScopedFindingPackage
		localFacts bool
		want       string
	}{
		"scoped": {seeds: []ScopedFindingPackage{{
			CVEID: "CVE-2026-9001", PackageID: "pkg:npm/example@1", PURL: "pkg:npm/example@1.2.3",
			PackageName: "example", Ecosystem: "npm",
		}}, localFacts: true, want: "scoped"},
		"no_seed": {want: "no_seed"},
	} {
		reader := sdkmetric.NewManualReader()
		inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
		if err != nil {
			t.Fatal(err)
		}
		var scopeFacts []facts.Envelope
		if tc.localFacts {
			scopeFacts = []facts.Envelope{
				vulnerabilityCVEFact("cve-1", "CVE-2026-9001", 9.8),
				vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-9001", "pkg:npm/example@1", "npm", "example", "1.2.3", "1.3.0"),
			}
		}
		loader := &scopedSeedLoader{
			stubSupplyChainImpactFactLoader: &stubSupplyChainImpactFactLoader{scopeFacts: scopeFacts},
			seeds:                           tc.seeds,
		}
		handler, _, _ := scopedPassHandler(loader, inst)
		intent := fanOutIntent(1, scopedStampPayload("CVE-2026-9001"))
		intent.Domain = reducercontract.DomainSupplyChainImpact
		if _, err := handler.Handle(context.Background(), intent); err != nil {
			t.Fatalf("%s: Handle() error = %v", name, err)
		}
		series, _ := passOutcomeSeriesOf(t, reader)
		if len(series) != 1 {
			t.Fatalf("%s: series = %v, want one point", name, series)
		}
		for key := range series {
			if key.reason != tc.want {
				t.Fatalf("%s: pass_reason = %q, want %q", name, key.reason, tc.want)
			}
		}
	}
}
