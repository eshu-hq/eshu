// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnsource

import "testing"

// Shard-scoped generation accounting for eshu-hq/eshu#9961 (plan §4.4): the
// CVE List v5 lays records out as cves/<year>/<NNNxxx> directories of up to
// 1,000 records, and a delta (deltaLog.json) marks shards dirty. Item 5.1
// measures whether one full-replace generation per dirty shard fits the
// reducer. These tests pin the shard mapping and the dirty-shard derivation
// the measurement runs on.
func TestCVEListShardForCVEIDGroupsByThousands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		cveID  string
		want   string
		wantOK bool
	}{
		{cveID: "CVE-2024-12345", want: "cves/2024/12xxx", wantOK: true},
		{cveID: "CVE-2024-12000", want: "cves/2024/12xxx", wantOK: true},
		{cveID: "CVE-2024-12999", want: "cves/2024/12xxx", wantOK: true},
		{cveID: "CVE-2024-0992", want: "cves/2024/0xxx", wantOK: true},
		{cveID: "CVE-1999-0001", want: "cves/1999/0xxx", wantOK: true},
		{cveID: "CVE-2025-123456", want: "cves/2025/123xxx", wantOK: true},
		{cveID: "  CVE-2024-12345  ", want: "cves/2024/12xxx", wantOK: true},
		// Malformed IDs never invent a shard.
		{cveID: ""},
		{cveID: "CVE-2024"},
		{cveID: "CVE-24-12345"},
		{cveID: "CVE-2024-123"},
		{cveID: "CVE-2024-12x45"},
		{cveID: "CVE-2024-+1234"},
		{cveID: "CVE-2024--1234"},
		{cveID: "GHSA-abcd-1234-efgh"},
	}

	for _, tt := range tests {
		t.Run(tt.cveID, func(t *testing.T) {
			t.Parallel()
			got, ok := CVEListShardForCVEID(tt.cveID)
			if ok != tt.wantOK {
				t.Fatalf("CVEListShardForCVEID(%q) ok = %v, want %v", tt.cveID, ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Fatalf("CVEListShardForCVEID(%q) = %q, want %q", tt.cveID, got, tt.want)
			}
		})
	}
}

func TestCVEListDirtyShardsDedupesToOneGenerationPerShard(t *testing.T) {
	t.Parallel()

	// A full day of deltas touches many CVEs but few shards: each dirty
	// shard gets exactly one full-replace generation (plan §4.4).
	delta := []string{
		"CVE-2024-12345",
		"CVE-2024-12001",
		"CVE-2024-12999",
		"CVE-2024-0992",
		"CVE-2023-45678",
		"CVE-2024-12345", // duplicate delivery dedupes to one generation
		"not-a-cve",      // malformed entries are skipped, never generated
	}
	got, skipped := CVEListDirtyShards(delta)
	want := []string{"cves/2023/45xxx", "cves/2024/0xxx", "cves/2024/12xxx"}
	if len(got) != len(want) {
		t.Fatalf("CVEListDirtyShards() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("CVEListDirtyShards() = %v, want %v", got, want)
		}
	}
	if skipped != 1 {
		t.Fatalf("CVEListDirtyShards() skipped = %d, want 1", skipped)
	}
}

// TestCVEListShardGenerationsFactsAreRecordsPlusReferences is regression R11
// (eshu-hq/eshu#10718, design 10671). The cve_list envelope emits one
// vulnerability.cve fact per record plus one vulnerability.reference fact per
// distinct reference URL (cvelist_envelope.go CVEListRecordEnvelopes in the
// collector), so a shard's facts are Records*1 + References, never one to
// one with records. Facts is write volume; the core accounting test uses the
// record-only floor as a hypothetical headroom bar.
func TestCVEListShardGenerationsFactsAreRecordsPlusReferences(t *testing.T) {
	t.Parallel()

	shards := []string{"cves/2024/12xxx", "cves/2024/0xxx", "cves/2024/7xxx"}
	counts := map[string]CVEListShardCounts{
		"cves/2024/12xxx": {Records: 1000, References: 3500},
		"cves/2024/0xxx":  {Records: 37, References: 0},
		"cves/2024/7xxx":  {Records: 10, References: 25},
	}
	wantFacts := map[string]int{
		"cves/2024/12xxx": 4500,
		"cves/2024/0xxx":  37,
		"cves/2024/7xxx":  35,
	}
	gens := CVEListShardGenerations(shards, counts)
	if len(gens) != len(shards) {
		t.Fatalf("CVEListShardGenerations() = %v, want one generation per dirty shard", gens)
	}
	for _, g := range gens {
		if g.Records != counts[g.Shard].Records {
			t.Fatalf("generation for %q records = %d, want %d", g.Shard, g.Records, counts[g.Shard].Records)
		}
		if g.References != counts[g.Shard].References {
			t.Fatalf("generation for %q references = %d, want %d", g.Shard, g.References, counts[g.Shard].References)
		}
		if g.Facts != wantFacts[g.Shard] {
			t.Fatalf("generation for %q carries %d facts, want %d (records*1 + references, not 1:1)",
				g.Shard, g.Facts, wantFacts[g.Shard])
		}
	}
}

func TestCVEListShardGenerationsDefaultsToFullShardOfRecords(t *testing.T) {
	t.Parallel()

	// A shard without observed counts defaults to a full upstream shard of
	// records. References are unobserved there, so Facts is the record-only
	// lower bound, not a total.
	gens := CVEListShardGenerations([]string{"cves/2024/12xxx"}, nil)
	if len(gens) != 1 {
		t.Fatalf("CVEListShardGenerations() = %v, want one generation", gens)
	}
	if gens[0].Records != 1000 || gens[0].References != 0 || gens[0].Facts != 1000 {
		t.Fatalf("default generation = %+v, want 1000 records, 0 observed references, 1000 facts", gens[0])
	}
}
