// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Committable 5.2 coverage for eshu-hq/eshu#10354 (Phase 5 5.2,
// criterion 1 build layer): the in-memory build over a 1,000-fact full
// shard through the production builder chain. Each CVE contributes a
// cve_list vulnerability.cve fact plus the affected-package and
// consumption envelopes that give it an owned anchor, so the builder emits
// findings instead of short-circuiting on empty input.
func BenchmarkCVEListFullShardBuild(b *testing.B) {
	var envelopes []facts.Envelope
	for i := 0; i < 334; i++ {
		cveID := fmt.Sprintf("CVE-2024-%04d", 1000+i)
		envelopes = append(envelopes,
			retractionCVEFact(fmt.Sprintf("cve-%d", i), cveID, "cve_list", cveID, ""),
		)
		envelopes = append(envelopes, retractionPackageEvidence(cveID, "cve_list", cveID)...)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		findings := BuildSupplyChainImpactFindings(envelopes)
		if len(findings) == 0 {
			b.Fatal("BuildSupplyChainImpactFindings() emitted no findings for a live full shard")
		}
	}
}
