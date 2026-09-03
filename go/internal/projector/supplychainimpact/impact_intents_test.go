// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychainimpact

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestBuildSupplyChainImpactReducerIntentNoFactNoIntent(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{FactKind: facts.VulnerabilitySourceSnapshotFactKind}})
	if _, ok := BuildSupplyChainImpactReducerIntent("scope-1", "gen-1", lookup); ok {
		t.Fatal("queued a supply_chain_impact intent for a source-snapshot-only fact")
	}
}

func TestBuildSupplyChainImpactReducerIntentEmptyGeneration(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup(nil)
	if _, ok := BuildSupplyChainImpactReducerIntent("scope-1", "gen-1", lookup); ok {
		t.Fatal("queued a supply_chain_impact intent for a generation with no facts at all")
	}
}

func TestBuildSupplyChainImpactReducerIntentFromVulnerabilityCVEFact(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{FactKind: facts.VulnerabilitySourceSnapshotFactKind},
		{
			FactKind:      facts.VulnerabilityCVEFactKind,
			FactID:        "cve-fact-1",
			SourceRef:     facts.Ref{SourceSystem: "vulnerability_intelligence"},
			CollectorKind: "vulnerability_intelligence",
		},
	})
	intent, ok := BuildSupplyChainImpactReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued for a vulnerability_cve fact")
	}
	if intent.Domain != reducer.DomainSupplyChainImpact {
		t.Fatalf("intent.Domain = %q, want supply_chain_impact", intent.Domain)
	}
	if intent.EntityKey != "supply_chain_impact:scope-1" {
		t.Fatalf("intent.EntityKey = %q", intent.EntityKey)
	}
	if intent.Reason != "supply-chain vulnerability evidence observed" {
		t.Fatalf("intent.Reason = %q", intent.Reason)
	}
	if intent.FactID != "cve-fact-1" {
		t.Fatalf("intent.FactID = %q, want the CVE fact", intent.FactID)
	}
}

func TestBuildSupplyChainImpactReducerIntentReasonBySourceKind(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		factKind string
		reason   string
	}{
		{"security alert", facts.SecurityAlertRepositoryAlertFactKind, "provider security alert evidence observed"},
		{"package identity", facts.PackageRegistryPackageFactKind, "package registry identity observed"},
		{"SBOM component", facts.SBOMComponentFactKind, "SBOM package evidence observed"},
		{"suppression", facts.VulnerabilitySuppressionFactKind, "vulnerability suppression evidence observed"},
		{"OCI manifest", facts.OCIImageManifestFactKind, "OCI image subject evidence observed"},
		{"OCI referrer", facts.OCIImageReferrerFactKind, "OCI image subject evidence observed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			lookup := projectorintent.NewFactLookup([]facts.Envelope{{
				FactKind: tc.factKind,
				FactID:   "fact-" + tc.name,
			}})
			intent, ok := BuildSupplyChainImpactReducerIntent("scope-1", "gen-1", lookup)
			if !ok {
				t.Fatalf("no intent queued for a %s fact", tc.name)
			}
			if intent.Reason != tc.reason {
				t.Fatalf("intent.Reason = %q, want %q", intent.Reason, tc.reason)
			}
		})
	}
}

// TestBuildSupplyChainImpactReducerIntentEarliestAcrossKinds proves the
// anchor is the earliest accepted fact in original generation order across
// all twelve candidate kinds, not the earliest fact of the first-checked
// kind.
func TestBuildSupplyChainImpactReducerIntentEarliestAcrossKinds(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{FactKind: facts.SBOMComponentFactKind, FactID: "sbom-fact-1"},
		{FactKind: facts.VulnerabilityCVEFactKind, FactID: "cve-fact-2"},
	})
	intent, ok := BuildSupplyChainImpactReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued")
	}
	if intent.FactID != "sbom-fact-1" {
		t.Fatalf("intent.FactID = %q, want the earliest fact sbom-fact-1, even though vulnerability_cve is checked first in candidateFactKinds", intent.FactID)
	}
}

// TestBuildSupplyChainImpactReducerIntentSourceSystemFallsBackToCollectorKind
// pins the shared two-tier projectorintent.SourceSystem label this family
// uses verbatim: SourceRef.SourceSystem wins when set, else the trimmed
// CollectorKind.
func TestBuildSupplyChainImpactReducerIntentSourceSystemFallsBackToCollectorKind(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{
		FactKind:      facts.VulnerabilityCVEFactKind,
		FactID:        "cve-fact-3",
		CollectorKind: "  vulnerability_intelligence  ",
	}})
	intent, ok := BuildSupplyChainImpactReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued for a vulnerability_cve fact")
	}
	if intent.SourceSystem != "vulnerability_intelligence" {
		t.Fatalf("intent.SourceSystem = %q, want the trimmed CollectorKind fallback", intent.SourceSystem)
	}
}

// TestBuildSupplyChainImpactReducerIntentSourceSystemPrefersSourceRef pins
// the tier ORDER, which the fallback test above cannot: it sets
// SourceRef.SourceSystem and CollectorKind to DIFFERENT values, so a
// regression that swapped the two tiers would change the result. A test
// where both tiers carry the same value passes either way and proves only
// that a label was produced.
func TestBuildSupplyChainImpactReducerIntentSourceSystemPrefersSourceRef(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{
		FactKind:      facts.VulnerabilityCVEFactKind,
		FactID:        "cve-fact-4",
		CollectorKind: "osv_scanner",
		SourceRef:     facts.Ref{SourceSystem: "  vulnerability_intelligence  "},
	}})
	intent, ok := BuildSupplyChainImpactReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued for a vulnerability_cve fact")
	}
	if intent.SourceSystem != "vulnerability_intelligence" {
		t.Fatalf("intent.SourceSystem = %q, want the trimmed SourceRef.SourceSystem to win over CollectorKind %q",
			intent.SourceSystem, "osv_scanner")
	}
}
