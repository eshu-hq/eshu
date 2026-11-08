// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"testing"
)

// Committable 5.2 coverage for eshu-hq/eshu#10354 (Phase 5 5.2), carried
// from the deleted 5.1 scratch shim
// (docs/internal/evidence/9961-shard-scoped-generations.md). Full-shard
// provenance selection runs the real selectAdvisoryProvenance path with
// NVD-deferred CVEs resolving CNA severity from cve_list, one selection
// per record of a 1,000-record shard. 5.1 scratch measured ≈0.76–0.79 ms
// per shard (T2 budget: p95 per-dirty-shard accounting <100 ms).
func BenchmarkCVEListFullShardSelection(b *testing.B) {
	observations := make([]AdvisoryProvenanceObservation, 0, 2000)
	for i := 0; i < 1000; i++ {
		id := fmt.Sprintf("CVE-2024-%04d", 1000+i)
		observations = append(observations,
			AdvisoryProvenanceObservation{Source: "nvd", AdvisoryID: id},
			AdvisoryProvenanceObservation{Source: "cve_list", AdvisoryID: id, SeverityScore: 7.5, SeverityLabel: "HIGH"},
		)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 1000; j++ {
			sel := selectAdvisoryProvenance("linux", observations[2*j:2*j+2])
			if sel.SeveritySource != "cve_list" {
				b.Fatalf("record %d SeveritySource = %q, want cve_list (NVD-deferred fixture broke)", j, sel.SeveritySource)
			}
		}
	}
}
