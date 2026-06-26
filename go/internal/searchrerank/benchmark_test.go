// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchrerank

import (
	"math"
	"strconv"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

// rerankBenchCase is one labeled rerank benchmark query: a baseline ranking, the
// request scope, and the document ids that are truly relevant. The baseline puts
// the relevant document below a lexical near-miss so the benchmark measures
// whether graph reranking recovers it.
type rerankBenchCase struct {
	name     string
	scope    searchretrieval.Scope
	baseline []searchretrieval.Result
	relevant map[string]bool
}

// ndcgAt computes binary-relevance nDCG@k for an ordered list of result ids.
func ndcgAt(order []string, relevant map[string]bool, k int) float64 {
	dcg := 0.0
	for i, id := range order {
		if i >= k {
			break
		}
		if relevant[id] {
			dcg += 1.0 / math.Log2(float64(i+2))
		}
	}
	ideal := 0.0
	for i := 0; i < len(relevant) && i < k; i++ {
		ideal += 1.0 / math.Log2(float64(i+2))
	}
	if ideal == 0 {
		return 0
	}
	return dcg / ideal
}

func orderIDs(results []searchretrieval.Result) []string {
	out := make([]string, 0, len(results))
	for _, r := range results {
		out = append(out, r.Document.ID)
	}
	return out
}

func rerankBenchCases() []rerankBenchCase {
	return []rerankBenchCase{
		{
			name:  "service_story_anchor",
			scope: searchretrieval.Scope{ServiceID: "svc-checkout", RepoID: "repo-pay"},
			baseline: []searchretrieval.Result{
				result("doc-near-miss", 1, 4.0, handle("repository", "repo-pay")),
				result("doc-service", 2, 3.0, handle("service", "svc-checkout")),
				result("doc-noise", 3, 2.0),
			},
			relevant: map[string]bool{"doc-service": true},
		},
		{
			name:  "incident_context_anchor",
			scope: searchretrieval.Scope{RepoID: "repo-pay"},
			baseline: []searchretrieval.Result{
				result("doc-near-miss", 1, 4.0, handle("repository", "repo-pay")),
				result("doc-incident", 2, 3.5, handle("incident", "inc-7")),
				result("doc-noise", 3, 1.0),
			},
			relevant: map[string]bool{"doc-incident": true},
		},
		{
			name:  "supply_chain_package_anchor",
			scope: searchretrieval.Scope{RepoID: "repo-pay"},
			baseline: []searchretrieval.Result{
				result("doc-near-miss", 1, 4.0, handle("repository", "repo-pay")),
				result("doc-package", 2, 3.2, handle("container_image", "img-9")),
			},
			relevant: map[string]bool{"doc-package": true},
		},
		{
			name:  "no_graph_signal_neutral",
			scope: searchretrieval.Scope{RepoID: "repo-pay"},
			baseline: []searchretrieval.Result{
				result("doc-a", 1, 4.0),
				result("doc-b", 2, 3.0),
			},
			relevant: map[string]bool{"doc-a": true},
		},
	}
}

// TestRerankBenchmarkAcceptanceEvidence is the measured accept/reject gate for
// graph-neighborhood reranking. It scores baseline vs reranked nDCG@3 over a
// labeled fixture suite and asserts the documented acceptance thresholds:
// reranking never regresses mean nDCG and strictly improves it on the
// graph-anchored cases. The numbers it prints back the checked-in evidence doc
// at docs/public/reference/searchbench-evidence/2678-graph-rerank.md.
func TestRerankBenchmarkAcceptanceEvidence(t *testing.T) {
	const k = 3
	cases := rerankBenchCases()

	var baselineSum, rerankedSum float64
	improved := 0
	for _, bc := range cases {
		baselineNDCG := ndcgAt(orderIDs(bc.baseline), bc.relevant, k)
		out := Rerank(bc.baseline, Options{
			Enabled: true,
			Anchor:  bc.scope.Anchor(),
			Scope:   bc.scope,
		})
		reranked := make([]searchretrieval.Result, 0, len(out.Results))
		for _, r := range out.Results {
			reranked = append(reranked, r.Result)
		}
		rerankedNDCG := ndcgAt(orderIDs(reranked), bc.relevant, k)

		if rerankedNDCG+1e-9 < baselineNDCG {
			t.Errorf("case %q regressed: baseline nDCG=%.4f reranked nDCG=%.4f",
				bc.name, baselineNDCG, rerankedNDCG)
		}
		if rerankedNDCG > baselineNDCG+1e-9 {
			improved++
		}
		baselineSum += baselineNDCG
		rerankedSum += rerankedNDCG
		t.Logf("case %-28s baseline_ndcg@%d=%.4f reranked_ndcg@%d=%.4f state=%s",
			bc.name, k, baselineNDCG, k, rerankedNDCG, out.State)
	}

	meanBaseline := baselineSum / float64(len(cases))
	meanReranked := rerankedSum / float64(len(cases))
	t.Logf("SUITE mean_baseline_ndcg@%d=%.4f mean_reranked_ndcg@%d=%.4f improved_cases=%d/%d",
		k, meanBaseline, k, meanReranked, improved, len(cases))

	// Acceptance: no regression in mean nDCG, and a strict improvement on the
	// graph-anchored cases (service, incident, package). The neutral no-signal
	// case must not move, proving reranking fails closed when no signal fires.
	if meanReranked+1e-9 < meanBaseline {
		t.Fatalf("REJECT: mean nDCG regressed (%.4f -> %.4f)", meanBaseline, meanReranked)
	}
	if improved < 3 {
		t.Fatalf("REJECT: expected >=3 improved cases, got %d", improved)
	}
}

func BenchmarkRerank(b *testing.B) {
	for _, size := range []int{10, 100, 1000} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			baseline := makeBenchResults(size, 0.3)
			opts := Options{
				Enabled: true,
				Anchor:  searchretrieval.Anchor{Kind: searchretrieval.ScopeKindRepo, ID: "repo-1"},
				Scope:   searchretrieval.Scope{RepoID: "repo-1"},
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = Rerank(baseline, opts)
			}
		})
	}
}

