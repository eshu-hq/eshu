// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

// Unit pins for eshu-hq/eshu#9878 decision 1 (CVE-keyed re-trigger
// convergence). The durable edge is: an enrichment change flips the finding
// content digest, the writer reports the CVE in ChangedCVEIDs, and the
// holder fan-out reopens every scope holding a copy. These tests pin the
// first link (digest flip -> ChangedCVEIDs, through the real writer against
// a prior-generation baseline) and the fan-out cause classification the
// operator counter carries. The end-to-end convergence proof lives in
// supply_chain_impact_fanout_enrichment_9878_live_test.go (postgres).

import (
	"context"
	"reflect"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestEnrichmentChangeReportsChangedCVE9878 pins the trigger precondition:
// a pass whose only difference from the scope's previous generation is an
// enrichment change (lowered EPSS with a newer score_date, a new KEV
// listing, or a newer EPSS score_date alone) reports the CVE as changed, so
// the fan-out re-triggers the other scopes holding copies. An identical
// re-derivation reports nothing.
func TestEnrichmentChangeReportsChangedCVE9878(t *testing.T) {
	t.Parallel()

	base := mustRescore9878Finding(t, rescore9878BaseEnvelopes(3.0, "0.71", "0.98", "2026-05-16", false))
	for _, tc := range []struct {
		name    string
		finding SupplyChainImpactFinding
		want    []string
	}{
		{
			name:    "lowered EPSS with newer score_date",
			finding: mustRescore9878Finding(t, rescore9878BaseEnvelopes(3.0, "0.12", "0.32", "2026-05-17", false)),
			want:    []string{rescore9878CVE},
		},
		{
			name:    "new KEV listing",
			finding: mustRescore9878Finding(t, rescore9878BaseEnvelopes(3.0, "0.71", "0.98", "2026-05-16", true)),
			want:    []string{rescore9878CVE},
		},
		{
			name:    "newer EPSS score_date alone",
			finding: mustRescore9878Finding(t, rescore9878BaseEnvelopes(3.0, "0.71", "0.98", "2026-05-17", false)),
			want:    []string{rescore9878CVE},
		},
		{
			name:    "identical re-derivation",
			finding: base,
			want:    nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			write := changedTestWrite(tc.finding)
			writer, beginner := changedTestWriter()
			beginner.state.stored = []fakeStoredFinding{priorLike(write, base)}
			result, err := writer.WriteSupplyChainImpactFindings(context.Background(), write)
			if err != nil {
				t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
			}
			if !reflect.DeepEqual(result.ChangedCVEIDs, tc.want) {
				t.Fatalf("ChangedCVEIDs = %v, want %v", result.ChangedCVEIDs, tc.want)
			}
		})
	}
}

// TestFanOutCauseClassification9878 pins the closed cause set: the class of
// the committing generation's own fact kinds. Enrichment generations are
// single-source (one collector target per scope generation), so epss and kev
// are exact there; a generation mixing families reads mixed, and a pass with
// no recognized scope facts reads other.
func TestFanOutCauseClassification9878(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		kinds []string
		want  string
	}{
		{name: "epss only", kinds: []string{chain.VulnerabilityEPSSScoreFactKind}, want: fanOutCauseEPSS},
		{name: "kev only", kinds: []string{chain.VulnerabilityKnownExploitedFactKind}, want: fanOutCauseKEV},
		{name: "advisory only", kinds: []string{chain.VulnerabilityCVEFactKind, chain.VulnerabilityAffectedPackageFactKind}, want: fanOutCauseAdvisory},
		{name: "suppression only", kinds: []string{chain.VulnerabilitySuppressionFactKind}, want: fanOutCauseSuppression},
		{name: "package evidence only", kinds: []string{chain.SBOMComponentFactKind}, want: fanOutCausePackageEvidence},
		{name: "epss with advisory", kinds: []string{chain.VulnerabilityEPSSScoreFactKind, chain.VulnerabilityCVEFactKind}, want: fanOutCauseMixed},
		{name: "epss with kev", kinds: []string{chain.VulnerabilityEPSSScoreFactKind, chain.VulnerabilityKnownExploitedFactKind}, want: fanOutCauseMixed},
		{name: "none", kinds: nil, want: fanOutCauseOther},
		{name: "duplicates collapse", kinds: []string{chain.VulnerabilityEPSSScoreFactKind, chain.VulnerabilityEPSSScoreFactKind}, want: fanOutCauseEPSS},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := fanOutCause(tc.kinds); got != tc.want {
				t.Fatalf("fanOutCause(%q) = %q, want %q", tc.kinds, got, tc.want)
			}
		})
	}
}

// TestFanOutCounterCarriesEnrichmentCause9878 drives a full Handle with an
// EPSS-only scope generation and proves the reopened outcome lands on the
// fan-out counter under cause=epss, so an operator can separate enrichment
// re-triggers from advisory or suppression ones.
func TestFanOutCounterCarriesEnrichmentCause9878(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	port := &stubFanOut{holders: []string{"holder-a"}}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
	handler := SupplyChainImpactHandler{
		FencingTokenIssuer: newTestImpactFencingTokenIssuer(),
		FactLoader: &stubSupplyChainImpactFactLoader{scopeFacts: []facts.Envelope{
			vulnerabilityEPSSFact("epss-9878-cause", "CVE-2026-9001", "0.05", "0.10"),
		}},
		Writer:      writer,
		FanOut:      port,
		Instruments: inst,
		Now:         func() time.Time { return time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC) },
	}
	if _, err := handler.Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !reflect.DeepEqual(port.reopened, []string{"holder-a"}) {
		t.Fatalf("reopened = %v, want the holder", port.reopened)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	found := false
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_fanout_total" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				outcome, _ := dp.Attributes.Value("outcome")
				cause, _ := dp.Attributes.Value("cause")
				if outcome.AsString() == string(FanOutReopened) && dp.Value == 1 {
					found = true
					if cause.AsString() != fanOutCauseEPSS {
						t.Fatalf("reopened cause = %q, want %q", cause.AsString(), fanOutCauseEPSS)
					}
				}
			}
		}
	}
	if !found {
		t.Fatal("no reopened fan-out datapoint recorded")
	}
}
