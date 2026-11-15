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
	for i := 0; i < len(seq); i++ {
		if seq[i] < '0' || seq[i] > '9' {
			return "", 0, false
		}
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

// CVEListShardCounts is the observed volume of one shard: how many CVE
// records it holds and how many distinct sanitized reference URLs those
// records carry in total.
type CVEListShardCounts struct {
	// Records is the CVE record count in the shard.
	Records int
	// References is the distinct reference URL count the shard's records
	// emit. The collector dedupes per record across the CNA and ADP
	// containers, so a URL shared by two records counts once for each.
	References int
}

// CVEListShardGeneration is one shard-scoped generation: the full-replace
// payload for exactly one dirty shard. The cve_list envelope
// (CVEListRecordEnvelopes in the collector satellite, cvelist_envelope.go)
// emits one vulnerability.cve fact per record plus one
// vulnerability.reference fact per distinct reference URL, so
// Facts = Records*1 + References. Facts is not one to one with Records; the
// reducer-side accounting test charges this volume against the per-intent
// evidence budget.
type CVEListShardGeneration struct {
	// Shard is the cves/<year>/<NNNxxx> directory this generation replaces.
	Shard string
	// Records is the CVE record count the generation carries (at most
	// 1,000 for a full upstream shard).
	Records int
	// References is the distinct reference URL count the generation's
	// records emit, one vulnerability.reference fact each. It is zero when
	// the caller has not observed references for the shard.
	References int
	// Facts is the total fact count the generation emits: one
	// vulnerability.cve per record plus one vulnerability.reference per
	// distinct reference URL (Records + References).
	Facts int
}

// CVEListShardGenerations builds one generation per dirty shard. A shard
// without observed counts defaults to a full shard of records with no
// observed references, so its Facts is the record-only lower bound; callers
// measuring a real delta pass observed counts, including References.
func CVEListShardGenerations(shards []string, counts map[string]CVEListShardCounts) []CVEListShardGeneration {
	gens := make([]CVEListShardGeneration, 0, len(shards))
	for _, shard := range shards {
		c, found := counts[shard]
		if !found {
			c = CVEListShardCounts{Records: cveListShardRecordsPerShard}
		}
		gens = append(gens, CVEListShardGeneration{
			Shard:      shard,
			Records:    c.Records,
			References: c.References,
			Facts:      c.Records + c.References,
		})
	}
	return gens
}
