// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"testing"
)

// Shard-scoped generation accounting for eshu-hq/eshu#9961 (plan §4.4): one
// full-replace generation per dirty cves/<year>/<NNNxxx> shard. A full
// upstream shard holds at most 1,000 records, and the cve_list envelope emits
// one vulnerability.cve fact per record plus one vulnerability.reference fact
// per distinct reference URL, so facts are records*1 + references (see
// internal/vulnsource CVEListShardGenerations; eshu-hq/eshu#10718). The first
// test charges the record-only floor of a full shard against the real
// per-intent evidence budget and the real truncation verdict; the break-even
// test below charges the reference term. The last runs the real cve_list
// provenance path at full-shard scale.
func TestShardScopedGenerationFitsEvidenceBudget(t *testing.T) {
	t.Parallel()

	// Record-only floor: 1,000 vulnerability.cve facts and zero references.
	// Real shards add one fact per distinct reference URL; see
	// TestShardScopedGenerationReferenceBreakEven for that term.
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

// TestShardScopedGenerationReferenceBreakEven pins the corrected T1 arithmetic
// for eshu-hq/eshu#10718. T1 requires a full shard to stay under 10% of the
// per-intent evidence budget. With facts = records + references, a
// 1,000-record shard has 10,000 facts of headroom at that share, so T1 holds
// while distinct references stay under 9,000 (under 9 per record on average)
// and fails at 9,000.
func TestShardScopedGenerationReferenceBreakEven(t *testing.T) {
	t.Parallel()

	const (
		fullShardRecords = 1000
		t1Share          = 0.10
	)
	t1Facts := int(float64(maxSupplyChainImpactEvidenceEnvelopesPerIntent) * t1Share)
	if t1Facts != 10_000 {
		t.Fatalf("T1 fact allowance = %d, want 10000 (10%% of the default budget)", t1Facts)
	}
	maxReferences := t1Facts - fullShardRecords - 1 // strictly under 10%
	if maxReferences != 8_999 {
		t.Fatalf("max references under T1 = %d, want 8999", maxReferences)
	}

	under := newSupplyChainImpactEvidenceBudget(0)
	if !under.charge(fullShardRecords + maxReferences) {
		t.Fatal("a full shard just under the T1 allowance trips the evidence budget")
	}
	if got := float64(under.used) / float64(under.limit); got >= t1Share {
		t.Fatalf("shard budget share = %.4f, want < %.2f", got, t1Share)
	}

	at := newSupplyChainImpactEvidenceBudget(0)
	if !at.charge(fullShardRecords + maxReferences + 1) {
		t.Fatal("a shard at the T1 allowance must still load; T1 is a headroom bar, not the budget")
	}
	if got := float64(at.used) / float64(at.limit); got < t1Share {
		t.Fatalf("shard budget share = %.4f, want >= %.2f (T1 must fail at 9,000 references)", got, t1Share)
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
