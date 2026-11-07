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

func TestCVEListShardGenerationsCarryPerShardFactCounts(t *testing.T) {
	t.Parallel()

	// The cve_list class emits exactly vulnerability.cve per record, so a
	// full-replace generation for a full upstream shard (1,000 records)
	// carries 1,000 facts. The reducer-side test asserts that volume
	// against the per-intent evidence budget.
	shards := []string{"cves/2024/12xxx", "cves/2024/0xxx"}
	records := map[string]int{"cves/2024/12xxx": 1000, "cves/2024/0xxx": 37}
	gens := CVEListShardGenerations(shards, records)
	if len(gens) != len(shards) {
		t.Fatalf("CVEListShardGenerations() = %v, want one generation per dirty shard", gens)
	}
	for _, g := range gens {
		if g.Facts != records[g.Shard] {
			t.Fatalf("generation for %q carries %d facts, want %d (1:1 records)", g.Shard, g.Facts, records[g.Shard])
		}
		if g.Records != records[g.Shard] {
			t.Fatalf("generation for %q records = %d, want %d", g.Shard, g.Records, records[g.Shard])
		}
	}
}
