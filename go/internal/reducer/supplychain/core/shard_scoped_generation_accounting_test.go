// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"testing"
)

// Shard-scoped generation accounting for eshu-hq/eshu#9961 (plan §4.4): one
// full-replace generation per dirty cves/<year>/<NNNxxx> shard. A full
// upstream shard holds at most 1,000 records and the cve_list class emits
// one vulnerability.cve fact per record (see internal/vulnsource
// CVEListShardGenerations and the registry admission of exactly
// vulnerability.cve for cve_list). This test asserts that volume against the
// real per-intent evidence budget and the real truncation verdict, and runs
// the real cve_list provenance path at full-shard scale.
func TestShardScopedGenerationFitsEvidenceBudget(t *testing.T) {
	t.Parallel()

	const fullShardFacts = 1000

	budget := newSupplyChainImpactEvidenceBudget(0)
	if budget.limit != maxSupplyChainImpactEvidenceEnvelopesPerIntent {
		t.Fatalf("evidence budget default = %d, want the package default %d",
			budget.limit, maxSupplyChainImpactEvidenceEnvelopesPerIntent)
	}
	if !budget.charge(fullShardFacts) {
		t.Fatalf("charging a full shard (%d envelopes) trips the evidence budget", fullShardFacts)
	}
	if budget.exhausted() {
		t.Fatal("a full shard exhausts the evidence budget")
	}
	// Even with 10x expansion evidence per fact, a per-shard intent stays
	// under the budget: headroom is two orders of magnitude, not a margin
	// call. The threshold in the 5.1 note requires <10% of budget per shard.
	if got := float64(budget.used) / float64(budget.limit); got >= 0.10 {
		t.Fatalf("full-shard budget share = %.3f, want < 0.10", got)
	}

	var truncation supplyChainImpactTruncation
	if truncation.partial() {
		t.Fatal("zero truncation must not be partial: a complete shard load retracts nothing spuriously")
	}
	if truncation.any() {
		t.Fatal("zero truncation must not discard suppression candidates")
	}
}

func TestShardScopedGenerationSelectsCVEListAtShardScale(t *testing.T) {
	t.Parallel()

	// A full shard of NVD-deferred CVEs: every record must resolve severity
	// from the CNA (cve_list) through the real selection path. This is the
	// reducer cost basis for the per-dirty-shard measurement in the 5.1 note.
	const shardRecords = 1000
	for i := 0; i < shardRecords; i++ {
		id := fmt.Sprintf("CVE-2024-%05d", 12000+i)
		selection := selectAdvisoryProvenance("npm", []AdvisoryProvenanceObservation{
			{Source: "nvd", AdvisoryID: id},
			{Source: "cve_list", AdvisoryID: id, SeverityScore: 7.5, SeverityLabel: "HIGH"},
		})
		if selection.SeveritySource != "cve_list" {
			t.Fatalf("record %d: SeveritySource = %q, want cve_list", i, selection.SeveritySource)
		}
	}
}
