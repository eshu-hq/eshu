// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// nvdLogicalContext is the configuration- and node-level logic the NVD
// collector copies onto every vulnerability.affected_product payload
// (newNVDAffectedProductEnvelope in the vulnerability-intelligence collector).
type nvdLogicalContext struct {
	configurationOperator string
	configurationNegate   bool
	nodeOperator          string
	nodeNegate            bool
}

// nvdAffectedProductFactWithLogic builds a vulnerability.affected_product
// envelope with the same payload keys the NVD collector emits, including the
// four operator and negate flags the reducer used to drop (#9696).
func nvdAffectedProductFactWithLogic(
	factID string,
	cveID string,
	criteria string,
	matchCriteriaID string,
	logic nvdLogicalContext,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: chain.VulnerabilityAffectedProductFactKind,
		Payload: map[string]any{
			"cve_id":                        cveID,
			"criteria":                      criteria,
			"match_criteria_id":             matchCriteriaID,
			"vulnerable":                    true,
			"source":                        "nvd",
			"source_configuration_operator": logic.configurationOperator,
			"source_configuration_negate":   logic.configurationNegate,
			"source_node_operator":          logic.nodeOperator,
			"source_node_negate":            logic.nodeNegate,
		},
	}
}

// ownedSBOMProductEvidence is the SBOM, attachment, and image evidence that
// turns a vulnerable CPE match into an affected_derived finding for
// testImpactProductCriteria.
func ownedSBOMProductEvidence() []facts.Envelope {
	return []facts.Envelope{
		sbomComponentCPEImpactFact("component-1", "doc-1", testImpactProductCriteria),
		sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
		containerImageIdentityImpactFact("image-1", testImpactSubjectDigest, testImpactRepositoryID),
	}
}

func TestBuildSupplyChainImpactFindingsNegatedNVDConfigurationNeverProducesAffected(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		logic nvdLogicalContext
	}{
		{
			name:  "negated configuration",
			logic: nvdLogicalContext{configurationOperator: "OR", configurationNegate: true, nodeOperator: "OR"},
		},
		{
			name:  "negated node",
			logic: nvdLogicalContext{configurationOperator: "OR", nodeOperator: "OR", nodeNegate: true},
		},
		{
			name:  "negated configuration and node",
			logic: nvdLogicalContext{configurationOperator: "AND", configurationNegate: true, nodeOperator: "AND", nodeNegate: true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			envelopes := []facts.Envelope{
				vulnerabilityCVEFact("cve-negate", "CVE-2026-0900", 8.6),
				nvdAffectedProductFactWithLogic("product-1", "CVE-2026-0900", testImpactProductCriteria, testImpactMatchCriteriaID, tc.logic),
			}
			envelopes = append(envelopes, ownedSBOMProductEvidence()...)

			findings := BuildSupplyChainImpactFindings(envelopes)

			if got := len(findings); got != 0 {
				t.Fatalf("len(findings) = %d, want 0 for a negated NVD configuration: %#v", got, findings)
			}
		})
	}
}

func TestBuildSupplyChainImpactFindingsNonNegatedNVDConfigurationStillProducesAffected(t *testing.T) {
	t.Parallel()

	// Positive control: operators AND/OR with negate=false must keep today's
	// affected_derived result, so the downgrade cannot over-drop (#9696).
	for _, tc := range []struct {
		name  string
		logic nvdLogicalContext
	}{
		{name: "OR node", logic: nvdLogicalContext{configurationOperator: "OR", nodeOperator: "OR"}},
		{name: "AND node", logic: nvdLogicalContext{configurationOperator: "AND", nodeOperator: "AND"}},
		{name: "flags absent", logic: nvdLogicalContext{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			envelopes := []facts.Envelope{
				vulnerabilityCVEFact("cve-negate", "CVE-2026-0901", 8.6),
				nvdAffectedProductFactWithLogic("product-1", "CVE-2026-0901", testImpactProductCriteria, testImpactMatchCriteriaID, tc.logic),
			}
			envelopes = append(envelopes, ownedSBOMProductEvidence()...)

			findings := BuildSupplyChainImpactFindings(envelopes)

			if got, want := len(findings), 1; got != want {
				t.Fatalf("len(findings) = %d, want %d: %#v", got, want, findings)
			}
			assertSupplyChainImpactStatus(t, findings[0], SupplyChainImpactAffectedDerived)
		})
	}
}

