// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSupplyChainImpactFindingsSelectsNewestEPSSDate(t *testing.T) {
	t.Parallel()

	older := vulnerabilityEPSSFact("epss-older", "CVE-2026-0001", "0.12", "0.32")
	older.Payload["score_date"] = "2026-05-15"
	newer := vulnerabilityEPSSFact("epss-newer", "CVE-2026-0001", "0.78", "0.96")
	newer.Payload["score_date"] = "2026-05-16"
	newer.Payload["model_version"] = "v4"
	base := []facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-0001", 9.8),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-0001", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
	}
	for _, order := range []struct {
		name   string
		scores []facts.Envelope
	}{
		{name: "older_then_newer", scores: []facts.Envelope{older, newer}},
		{name: "newer_then_older", scores: []facts.Envelope{newer, older}},
	} {
		t.Run(order.name, func(t *testing.T) {
			input := append(slices.Clone(base), order.scores...)
			findings := BuildSupplyChainImpactFindings(input)
			if len(findings) != 1 {
				t.Fatalf("findings = %d, want 1", len(findings))
			}
			finding := findings[0]
			if finding.EPSSProbability != "0.78" || finding.EPSSPercentile != "0.96" {
				t.Errorf("EPSS = %q/%q, want newest score 0.78/0.96", finding.EPSSProbability, finding.EPSSPercentile)
			}
			provenance := supplyChainImpactProvenancePayload(finding)
			if provenance["selected_epss_score_date"] != "2026-05-16" || provenance["selected_epss_model_version"] != "v4" {
				t.Errorf("persisted provenance = %#v, want selected EPSS date and version", provenance)
			}
			if finding.EPSSScoreDate != "2026-05-16" || finding.EPSSModelVersion != "v4" {
				t.Errorf("EPSS provenance = %q/%q, want 2026-05-16/v4", finding.EPSSScoreDate, finding.EPSSModelVersion)
			}
			if !slices.Contains(finding.EvidenceFactIDs, "epss-newer") || slices.Contains(finding.EvidenceFactIDs, "epss-older") {
				t.Errorf("evidence fact IDs = %#v, want only newest EPSS fact", finding.EvidenceFactIDs)
			}
		})
	}
}

func BenchmarkBuildSupplyChainImpactFindingsTwoEPSSDates(b *testing.B) {
	older := vulnerabilityEPSSFact("epss-older", "CVE-2026-0001", "0.12", "0.32")
	older.Payload["score_date"] = "2026-05-15"
	newer := vulnerabilityEPSSFact("epss-newer", "CVE-2026-0001", "0.78", "0.96")
	newer.Payload["score_date"] = "2026-05-16"
	envelopes := []facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-0001", 9.8),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-0001", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
		newer,
		older,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if findings := BuildSupplyChainImpactFindings(envelopes); len(findings) != 1 {
			b.Fatalf("findings = %d, want 1", len(findings))
		}
	}
}

func TestEPSSSelectionTiesAndKEV(t *testing.T) {
	t.Parallel()
	legacy := vulnerabilityEPSSFact("legacy", "CVE-2026-0002", "0.91", "0.99")
	delete(legacy.Payload, "score_date")
	earlier := vulnerabilityEPSSFact("dated-earlier", "CVE-2026-0002", "0.34", "0.54")
	later := vulnerabilityEPSSFact("dated-later", "CVE-2026-0002", "0.56", "0.76")
	earlier.ObservedAt = time.Date(2026, 5, 16, 8, 0, 0, 0, time.UTC)
	later.ObservedAt = earlier.ObservedAt.Add(time.Hour)
	kev := vulnerabilityKEVFact("kev", "CVE-2026-0002")
	for _, order := range [][]facts.Envelope{
		{legacy, later, kev, earlier},
		{kev, earlier, later, legacy},
	} {
		index, quarantined, err := buildSupplyChainImpactIndexWithQuarantine(order)
		if err != nil || len(quarantined) != 0 {
			t.Fatalf("index error = %v, quarantined = %#v", err, quarantined)
		}
		signals := index.riskSignals["CVE-2026-0002"]
		if signals.EPSSFactID != "dated-later" || signals.EPSSProbability != "0.56" || signals.KEVFactID != "kev" || !signals.KnownExploited {
			t.Errorf("signals = %#v, want later same-day score and independent KEV", signals)
		}
	}
	later.ObservedAt = earlier.ObservedAt
	for _, order := range [][]facts.Envelope{{earlier, later}, {later, earlier}} {
		index, _, err := buildSupplyChainImpactIndexWithQuarantine(order)
		if err != nil {
			t.Fatal(err)
		}
		if got := index.riskSignals["CVE-2026-0002"].EPSSFactID; got != "dated-later" {
			t.Errorf("equal-time tie winner = %q, want stable dated-later", got)
		}
	}
}

