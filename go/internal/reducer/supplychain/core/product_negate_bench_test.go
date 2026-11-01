// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// benchProductEnvelopes builds count CVEs with one NVD match each, every
// negateEvery-th match under a negated configuration (0 disables negation).
// It uses only payload keys the NVD collector emits, so the same benchmark
// compiles and runs against the code before the #9696 change.
func benchProductEnvelopes(count int, negateEvery int) []facts.Envelope {
	envelopes := make([]facts.Envelope, 0, count*2)
	for i := 0; i < count; i++ {
		cveID := fmt.Sprintf("CVE-2026-%05d", i)
		envelopes = append(envelopes, vulnerabilityCVEFact(fmt.Sprintf("cve-%d", i), cveID, 7.5))
		envelopes = append(envelopes, nvdAffectedProductFactWithLogic(
			fmt.Sprintf("product-%d", i),
			cveID,
			fmt.Sprintf("cpe:2.3:a:example:product%d:1.0:*:*:*:*:*:*:*", i),
			fmt.Sprintf("match-%d", i),
			nvdLogicalContext{
				configurationOperator: "OR",
				configurationNegate:   negateEvery > 0 && i%negateEvery == 0,
				nodeOperator:          "OR",
			},
		))
	}
	return envelopes
}

// BenchmarkBuildSupplyChainImpactIndexAffectedProducts measures the index
// build over 5000 NVD matches with no negation and with every fourth match
// negated, so the decode of the four logic flags and the negated drop are both
// on the measured path (#9696).
func BenchmarkBuildSupplyChainImpactIndexAffectedProducts(b *testing.B) {
	for _, tc := range []struct {
		name        string
		negateEvery int
	}{
		{name: "none_negated", negateEvery: 0},
		{name: "quarter_negated", negateEvery: 4},
	} {
		envelopes := benchProductEnvelopes(5000, tc.negateEvery)
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, _, err := buildSupplyChainImpactIndexWithQuarantine(envelopes); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkBuildSupplyChainImpactFindingsAffectedProducts measures the whole
// in-process build (index, grouping, and classification) over the same 5000 NVD
// matches with no SBOM evidence, with and without negated matches, so the index
// build's cost can be read as a share of the work around it (#9696).
func BenchmarkBuildSupplyChainImpactFindingsAffectedProducts(b *testing.B) {
	for _, tc := range []struct {
		name        string
		negateEvery int
	}{
		{name: "none_negated", negateEvery: 0},
		{name: "quarter_negated", negateEvery: 4},
	} {
		envelopes := benchProductEnvelopes(5000, tc.negateEvery)
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, _, err := buildSupplyChainImpactFindingsWithQuarantine(envelopes); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