func TestBuildSupplyChainImpactFindingsNegatedProductDoesNotHideSiblingMatch(t *testing.T) {
	t.Parallel()

	// One CVE carries a negated match for the owned product and a separate,
	// non-negated match for a different CPE the SBOM also lists. Only the
	// non-negated match may produce a finding, whichever fact comes first: a
	// downgrade that wiped the CVE's earlier matches would pass only one order.
	// This covers a sibling with a different CPE. The same CPE repeated under
	// another configuration is one fact per CVE and is merged by the collector.
	const siblingCriteria = "cpe:2.3:a:example:agent:2.0.0:*:*:*:*:*:*:*"
	negated := nvdAffectedProductFactWithLogic("product-negated", "CVE-2026-0902", testImpactProductCriteria, testImpactMatchCriteriaID,
		nvdLogicalContext{configurationOperator: "OR", configurationNegate: true, nodeOperator: "OR"})
	live := nvdAffectedProductFactWithLogic("product-live", "CVE-2026-0902", siblingCriteria, "b5ec4c98-0000-4000-9000-000000000002",
		nvdLogicalContext{configurationOperator: "OR", nodeOperator: "OR"})

	for _, tc := range []struct {
		name     string
		products []facts.Envelope
	}{
		{name: "negated first", products: []facts.Envelope{negated, live}},
		{name: "live first", products: []facts.Envelope{live, negated}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			envelopes := []facts.Envelope{vulnerabilityCVEFact("cve-negate", "CVE-2026-0902", 8.6)}
			envelopes = append(envelopes, tc.products...)
			envelopes = append(envelopes,
				sbomComponentCPEImpactFact("component-1", "doc-1", testImpactProductCriteria),
				sbomComponentCPEImpactFact("component-2", "doc-1", siblingCriteria),
				sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
				containerImageIdentityImpactFact("image-1", testImpactSubjectDigest, testImpactRepositoryID),
			)

			findings := BuildSupplyChainImpactFindings(envelopes)

			if got, want := len(findings), 1; got != want {
				t.Fatalf("len(findings) = %d, want %d: %#v", got, want, findings)
			}
			if got := findings[0].ProductCriteria; got != siblingCriteria {
				t.Fatalf("ProductCriteria = %q, want the non-negated sibling %q", got, siblingCriteria)
			}
		})
	}
}

// TestBuildSupplyChainImpactFindingsOlderProductFactsAreNotNegated proves a
// fact without the four logic keys (written before the collector kept them), or
// carrying JSON null for them, is not read as negated.
func TestBuildSupplyChainImpactFindingsOlderProductFactsAreNotNegated(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		mutate func(payload map[string]any)
	}{
		{name: "keys omitted", mutate: func(payload map[string]any) {
			for _, key := range []string{"source_configuration_operator", "source_configuration_negate", "source_node_operator", "source_node_negate"} {
				delete(payload, key)
			}
		}},
		{name: "null values", mutate: func(payload map[string]any) {
			for _, key := range []string{"source_configuration_operator", "source_configuration_negate", "source_node_operator", "source_node_negate"} {
				payload[key] = nil
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			product := nvdAffectedProductFactWithLogic("product-1", "CVE-2026-0904", testImpactProductCriteria, testImpactMatchCriteriaID, nvdLogicalContext{})
			tc.mutate(product.Payload)
			envelopes := []facts.Envelope{vulnerabilityCVEFact("cve-negate", "CVE-2026-0904", 8.6), product}
			envelopes = append(envelopes, ownedSBOMProductEvidence()...)

			findings := BuildSupplyChainImpactFindings(envelopes)

			if got, want := len(findings), 1; got != want {
				t.Fatalf("len(findings) = %d, want %d for an older fact: %#v", got, want, findings)
			}
			assertSupplyChainImpactStatus(t, findings[0], SupplyChainImpactAffectedDerived)
		})
	}
}

func TestSupplyChainAffectedProductFromEnvelopeDecodesLogicalFlags(t *testing.T) {
	t.Parallel()

	envelope := nvdAffectedProductFactWithLogic("product-1", "CVE-2026-0903", testImpactProductCriteria, testImpactMatchCriteriaID,
		nvdLogicalContext{configurationOperator: "AND", configurationNegate: true, nodeOperator: "OR", nodeNegate: true})

	product, err := supplyChainAffectedProductFromEnvelope(envelope)
	if err != nil {
		t.Fatalf("supplyChainAffectedProductFromEnvelope() error = %v", err)
	}

	if !product.ConfigurationNegate || !product.NodeNegate {
		t.Fatalf("negate flags = (%t, %t), want both true", product.ConfigurationNegate, product.NodeNegate)
	}
	if product.ConfigurationOperator != "AND" || product.NodeOperator != "OR" {
		t.Fatalf("operators = (%q, %q), want (AND, OR)", product.ConfigurationOperator, product.NodeOperator)
	}
	if !product.Negated() {
		t.Fatal("Negated() = false, want true when either negate flag is set")
	}
}
