// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnsource

import (
	"sort"
	"strconv"
	"strings"
)

// CVE List v5 shard layout for the item 5.1 measurement (eshu-hq/eshu#9961,
// plan §4.4). Upstream lays records out as cves/<year>/<NNNxxx> directories
// of up to 1,000 records each, so a CVE maps to its shard by integer-dividing
// its sequence number by 1,000. A delta (deltaLog.json) marks shards dirty,
// and each dirty shard is re-collected in full, so one generation replaces
// exactly one shard and retraction stays correct. This file is the pure
// measurement substrate: no network, no database, no workflow mutation.

// cveListShardRecordsPerShard is the upstream directory fan-out: one
// <NNNxxx> directory holds sequence numbers [N*1000, N*1000+999].
const cveListShardRecordsPerShard = 1000

// CVEListShardForCVEID maps a CVE ID to its CVE List v5 shard directory,
// cves/<year>/<NNNxxx>. ok is false for any ID outside the strict
// CVE-<year>-<sequence> grammar (4-digit year, 4-or-more-digit sequence),
// so a caller never invents a shard.
func CVEListShardForCVEID(cveID string) (shard string, ok bool) {
	year, sequence, valid := splitCVEListID(cveID)
	if !valid {
		return "", false
	}
	group := sequence / cveListShardRecordsPerShard
	return "cves/" + year + "/" + strconv.Itoa(group) + "xxx", true
}

// splitCVEListID splits a CVE ID into its year and sequence number. The
// match is case-insensitive on the CVE- prefix; the year is exactly four
// digits and the sequence is four or more digits.
func splitCVEListID(cveID string) (year string, sequence int, valid bool) {
	rest, found := strings.CutPrefix(strings.ToUpper(strings.TrimSpace(cveID)), "CVE-")
	if !found {
		return "", 0, false
	}
	year, seq, found := strings.Cut(rest, "-")
	if !found || len(year) != 4 {
		return "", 0, false
	}
	for i := 0; i < len(year); i++ {
		if year[i] < '0' || year[i] > '9' {
			return "", 0, false
		}
	}
	if len(seq) < 4 {
		return "", 0, false
	}
	n, err := strconv.Atoi(seq)
	if err != nil {
		return "", 0, false
	}
	return year, n, true
}

// CVEListDirtyShards reduces delta-touched CVE IDs (deltaLog.json entries)
// to the sorted unique shard directories that need re-collection. Each
// returned shard gets exactly one full-replace generation. Malformed IDs
// are skipped and reported via skipped, never generated.
func CVEListDirtyShards(deltaCVEIDs []string) (shards []string, skipped int) {
	seen := make(map[string]struct{}, len(deltaCVEIDs))
	for _, id := range deltaCVEIDs {
		shard, ok := CVEListShardForCVEID(id)
		if !ok {
			skipped++
			continue
		}
		seen[shard] = struct{}{}
	}
	shards = make([]string, 0, len(seen))
	for shard := range seen {
		shards = append(shards, shard)
	}
	sort.Strings(shards)
	return shards, skipped
}

// CVEListShardGeneration is one shard-scoped generation: the full-replace
// payload for exactly one dirty shard. The cve_list class emits one
// vulnerability.cve fact per record, so Facts equals Records; the
// reducer-side accounting test asserts that volume against the per-intent
// evidence budget.
type CVEListShardGeneration struct {
	// Shard is the cves/<year>/<NNNxxx> directory this generation replaces.
	Shard string
	// Records is the CVE record count the generation carries (at most
	// 1,000 for a full upstream shard).
	Records int
	// Facts is the vulnerability.cve fact count the generation emits.
	Facts int
}

// CVEListShardGenerations builds one generation per dirty shard. Shards
// without a record count default to a full shard; callers measuring a real
// delta pass observed counts.
func CVEListShardGenerations(shards []string, records map[string]int) []CVEListShardGeneration {
	gens := make([]CVEListShardGeneration, 0, len(shards))
	for _, shard := range shards {
		n, found := records[shard]
		if !found {
			n = cveListShardRecordsPerShard
		}
		gens = append(gens, CVEListShardGeneration{Shard: shard, Records: n, Facts: n})
	}
	return gens
}
