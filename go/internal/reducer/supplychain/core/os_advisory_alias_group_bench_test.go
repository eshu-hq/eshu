// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// aliasBenchEnvelopes builds n alias-paired advisory records (n even): each
// GHSA-i record aliases CVE-i and each CVE-i record aliases back, so the
// index build unions n/2 two-member components. Every record carries one
// affected_package fact so the package remap runs too.
func aliasBenchEnvelopes(n int) []facts.Envelope {
	if n%2 != 0 {
		panic("aliasBenchEnvelopes needs an even record count")
	}
	out := make([]facts.Envelope, 0, 2*n)
	// Representative 1.2 KB advisory text per record, mirroring the cost
	// proof's 1.5 KB payloads so the decode floor is read on the same shape
	// the production pass decodes. The grouping pass never reads these
	// bytes; only the floor does.
	details := make([]byte, 1200)
	for i := range details {
		details[i] = 'a' + byte(i%26)
	}
	for i := 0; i < n/2; i++ {
		ghsa := fmt.Sprintf("GHSA-bench-%05d", i)
		cve := fmt.Sprintf("CVE-2026-%05d", i)
		ghsaFact := aliasCVEFact(fmt.Sprintf("bench-ghsa-%d", i), ghsa, "osv", ghsa, ghsa, cve)
		ghsaFact.Payload["details"] = string(details)
		cveFact := aliasCVEFact(fmt.Sprintf("bench-cve-%d", i), cve, "nvd", cve, cve, ghsa)
		cveFact.Payload["details"] = string(details)
		out = append(out,
			ghsaFact,
			aliasAffectedPackageFact(fmt.Sprintf("bench-ghsa-pkg-%d", i), ghsa, "osv", ghsa),
			cveFact,
			aliasAffectedPackageFact(fmt.Sprintf("bench-cve-pkg-%d", i), cve, "nvd", cve),
		)
	}
	return out
}

// aliasBenchDecodePayload is a representative vulnerability.cve payload
// mirroring the cost proof's decode floor: json.Unmarshal into a generic map,
// once per record. It marshals a padded bench envelope payload, so the floor
// is read on the same bytes the production pass decodes.
func aliasBenchDecodePayload() []byte {
	envelopes := aliasBenchEnvelopes(2)
	raw, err := json.Marshal(envelopes[0].Payload)
	if err != nil {
		panic(err)
	}
	return raw
}

// BenchmarkAliasGroupingDecodeFloor measures the per-record decode floor the
// 5%-of-decode bar is read against: one json.Unmarshal of a representative
// cve payload per record.
func BenchmarkAliasGroupingDecodeFloor(b *testing.B) {
	raw := aliasBenchDecodePayload()
	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))
	for i := 0; i < b.N; i++ {
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			b.Fatal(err)
		}
	}
}

// aliasBenchSparseEnvelopes mirrors the production shape: 5% of records
// carry a cross-record alias edge, 95% are alias-free singletons. The
// fully-paired corpus is the adversarial worst case; this one is what the
// pass pays on a real load.
func aliasBenchSparseEnvelopes(n int) []facts.Envelope {
	out := make([]facts.Envelope, 0, 2*n)
	details := "benchmark advisory"
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("CVE-2026-s%05d", i)
		fact := aliasCVEFact(fmt.Sprintf("sparse-cve-%d", i), id, "nvd", id, id)
		if i%20 == 0 {
			peer := fmt.Sprintf("GHSA-sparse-%05d", i)
			fact.Payload["aliases"] = []any{id, peer}
			peerFact := aliasCVEFact(fmt.Sprintf("sparse-peer-%d", i), peer, "osv", peer, peer, id)
			peerFact.Payload["details"] = details
			out = append(out, peerFact, aliasAffectedPackageFact(fmt.Sprintf("sparse-peer-pkg-%d", i), peer, "osv", peer))
		}
		fact.Payload["details"] = details
		out = append(out, fact, aliasAffectedPackageFact(fmt.Sprintf("sparse-pkg-%d", i), id, "nvd", id))
	}
	return out
}