func BenchmarkRerankDenseSignals(b *testing.B) {
	for _, size := range []int{10, 100, 1000} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			baseline := makeBenchResults(size, 0.9)
			opts := Options{
				Enabled: true,
				Anchor:  searchretrieval.Anchor{Kind: searchretrieval.ScopeKindRepo, ID: "repo-1"},
				Scope:   searchretrieval.Scope{RepoID: "repo-1"},
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = Rerank(baseline, opts)
			}
		})
	}
}

func BenchmarkRerankNoSignals(b *testing.B) {
	for _, size := range []int{10, 100, 1000} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			baseline := makeBenchResults(size, 0)
			opts := Options{
				Enabled: true,
				Anchor:  searchretrieval.Anchor{Kind: searchretrieval.ScopeKindRepo, ID: "repo-1"},
				Scope:   searchretrieval.Scope{RepoID: "repo-1"},
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = Rerank(baseline, opts)
			}
		})
	}
}

func makeBenchResults(n int, signalDensity float64) []searchretrieval.Result {
	results := make([]searchretrieval.Result, n)
	signalKinds := []string{"service", "workload", "deployment", "incident", "container_image", "owner"}
	for i := range results {
		var handles []searchdocs.GraphHandle
		if signalDensity > 0 && float64(i)/float64(n) < signalDensity {
			kind := signalKinds[i%len(signalKinds)]
			handles = append(handles, handle(kind, "sig-"+strconv.Itoa(i)))
		}
		results[i] = result(
			"d-"+strconv.Itoa(i),
			i+1,
			float64(n-i),
			handles...,
		)
	}
	return results
}
