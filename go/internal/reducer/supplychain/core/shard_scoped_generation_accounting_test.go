// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"testing"

	chain "github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// Shard-scoped generation accounting for eshu-hq/eshu#9961 (plan §4.4): one
// full-replace generation per dirty cves/<year>/<NNNxxx> shard. A full
// upstream shard holds at most 1,000 records, and the cve_list envelope emits
// one vulnerability.cve fact per record plus one vulnerability.reference fact
// per distinct reference URL, so facts are records*1 + references (see
// internal/vulnsource CVEListShardGenerations; eshu-hq/eshu#10718). The first
// test charges the record-only floor of a full shard against the real
// per-intent evidence budget and the real truncation verdict; the second pins
// that the reducer does not load reference facts; the last runs the real
// cve_list provenance path at full-shard scale.
func TestShardScopedGenerationFitsEvidenceBudget(t *testing.T) {
	t.Parallel()

	// Record-only floor: 1,000 vulnerability.cve facts and zero references.
	// Real shards add one fact per distinct reference URL, which the reducer does
	// not load (see TestShardScopedGenerationDoesNotLoadReferenceFacts). The
	// production budget also counts expansion evidence only, never the intent's
	// own base scope load, so this charge is a hypothetical headroom bar for a
	// shard-sized load, not budget use the reducer performs today.
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

// TestShardScopedGenerationDoesNotLoadReferenceFacts pins the premise of the
// T1 restatement for eshu-hq/eshu#10718: the reducer loads no
// vulnerability.reference facts, so they are write volume and do not charge
// the per-intent evidence budget. If this fails, the reducer started loading
// them, and the 5.1 note and the break-even below must be redone.
func TestShardScopedGenerationDoesNotLoadReferenceFacts(t *testing.T) {
	t.Parallel()

	for _, kind := range supplyChainImpactFactKinds() {
		if kind == chain.VulnerabilityReferenceFactKind {
			t.Fatalf("supplyChainImpactFactKinds() loads %q: reference facts now charge the evidence budget; redo the T1 restatement in the 9961 note", kind)
		}
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
