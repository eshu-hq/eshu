// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/vulnerabilityintelligence"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// lastFactIDWins mirrors the Postgres fact upsert at equal fencing tokens: of
// several envelopes sharing a FactID, the last one is the one stored.
func lastFactIDWins(in []facts.Envelope) []facts.Envelope {
	last := map[string]int{}
	for i, envelope := range in {
		last[envelope.FactID] = i
	}
	out := make([]facts.Envelope, 0, len(in))
	for i, envelope := range in {
		if last[envelope.FactID] == i {
			out = append(out, envelope)
		}
	}
	return out
}

func nvdConfiguration(configurationNegate, nodeNegate bool, match vulnerabilityintelligence.NVDCPEMatch) vulnerabilityintelligence.NVDConfiguration {
	return vulnerabilityintelligence.NVDConfiguration{
		Operator: "OR",
		Negate:   configurationNegate,
		Nodes: []vulnerabilityintelligence.NVDConfigNode{{
			Operator: "OR",
			Negate:   nodeNegate,
			CPEMatch: []vulnerabilityintelligence.NVDCPEMatch{match},
		}},
	}
}

// TestNVDCollectorOutputGivesOrderIndependentFindings pushes one NVD record,
// whose CPE match repeats across configurations, through the real collector
// function and the reducer (with the storage layer's last-write-wins fact
// dedupe applied). The finding count must not depend on configuration order,
// and must be 0 only when every occurrence is negated (#9696).
func TestNVDCollectorOutputGivesOrderIndependentFindings(t *testing.T) {
	t.Parallel()

	match := vulnerabilityintelligence.NVDCPEMatch{
		Vulnerable:      true,
		Criteria:        testImpactProductCriteria,
		MatchCriteriaID: testImpactMatchCriteriaID,
	}
	plain := nvdConfiguration(false, false, match)
	negatedConfiguration := nvdConfiguration(true, false, match)
	negatedNode := nvdConfiguration(false, true, match)

	for _, tc := range []struct {
		name           string
		configurations []vulnerabilityintelligence.NVDConfiguration
		wantFindings   int
	}{
		{name: "plain then negated", configurations: []vulnerabilityintelligence.NVDConfiguration{plain, negatedConfiguration}, wantFindings: 1},
		{name: "negated then plain", configurations: []vulnerabilityintelligence.NVDConfiguration{negatedConfiguration, plain}, wantFindings: 1},
		{name: "plain then negated node", configurations: []vulnerabilityintelligence.NVDConfiguration{plain, negatedNode}, wantFindings: 1},
		{name: "negated node then plain", configurations: []vulnerabilityintelligence.NVDConfiguration{negatedNode, plain}, wantFindings: 1},
		{name: "every occurrence negated", configurations: []vulnerabilityintelligence.NVDConfiguration{negatedConfiguration, negatedNode}, wantFindings: 0},
		{name: "every occurrence negated, reversed", configurations: []vulnerabilityintelligence.NVDConfiguration{negatedNode, negatedConfiguration}, wantFindings: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			emitted, err := vulnerabilityintelligence.NVDRecordEnvelopes(
				vulnerabilityintelligence.VulnerabilityEnvelopeContext{
					ScopeID:             "vuln-intel://nvd/cve",
					GenerationID:        "nvd:2026-10-31T00:00:00Z",
					CollectorInstanceID: "public-nvd",
					ObservedAt:          time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
					SourceURI:           "https://services.nvd.nist.gov/rest/json/cves/2.0?cveId=CVE-2026-0960",
				},
				vulnerabilityintelligence.NVDCVE{ID: "CVE-2026-0960", Configurations: tc.configurations},
			)
			if err != nil {
				t.Fatalf("NVDRecordEnvelopes() error = %v", err)
			}

			stored := lastFactIDWins(emitted)
			stored = append(stored, ownedSBOMProductEvidence()...)
			findings := BuildSupplyChainImpactFindings(stored)

			if got := len(findings); got != tc.wantFindings {
				t.Fatalf("len(findings) = %d, want %d", got, tc.wantFindings)
			}
		})
	}
}