func TestEPSSInvalidDateIsQuarantined(t *testing.T) {
	t.Parallel()
	invalid := vulnerabilityEPSSFact("invalid", "CVE-2026-0003", "0.91", "0.99")
	invalid.Payload["score_date"] = "2026-05-99"
	valid := vulnerabilityEPSSFact("valid", "CVE-2026-0003", "0.21", "0.31")
	index, quarantined, err := buildSupplyChainImpactIndexWithQuarantine([]facts.Envelope{invalid, valid})
	if err != nil {
		t.Fatal(err)
	}
	if len(quarantined) != 1 || quarantined[0].FactID != "invalid" {
		t.Fatalf("quarantined = %#v, want invalid fact", quarantined)
	}
	if got := index.riskSignals["CVE-2026-0003"].EPSSFactID; got != "valid" {
		t.Fatalf("selected fact = %q, want valid", got)
	}
}

func TestEPSSSelectedObservationDoesNotInheritOlderModelVersion(t *testing.T) {
	t.Parallel()
	older := vulnerabilityEPSSFact("older", "CVE-2026-0004", "0.12", "0.32")
	older.Payload["score_date"] = "2026-05-15"
	older.Payload["model_version"] = "v3"
	newer := vulnerabilityEPSSFact("newer", "CVE-2026-0004", "0.78", "0.96")
	for _, order := range [][]facts.Envelope{{older, newer}, {newer, older}} {
		index, quarantined, err := buildSupplyChainImpactIndexWithQuarantine(order)
		if err != nil || len(quarantined) != 0 {
			t.Fatalf("index error = %v, quarantined = %#v", err, quarantined)
		}
		signals := index.riskSignals["CVE-2026-0004"]
		if signals.EPSSFactID != "newer" || signals.EPSSModelVersion != "" {
			t.Errorf("signals = %#v, want newer score with absent model version", signals)
		}
	}
}

func TestEPSSInvalidModelVersionIsQuarantined(t *testing.T) {
	t.Parallel()
	invalid := vulnerabilityEPSSFact("invalid-model", "CVE-2026-0005", "0.78", "0.96")
	invalid.Payload["model_version"] = 4
	index, quarantined, err := buildSupplyChainImpactIndexWithQuarantine([]facts.Envelope{invalid})
	if err != nil {
		t.Fatal(err)
	}
	if len(quarantined) != 1 || quarantined[0].FactID != "invalid-model" {
		t.Fatalf("quarantined = %#v, want invalid-model", quarantined)
	}
	if _, ok := index.riskSignals["CVE-2026-0005"]; ok {
		t.Fatal("invalid model version contributed a risk signal")
	}
}

func TestEPSSSelectionWithMissingFactIDKeepsNewerDate(t *testing.T) {
	t.Parallel()
	newer := vulnerabilityEPSSFact("", "CVE-2026-0006", "0.78", "0.96")
	older := vulnerabilityEPSSFact("older", "CVE-2026-0006", "0.12", "0.32")
	older.Payload["score_date"] = "2026-05-15"
	for _, order := range [][]facts.Envelope{{newer, older}, {older, newer}} {
		index, _, err := buildSupplyChainImpactIndexWithQuarantine(order)
		if err != nil {
			t.Fatal(err)
		}
		if got := index.riskSignals["CVE-2026-0006"].EPSSProbability; got != "0.78" {
			t.Errorf("selected probability = %q, want newer unduplicated score", got)
		}
	}
}