// BenchmarkAliasGroupingTypedDecodeFloor measures the real per-record decode
// cost on the reducer path: the typed factschema decode plus validation that
// every record pays before the grouping pass reads it. The raw-unmarshal
// floor understates the run; this seam is the bar the pass is held to.
func BenchmarkAliasGroupingTypedDecodeFloor(b *testing.B) {
	envelopes := aliasBenchEnvelopes(1000)
	cves := make([]facts.Envelope, 0, 1000)
	for _, envelope := range envelopes {
		if envelope.FactKind == "vulnerability.cve" {
			cves = append(cves, envelope)
		}
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, envelope := range cves {
			if _, err := supplyChainCVEFromEnvelope(envelope); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkAliasGroupingIndexBuild measures the whole index build including
// the alias grouping pass at 1x (1,000 records) and 10x (10,000 records) on
// the adversarial fully-paired corpus. The disabled variant runs the
// identical build with grouping off, so enabled-minus-disabled isolates the
// pass cost. Measured bands (Apple M5, 30x; the raw-unmarshal floor on the
// same 1.3 KB payloads is ~3.4 us/record, so the 5% bar is ~172 ns/record):
// adversarial ~470 ns/record (~14% of the raw floor), sparse ~434 ns/record
// (~13%). The gap to the 5% bar is the ruled scope over the shim's
// union-find-only cost: package/product remap, group risk merge, per-key
// outcomes, and enrichment provenance. Absolute cost at 10k (~4.7 ms) is
// ~0.15% of a 3.02 s pass, and production corpora (sparse alias edges) pay
// only the singleton union-find path (cost proof
// 9209-3-2-advisory-group-cost.md).
func BenchmarkAliasGroupingIndexBuild(b *testing.B) {
	disabled := supplyChainAliasGroupConfig{disabled: true}
	for _, n := range []int{1000, 10000} {
		envelopes := aliasBenchEnvelopes(n)
		b.Run(fmt.Sprintf("records=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, _, err := buildSupplyChainImpactIndexWithQuarantine(envelopes); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("records=%d_grouping_disabled", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, _, err := buildSupplyChainImpactIndexWithQuarantine(envelopes, disabled); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkAliasGroupingIndexBuildSparse measures the same build on the
// production-shaped corpus: 5% of records carry a cross-record alias edge,
// 95% are alias-free singletons. The fully-paired benchmark is the
// adversarial worst case; this one is what the pass pays on a real load.
func BenchmarkAliasGroupingIndexBuildSparse(b *testing.B) {
	disabled := supplyChainAliasGroupConfig{disabled: true}
	for _, n := range []int{1000, 10000} {
		envelopes := aliasBenchSparseEnvelopes(n)
		b.Run(fmt.Sprintf("records=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, _, err := buildSupplyChainImpactIndexWithQuarantine(envelopes); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("records=%d_grouping_disabled", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, _, err := buildSupplyChainImpactIndexWithQuarantine(envelopes, disabled); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// TestAliasGroupingConcurrentPassesAgree is the concurrency proof: concurrent
// passes over overlapping alias components compute identical keys, so the
// winners upsert stays idempotent with no serialization.
func TestAliasGroupingConcurrentPassesAgree(t *testing.T) {
	t.Parallel()

	envelopes := aliasBenchEnvelopes(200)
	const workers = 8
	type result struct {
		keys    []string
		members map[string][]string
	}
	results := make([]result, workers)
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			index, quarantined, err := buildSupplyChainImpactIndexWithQuarantine(envelopes)
			if err != nil {
				errs[w] = err
				return
			}
			if len(quarantined) != 0 {
				errs[w] = fmt.Errorf("quarantined %d facts", len(quarantined))
				return
			}
			r := result{members: map[string][]string{}}
			for _, cve := range index.cves {
				r.keys = append(r.keys, cve.AdvisoryID+"->"+cve.CVEID)
			}
			for key, group := range index.aliasGroups {
				r.members[key] = append([]string(nil), group.members...)
			}
			slices.Sort(r.keys)
			results[w] = r
		}(w)
	}
	wg.Wait()
	for w, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", w, err)
		}
	}
	for w := 1; w < workers; w++ {
		if !slices.Equal(results[w].keys, results[0].keys) {
			t.Fatalf("worker %d keys differ from worker 0", w)
		}
		if len(results[w].members) != len(results[0].members) {
			t.Fatalf("worker %d groups %d != worker 0 groups %d", w, len(results[w].members), len(results[0].members))
		}
		for key, members := range results[0].members {
			if !slices.Equal(results[w].members[key], members) {
				t.Fatalf("worker %d group %s members differ", w, key)
			}
		}
	}
}
