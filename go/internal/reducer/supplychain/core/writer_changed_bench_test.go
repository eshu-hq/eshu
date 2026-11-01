// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu-sdk/factschema"
)

func benchChangedFindings(n int) []SupplyChainImpactFinding {
	findings := make([]SupplyChainImpactFinding, 0, n)
	for i := 0; i < n; i++ {
		finding := changedTestFinding(fmt.Sprintf("CVE-2026-%06d", i))
		finding.PackageID = fmt.Sprintf("pkg:npm/bench-%d", i)
		finding.PriorityReasonCodes = []string{"kev", "epss_high", "reachable"}
		finding.WorkloadIDs = []string{"workload:a", "workload:b"}
		findings = append(findings, finding)
	}
	return findings
}

// BenchmarkWritePayloadWithoutDigest is the per-finding cost the writer already
// paid before the content digest: build the typed payload, encode it, marshal it.
func BenchmarkWritePayloadWithoutDigest(b *testing.B) {
	write := changedTestWrite()
	findings := benchChangedFindings(1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, finding := range findings {
			payload, err := factschema.EncodeReducerSupplyChainImpactFinding(supplyChainImpactTypedPayload(write, finding))
			if err != nil {
				b.Fatal(err)
			}
			if _, err := json.Marshal(payload); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkWritePayloadWithDigest is the same work plus the content digest the
// fan-out's change detection needs (#9695): one extra marshal and a SHA-256 per
// finding.
func BenchmarkWritePayloadWithDigest(b *testing.B) {
	write := changedTestWrite()
	findings := benchChangedFindings(1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, finding := range findings {
			typed := supplyChainImpactTypedPayload(write, finding)
			digest := contentDigestOf(typed)
			if typed.Provenance == nil {
				typed.Provenance = map[string]any{}
			}
			typed.Provenance[contentDigestProvenanceKey] = digest
			payload, err := factschema.EncodeReducerSupplyChainImpactFinding(typed)
			if err != nil {
				b.Fatal(err)
			}
			if _, err := json.Marshal(payload); err != nil {
				b.Fatal(err)
			}
		}
	}
}
