// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"maps"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// osvTargetsAuthorizedLoader is a protection-engaged loader that returns
// prebuilt scan targets. The static load holds no OS advisory, so every OS
// join the pass reports comes from the per-target pass.
type osvTargetsAuthorizedLoader struct {
	static  *stubSupplyChainImpactFactLoader
	targets []AuthorizedScanImpactEvidence
}

func (l osvTargetsAuthorizedLoader) ListAuthorizedSupplyChainImpactEvidence(
	ctx context.Context,
	_ reducercontract.Intent,
	staticExpansion AuthorizedStaticSupplyChainImpactExpansion,
) (AuthorizedSupplyChainImpactLoad, error) {
	static, err := staticExpansion(ctx, l.static, nil)
	if err != nil {
		return AuthorizedSupplyChainImpactLoad{}, err
	}
	load := AuthorizedSupplyChainImpactLoad{
		ProtectionEngaged:    true,
		StaticEnvelopes:      static.Envelopes,
		SuppressionEnvelopes: static.SuppressionEnvelopes,
		Targets:              l.targets,
		Complete:             true,
	}
	for _, target := range l.targets {
		load.ScanPublications = append(load.ScanPublications, ScanImpactPublicationWitness{
			ScanTargetID: target.ScanTargetID, ScanGenerationID: target.ScanGenerationID, Complete: true,
		})
	}
	return load, nil
}

// TestSupplyChainImpactAuthorizedPathCountsOSAdvisoryJoins pins the #9848
// join signal on the per-target path, the only path that carries the scanner
// worker's derived os_package rows (review F1). Each target's index counts
// its OS advisory facts: a target with a bookworm row joins the Debian
// advisory, a target with only an Ubuntu row does not.
func TestSupplyChainImpactAuthorizedPathCountsOSAdvisoryJoins(t *testing.T) {
	t.Parallel()

	h := newHeadroomHarness(t)
	loader := expandingImpactLoader()
	loader.activeForFilter = nil
	advisories, authority := authorizedOSVAdvisories(t, "debian-cve-2023-38039.json")
	targets := []AuthorizedScanImpactEvidence{
		authorizedOSVTarget(0, advisories, authority, debianCurl("12", "7.88.1-10+deb12u2")),
		authorizedOSVTarget(1, advisories, authority, ubuntuCurl("22.04", "7.81.0-1ubuntu1")),
	}
	handler := SupplyChainImpactHandler{
		FencingTokenIssuer:       newTestImpactFencingTokenIssuer(),
		AuthorizedEvidenceLoader: osvTargetsAuthorizedLoader{static: loader, targets: targets},
		Writer:                   &recordingSupplyChainImpactWriter{},
		Instruments:              h.inst,
	}
	result, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-9848-authorized",
		ScopeID:      "vuln-intel://osv/debian/protected",
		GenerationID: "generation-9848",
		SourceSystem: "vulnerability_intelligence",
		Domain:       reducercontract.DomainSupplyChainImpact,
		Cause:        "vulnerability evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got := result.SubSignals[osAdvisoriesConsideredSubSignal]; got != 2 {
		t.Fatalf("%s = %v, want 2 (one per target index)", osAdvisoriesConsideredSubSignal, got)
	}
	if got := result.SubSignals[osAdvisoriesJoinedSubSignal]; got != 1 {
		t.Fatalf("%s = %v, want 1 (the bookworm target)", osAdvisoriesJoinedSubSignal, got)
	}

	var rm metricdata.ResourceMetrics
	if err := h.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	got := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_os_advisory_joins_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric data = %T, want Sum[int64]", m.Data)
			}
			for _, point := range sum.DataPoints {
				source, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionSource))
				outcome, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionOutcome))
				got[source.AsString()+"/"+outcome.AsString()] += point.Value
			}
		}
	}
	want := map[string]int64{"debian/joined": 1, "debian/not_joined": 1}
	if !maps.Equal(got, want) {
		t.Fatalf("counter points = %v, want %v", got, want)
	}
}
